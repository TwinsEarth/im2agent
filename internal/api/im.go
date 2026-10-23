package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	"human-agent-im/internal/eventbus"
	"human-agent-im/internal/im"
)

// pgGroupRepo 用真实 PostgreSQL 实现 im.GroupRepo。
type pgGroupRepo struct{ db *sql.DB }

func (r pgGroupRepo) Members(groupID string) ([]string, error) {
	rows, err := r.db.Query(`SELECT user_id FROM im_group_members WHERE group_id=$1 ORDER BY user_id`, groupID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var u string
		if err := rows.Scan(&u); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

func (r pgGroupRepo) SaveGroupMessage(m im.GroupMessage) (int64, error) {
	var id int64
	err := r.db.QueryRow(
		`INSERT INTO im_group_messages(group_id, from_user, text) VALUES ($1,$2,$3) RETURNING id`,
		m.GroupID, m.FromUser, m.Text,
	).Scan(&id)
	return id, err
}

func (d *Deps) IMMux() http.Handler {
	mux := http.NewServeMux()
	appID, _ := strconv.ParseInt(os.Getenv("TIM_SDK_APP_ID"), 10, 64)
	secretKey := os.Getenv("TIM_SECRET_KEY")
	endpoint := os.Getenv("TIM_ENDPOINT")
	// 回调验签密钥：优先专用 TIM_WEBHOOK_SECRET，缺省回退 SecretKey。
	webhookSecret := os.Getenv("TIM_WEBHOOK_SECRET")
	if webhookSecret == "" {
		webhookSecret = secretKey
	}
	client := im.New(im.Config{
		SDKAppID:  appID,
		SecretKey: secretKey,
		Endpoint:  endpoint,
	})
	repo := pgGroupRepo{db: d.DB}

	// v1.2.1: UserSig 签发
	mux.HandleFunc("POST /v1/im/usersig", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			UserID string `json:"userId"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		us, err := client.GenUserSig(req.UserID)
		if err != nil {
			writeErr(w, 400, err.Error())
			return
		}
		writeJSON(w, 200, us)
	})

	// v1.2.1: UserSig 刷新（重新签发，旧票自然过期）
	mux.HandleFunc("POST /v1/im/usersig/refresh", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			UserID string `json:"userId"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		us, err := client.GenUserSig(req.UserID)
		if err != nil {
			writeErr(w, 400, err.Error())
			return
		}
		writeJSON(w, 200, map[string]any{
			"userId": us.UserID, "userSig": us.Sig, "expiresAt": us.ExpiresAt,
			"mode": us.Mode, "refreshed": true,
		})
	})

	// v1.2.2: Webhook 第三方回调（验签 + SDKAppID 校验 + MsgKey 幂等落库）。
	mux.HandleFunc("POST /v1/im/webhook", func(w http.ResponseWriter, r *http.Request) {
		raw, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		if err != nil {
			writeErr(w, 400, "read body")
			return
		}
		// 验签：配置了密钥时强制；支持签名头或 ?key= 两种方式。
		verified := false
		if webhookSecret != "" {
			if qk := r.URL.Query().Get("key"); qk != "" {
				if qk != webhookSecret {
					writeErr(w, 401, "bad webhook key")
					return
				}
				verified = true
			} else {
				ts, _ := strconv.ParseInt(r.Header.Get("X-TIM-Timestamp"), 10, 64)
				sig := r.Header.Get("X-TIM-Signature")
				if err := im.VerifyWebhook(webhookSecret, ts, raw, sig); err != nil {
					writeErr(w, 401, err.Error())
					return
				}
				verified = true
			}
		} else if os.Getenv("IM2A_WEBHOOK_DEV") != "1" {
			// 安全默认：对外回调端点未配签名密钥时拒绝，防止匿名伪造业务消息。
			writeErr(w, 503, "webhook unconfigured: set TIM_WEBHOOK_SECRET (use ?key= in Tencent callback URL)")
			return
		}

		var ev im.WebhookEvent
		if err := json.Unmarshal(raw, &ev); err != nil {
			writeErr(w, 400, "bad event")
			return
		}
		if err := ev.Valid(); err != nil {
			writeErr(w, 400, err.Error())
			return
		}
		if !client.AppIDMatches(ev) {
			writeErr(w, 401, "SdkAppID mismatch")
			return
		}

		duplicated := false
		if d.DB != nil {
			payload, _ := json.Marshal(ev)
			res, err := d.DB.Exec(`INSERT INTO im_webhook_events
				(callback_type, from_user, to_user, group_id, msg_key, sdk_app_id, payload)
				VALUES ($1,$2,$3,$4,$5,$6,$7)
				ON CONFLICT (msg_key) WHERE msg_key IS NOT NULL DO NOTHING`,
				ev.CallbackType, ev.FromUserID, ev.ToUserID, ev.GroupID,
				nullIfEmpty(ev.MsgKey), ev.SdkAppID, payload)
			if err != nil {
				log.Printf("webhook persist error: %v", err)
				writeErr(w, 500, "persist webhook")
				return
			}
			if n, _ := res.RowsAffected(); n == 0 && ev.MsgKey != "" {
				duplicated = true
			}
		}
		// 腾讯云要求回调返回 200（否则重试），幂等重复也返回 200。
		writeJSON(w, 200, map[string]any{
			"action": "allow", "eventType": ev.CallbackType,
			"from": ev.FromUserID, "to": ev.ToUserID, "group": ev.GroupID,
			"channel": im.Route(ev.CallbackType), "verified": verified,
			"duplicated": duplicated, "persisted": d.DB != nil,
		})
	})

	// v1.2.0: 真实发送单聊（未配置腾讯云凭证返回 503，绝不假装成功）。
	mux.HandleFunc("POST /v1/im/send", func(w http.ResponseWriter, r *http.Request) {
		var req im.SendMessageReq
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeErr(w, 400, "bad body")
			return
		}
		rcpt, err := client.SendC2C(req)
		if err != nil {
			if errors.Is(err, im.ErrNotConfigured) {
				writeJSON(w, 503, map[string]any{"error": err.Error(), "mode": client.Mode()})
				return
			}
			log.Printf("tim send c2c error: %v", err)
			writeErr(w, 502, err.Error())
			return
		}
		writeJSON(w, 200, rcpt)
	})

	// v1.2.3: 消息三通道路由。
	mux.HandleFunc("POST /v1/im/dispatch", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			MsgType string `json:"msgType"`
			From    string `json:"from"`
			To      string `json:"to"`
			Text    string `json:"text"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		disp := &im.Dispatcher{
			IMClient: client,
			WSPublish: func(t string, p any) {
				if d.Stream != nil {
					d.Stream.Broadcast(t, p)
				}
			},
			BusPublish: func(e string, p any) {
				evt := &eventbus.Event{
					Type:      eventbus.EventType(e),
					AgentID:   req.From,
					Timestamp: time.Now().Unix(),
					Payload:   map[string]any{"from": req.From, "to": req.To, "data": p},
				}
				if d.Bus != nil {
					_ = d.Bus.Publish(context.Background(), evt)
				}
			},
		}
		ch, err := disp.Dispatch(req.MsgType, im.SendMessageReq{
			FromUserID: req.From, ToUserID: req.To, Text: req.Text,
		})
		if err != nil {
			if errors.Is(err, im.ErrNotConfigured) {
				writeJSON(w, 503, map[string]any{"channel": ch, "error": err.Error(), "mode": client.Mode()})
				return
			}
			writeErr(w, 500, err.Error())
			return
		}
		writeJSON(w, 200, map[string]any{"msgType": req.MsgType, "channel": ch, "mode": client.Mode()})
	})

	// v1.2.4: 建群（真实落 im_groups + im_group_members）。
	mux.HandleFunc("POST /v1/im/groups", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			GroupID string   `json:"groupId"`
			Name    string   `json:"name"`
			OwnerID string   `json:"ownerId"`
			Members []string `json:"members"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeErr(w, 400, "bad body")
			return
		}
		g, err := im.NewGroup(req.GroupID, req.Name, req.OwnerID, req.Members)
		if err != nil {
			writeErr(w, 400, err.Error())
			return
		}
		if d.DB != nil {
			if _, err := d.DB.Exec(`INSERT INTO im_groups(group_id,name,owner_id) VALUES($1,$2,$3)
				ON CONFLICT (group_id) DO NOTHING`, g.GroupID, g.Name, g.OwnerID); err != nil {
				log.Printf("create group error: %v", err)
				writeErr(w, 500, "persist group")
				return
			}
			for _, m := range g.Members {
				role := "member"
				if m == g.OwnerID {
					role = "owner"
				}
				if _, err := d.DB.Exec(`INSERT INTO im_group_members(group_id,user_id,role)
					VALUES($1,$2,$3) ON CONFLICT DO NOTHING`, g.GroupID, m, role); err != nil {
					log.Printf("add member error: %v", err)
					writeErr(w, 500, "persist member")
					return
				}
			}
		}
		writeJSON(w, 201, g)
	})

	// v1.2.4: 群成员列表。
	mux.HandleFunc("GET /v1/im/groups/{id}/members", func(w http.ResponseWriter, r *http.Request) {
		gid := r.PathValue("id")
		if d.DB == nil {
			writeJSON(w, 200, map[string]any{"groupId": gid, "members": []string{}, "count": 0})
			return
		}
		members, err := repo.Members(gid)
		if err != nil {
			log.Printf("list members error: %v", err)
			writeErr(w, 500, "list members")
			return
		}
		writeJSON(w, 200, map[string]any{"groupId": gid, "members": members, "count": len(members)})
	})

	// v1.2.4: 群消息广播（成员校验 → 落 im_group_messages → 推流给其他成员）。
	mux.HandleFunc("POST /v1/im/groups/{id}/broadcast", func(w http.ResponseWriter, r *http.Request) {
		gid := r.PathValue("id")
		var req struct {
			From string `json:"from"`
			Text string `json:"text"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeErr(w, 400, "bad body")
			return
		}
		if d.DB == nil {
			writeJSON(w, 503, map[string]any{"error": "persistence not configured"})
			return
		}
		bc := &im.Broadcaster{
			Repo: repo,
			Push: func(topic string, payload any) {
				if d.Stream != nil {
					d.Stream.Broadcast(topic, payload)
				}
			},
		}
		pushed, msgID, err := bc.Broadcast(im.GroupMessage{
			GroupID: gid, FromUser: req.From, Text: req.Text,
		})
		if err != nil {
			if errors.Is(err, im.ErrNotMember) {
				writeErr(w, 403, err.Error())
				return
			}
			log.Printf("group broadcast error: %v", err)
			writeErr(w, 500, err.Error())
			return
		}
		writeJSON(w, 200, map[string]any{
			"groupId": gid, "from": req.From, "msgId": msgID,
			"pushed": pushed, "pushedCount": len(pushed),
		})
	})

	return mux
}

// nullIfEmpty 空字符串转 NULL（用于可空唯一键）。
func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}


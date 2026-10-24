package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"os"

	"human-agent-im/internal/relay"
)

// webhookRequest 是入站消息信封（微信 → openclaw → IM2A）。
type webhookRequest struct {
	Account       string `json:"account"`       // 哪个 openclaw bot 收到
	BotWxid       string `json:"botWxid"`       // 可选，缺省由 account 解析
	FromWxid      string `json:"fromWxid"`      // 发消息者 wxid
	Text          string `json:"text"`          // 消息正文
	MsgID         string `json:"msgId"`         // 渠道消息唯一 id（幂等）
	NegoSessionID string `json:"negoSessionId"` // 非空=推进已存在的协商会话
	NegoIntent    string `json:"negoIntent"`    // ACCEPT/COUNTER/REJECT/REQUEST_INFO/REQUEST_AUTH
}

// webhookIdentity 是 wxid → 用户的识别结果（任务 #105）。
type webhookIdentity struct {
	Wxid   string `json:"wxid"`
	UserID int64  `json:"userId"`
	Bound  bool   `json:"bound"`
}

// webhookResponse 是 webhook 响应。
type webhookResponse struct {
	OK       bool             `json:"ok"`
	Identity *webhookIdentity `json:"identity,omitempty"`
	Result   *relay.Result    `json:"result,omitempty"`
	Error    string           `json:"error,omitempty"`
}

// NewWechatWebhookHandler 构造 v2.2.1 标准 REST webhook：POST /v1/wechat/webhook。
//
// 与 MCP 端点（/v2/wechat/mcp）共用同一个 relay.Service，提供给 openclaw / 其他渠道
// 在收到微信消息后做确定性 HTTP POST（不依赖 agent 自主决定调工具）。
//
// 鉴权：query ?key= 或 Header X-Webhook-Key，需等于环境变量 IM2A_WECHAT_WEBHOOK_KEY；
// 该变量为空时表示仅在 loopback 部署、不做 key 校验（与 MCP 端点一致）。
func NewWechatWebhookHandler(relaySvc *relay.Service, db *sql.DB) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method != http.MethodPost {
			writeWebhookError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		if !webhookAuthorized(r) {
			writeWebhookError(w, http.StatusUnauthorized, "invalid webhook key")
			return
		}

		var req webhookRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeWebhookError(w, http.StatusBadRequest, "parse error: "+err.Error())
			return
		}
		if req.Account == "" || req.FromWxid == "" || req.Text == "" {
			writeWebhookError(w, http.StatusBadRequest, "account/fromWxid/text required")
			return
		}

		// 协商推进分支：带 negoSessionId 时推进 8 态状态机，而非新建消息。
		if req.NegoSessionID != "" {
			sess, nerr := relaySvc.NegoReply(r.Context(), req.NegoSessionID, req.FromWxid, req.NegoIntent, req.Text)
			if nerr != nil {
				writeWebhookError(w, http.StatusBadRequest, "nego reply: "+nerr.Error())
				return
			}
			json.NewEncoder(w).Encode(map[string]any{
				"ok":   true,
				"nego": map[string]any{"id": sess.ID, "state": sess.State.String(), "round": sess.Round},
			})
			return
		}

		// 识别 wxid → 查用户（best-effort，未绑定也允许走 self 助手，由 relay 决定）。
		ident := identifyWxid(r.Context(), db, req.FromWxid)

		// 幂等键优先用渠道 msgId，否则用账号/发送者/正文兜底。
		idem := req.MsgID
		if idem == "" {
			idem = "wh_" + req.Account + "_" + req.FromWxid + "_" + req.Text
		}
		res, err := relaySvc.Relay(r.Context(), req.Account, req.FromWxid, req.Text, idem)
		if err != nil {
			writeWebhookError(w, http.StatusBadGateway, "relay: "+err.Error())
			return
		}
		json.NewEncoder(w).Encode(webhookResponse{OK: true, Identity: ident, Result: res})
	})
}

// identifyWxid 查 wechat_identity 把 wxid 映射到 user_id。
func identifyWxid(ctx context.Context, db *sql.DB, wxid string) *webhookIdentity {
	ident := &webhookIdentity{Wxid: wxid, Bound: false}
	if db == nil || wxid == "" {
		return ident
	}
	var uid int64
	err := db.QueryRowContext(ctx, `SELECT user_id FROM wechat_identity WHERE wxid=$1`, wxid).Scan(&uid)
	if err == nil {
		ident.UserID = uid
		ident.Bound = true
	}
	return ident
}

func webhookAuthorized(r *http.Request) bool {
	want := os.Getenv("IM2A_WECHAT_WEBHOOK_KEY")
	if want == "" {
		return true // 仅 loopback 部署，不校验
	}
	got := r.URL.Query().Get("key")
	if got == "" {
		got = r.Header.Get("X-Webhook-Key")
	}
	return got == want
}

func writeWebhookError(w http.ResponseWriter, code int, msg string) {
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(webhookResponse{OK: false, Error: msg})
}

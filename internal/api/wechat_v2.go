package api

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"

	"human-agent-im/internal/a2a"
	"human-agent-im/internal/agentdir"
	"human-agent-im/internal/agentquota"
	"human-agent-im/internal/botroute"
	"human-agent-im/internal/report"
	"human-agent-im/internal/tempgroup"
	"human-agent-im/internal/userkey"
	"human-agent-im/internal/wechatapi"
	"human-agent-im/internal/wechatbot"
	"human-agent-im/internal/wechatid"
)

// WechatV2Mux 统一接入 v2.0.x 微信 Bot 体系全部端点。
func (d *Deps) WechatV2Mux() http.Handler {
	mux := http.NewServeMux()
	w := func(wr http.ResponseWriter, code int, v any) {
		wr.Header().Set("Content-Type", "application/json; charset=utf-8")
		wr.WriteHeader(code)
		_ = json.NewEncoder(wr).Encode(v)
	}

	// ---- v2.0.5 协议入口（验签/幂等/分发，全依赖注入）----
	wapi := wechatapi.New(d.DB, userkey.New(d.DB)).WithOptions(wechatapi.Options{
		Quota:    agentquota.New(d.DB),
		Dir:      agentdir.New(d.DB),
		A2A:      a2a.New(d.DB),
		WechatID: wechatid.New(d.DB),
	})
	mux.HandleFunc("POST /v1/wechatbot/inbox", func(wr http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		out, err := wapi.Handle(r.Context(), body)
		if err != nil {
			code := 400
			if strings.Contains(err.Error(), "sig") || strings.Contains(err.Error(), "unbound") {
				code = 401
			}
			if strings.Contains(err.Error(), "quota") {
				code = 409
			}
			w(wr, code, map[string]any{"error": err.Error()})
			return
		}
		wr.Header().Set("Content-Type", "application/json; charset=utf-8")
		_, _ = wr.Write(out)
	})

	// ---- v2.0.0 Bot 组绑定 ----
	bot := wechatbot.New(d.DB)
	mux.HandleFunc("POST /v1/wechat/groups", func(wr http.ResponseWriter, r *http.Request) {
		var b struct{ Id, Owner, Name string }
		if err := decode(r, &b); err != nil { w(wr, 400, errBody{err.Error()}); return }
		g, err := bot.CreateGroup(r.Context(), b.Id, b.Owner, b.Name)
		if err != nil { w(wr, 400, errBody{err.Error()}); return }
		w(wr, 201, g)
	})
	mux.HandleFunc("POST /v1/wechat/bindings", func(wr http.ResponseWriter, r *http.Request) {
		var b struct{ Id, GroupId, Wxid, Device string }
		if err := decode(r, &b); err != nil { w(wr, 400, errBody{err.Error()}); return }
		bind, err := bot.BindDevice(r.Context(), b.Id, b.GroupId, b.Wxid, b.Device)
		if err != nil { w(wr, 409, errBody{err.Error()}); return }
		w(wr, 201, bind)
	})
	mux.HandleFunc("GET /v1/wechat/groups/{id}", func(wr http.ResponseWriter, r *http.Request) {
		g, bs, err := bot.Group(r.Context(), r.PathValue("id"))
		if err != nil { w(wr, 404, errBody{err.Error()}); return }
		w(wr, 200, map[string]any{"group": g, "bindings": bs})
	})

	// ---- v2.0.1 wxid 识别 + 额度 ----
	wid := wechatid.New(d.DB)
	q := agentquota.New(d.DB)
	mux.HandleFunc("POST /v1/wechat/identify", func(wr http.ResponseWriter, r *http.Request) {
		var b struct{ Wxid string }
		if err := decode(r, &b); err != nil || b.Wxid == "" { w(wr, 400, errBody{"missing wxid"}); return }
		uid, isNew, err := wid.Identify(r.Context(), b.Wxid)
		if err != nil { w(wr, 400, errBody{err.Error()}); return }
		w(wr, 200, map[string]any{"userId": uid, "isNew": isNew})
	})
	mux.HandleFunc("GET /v1/quota/{userId}", func(wr http.ResponseWriter, r *http.Request) {
		uid, _ := strconv.ParseInt(r.PathValue("userId"), 10, 64)
		u, err := q.Usage(r.Context(), uid)
		if err != nil { w(wr, 404, errBody{err.Error()}); return }
		w(wr, 200, u)
	})
	mux.HandleFunc("POST /v1/quota/upgrade", func(wr http.ResponseWriter, r *http.Request) {
		var b struct{ UserId int64; Plan string }
		if err := decode(r, &b); err != nil { w(wr, 400, errBody{err.Error()}); return }
		if err := q.Upgrade(r.Context(), b.UserId, b.Plan); err != nil { w(wr, 400, errBody{err.Error()}); return }
		w(wr, 200, map[string]any{"userId": b.UserId, "plan": b.Plan, "upgraded": true})
	})

	// ---- v2.0.8 临时协商群 ----
	tg := tempgroup.New(d.DB)
	mux.HandleFunc("POST /v1/tempgroup/create", func(wr http.ResponseWriter, r *http.Request) {
		var b struct{ Id, Topic, CreatedBy string }
		if err := decode(r, &b); err != nil { w(wr, 400, errBody{err.Error()}); return }
		if err := tg.Create(r.Context(), b.Id, b.Topic, b.CreatedBy); err != nil { w(wr, 400, errBody{err.Error()}); return }
		w(wr, 201, map[string]any{"id": b.Id, "active": true})
	})
	mux.HandleFunc("POST /v1/tempgroup/join", func(wr http.ResponseWriter, r *http.Request) {
		var b struct{ GroupId, AgentId string }
		if err := decode(r, &b); err != nil { w(wr, 400, errBody{err.Error()}); return }
		if err := tg.Join(r.Context(), b.GroupId, b.AgentId); err != nil { w(wr, 409, errBody{err.Error()}); return }
		w(wr, 200, map[string]any{"joined": true})
	})
	mux.HandleFunc("POST /v1/tempgroup/post", func(wr http.ResponseWriter, r *http.Request) {
		var b struct{ GroupId, FromAgent string; Payload any }
		if err := decode(r, &b); err != nil { w(wr, 400, errBody{err.Error()}); return }
		if err := tg.Post(r.Context(), b.GroupId, b.FromAgent, b.Payload); err != nil { w(wr, 409, errBody{err.Error()}); return }
		w(wr, 201, map[string]any{"posted": true})
	})
	mux.HandleFunc("POST /v1/tempgroup/destroy", func(wr http.ResponseWriter, r *http.Request) {
		var b struct{ GroupId string }
		if err := decode(r, &b); err != nil { w(wr, 400, errBody{err.Error()}); return }
		if err := tg.Destroy(r.Context(), b.GroupId); err != nil { w(wr, 409, errBody{err.Error()}); return }
		w(wr, 200, map[string]any{"destroyed": true})
	})
	mux.HandleFunc("GET /v1/tempgroup/history/{id}", func(wr http.ResponseWriter, r *http.Request) {
		h, err := tg.History(r.Context(), r.PathValue("id"), 50)
		if err != nil { w(wr, 400, errBody{err.Error()}); return }
		w(wr, 200, map[string]any{"messages": h, "count": len(h)})
	})

	// ---- v2.0.9 Bot 间路由 ----
	br := botroute.New(d.DB)
	mux.HandleFunc("POST /v1/botroute/send", func(wr http.ResponseWriter, r *http.Request) {
		var b struct{ From, To, Type string; Payload any }
		if err := decode(r, &b); err != nil { w(wr, 400, errBody{err.Error()}); return }
		if err := br.Route(r.Context(), b.From, b.To, b.Type, b.Payload); err != nil { w(wr, 409, errBody{err.Error()}); return }
		w(wr, 202, map[string]any{"routed": true})
	})
	mux.HandleFunc("GET /v1/botroute/inbox/{wxid}", func(wr http.ResponseWriter, r *http.Request) {
		items, err := br.Inbox(r.Context(), r.PathValue("wxid"), 50)
		if err != nil { w(wr, 400, errBody{err.Error()}); return }
		w(wr, 200, map[string]any{"items": items, "count": len(items)})
	})

	// ---- v2.1.0 工作汇报 ----
	rep := report.New(d.DB)
	mux.HandleFunc("POST /v1/report/generate", func(wr http.ResponseWriter, r *http.Request) {
		var b struct{ UserId string }
		if err := decode(r, &b); err != nil { w(wr, 400, errBody{err.Error()}); return }
		rp, err := rep.Generate(r.Context(), b.UserId)
		if err != nil { w(wr, 400, errBody{err.Error()}); return }
		w(wr, 201, rp)
	})
	mux.HandleFunc("POST /v1/report/{id}/approve", func(wr http.ResponseWriter, r *http.Request) {
		id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err := rep.Approve(r.Context(), id); err != nil { w(wr, 409, errBody{err.Error()}); return }
		w(wr, 200, map[string]any{"id": id, "status": "approved"})
	})
	return mux
}

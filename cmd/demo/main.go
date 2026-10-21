// cmd/demo 极轻量验证原型：单二进制、内存存储、内嵌网页。
//
// 一条命令起完整闭环：
//
//	go run ./cmd/demo
//
// 浏览器打开 http://localhost:8080 即可看到：
//   - 本人视角的会话列表（家庭 Agent / 物业 Agent / 陌生人商机 / 审计流）
//   - 一条家庭 Agent 发起的大额采购审批卡（2500 元人体工学椅）
//   - 一条陌生人付费来意卡（商务合作，发送即扣费）
//   - 通过 / 驳回 / 代回复 后状态即时流转，审计流实时回放
//
// 不依赖 postgres / redis / kafka / flutter / docker，零外部进程。
package main

import (
	"context"
	"embed"
	"encoding/json"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"im2agent/internal/api"
	"im2agent/internal/approval"
	"im2agent/internal/audit"
	"im2agent/internal/id"
	"im2agent/internal/ledger"
	"im2agent/internal/llm"
	"im2agent/internal/trustzone"
)

//go:embed web
var webFS embed.FS

// demo 用户 ID（本人）。
const demoUserID uint64 = 100

func main() {
	addr := flag.String("addr", ":8080", "监听地址")
	healthcheck := flag.Bool("healthcheck", false, "健康检查模式：调用本机 /healthz，成功 exit 0，失败 exit 1（供 docker HEALTHCHECK 用）")
	flag.Parse()

	if *healthcheck {
		// 从 -addr 推断监听端口（去掉前导冒号）
		host := strings.TrimPrefix(*addr, ":")
		if host == "" {
			host = "8080"
		}
		resp, err := http.Get("http://127.0.0.1:" + host + "/healthz")
		if err != nil {
			os.Exit(1)
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			os.Exit(1)
		}
		os.Exit(0)
	}

	deps, err := api.NewDeps("", "")
	if err != nil {
		log.Fatalf("init deps: %v", err)
	}
	orch := api.NewOrchestrator(*deps)

	// v0.7-llm：从环境变量初始化 OpenAI 兼容客户端（未配置则优雅降级）
	llmClient, llmErr := llm.New(
		os.Getenv("OPENAI_BASE_URL"),
		os.Getenv("OPENAI_API_KEY"),
		os.Getenv("LLM_MODEL"),
	)
	llmReady := llmErr == nil

	seed(deps, orch)

	top := http.NewServeMux()

	// 业务子服务（StripPrefix 后复用现有 mux）
	top.Handle("/api/relation/", http.StripPrefix("/api/relation", deps.RelationMux("relation-svc")))
	top.Handle("/api/payment/", http.StripPrefix("/api/payment", deps.PaymentMux("payment-svc")))
	top.Handle("/api/approval/", http.StripPrefix("/api/approval", deps.ApprovalMux("approval-engine")))
	top.Handle("/api/negotiation/", http.StripPrefix("/api/negotiation", deps.NegotiationMux("negotiation-svc")))
	top.Handle("/api/compliance/", http.StripPrefix("/api/compliance", deps.ComplianceMux("compliance-gate")))
	top.Handle("/api/msg/", http.StripPrefix("/api/msg", api.NewMsgMux("msg-svc")))
	top.Handle("/api/orchestrator/", http.StripPrefix("/api/orchestrator", orch.Mux("agent-orchestrator")))

	// POST /api/approval/decide  清单约定的统一决策入口。
	// Body: {"ticket_id":"uuid","decision":"approve|reject|modify","note":"...","patch":{...}}
	top.HandleFunc("POST /api/approval/decide", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			TicketID string         `json:"ticket_id"`
			Decision string         `json:"decision"`
			Note     string         `json:"note"`
			Patch    map[string]any `json:"patch"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.TicketID == "" {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "ticket_id required"})
			return
		}
		var t *approval.Ticket
		var err error
		switch req.Decision {
		case "approve":
			t, err = deps.Engine.Approve(req.TicketID, "user", req.Note)
		case "reject":
			t, err = deps.Engine.Reject(req.TicketID, "user", req.Note)
		case "modify":
			t, err = deps.Engine.Modify(req.TicketID, req.Patch, "user", req.Note)
		default:
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "decision must be approve|reject|modify"})
			return
		}
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"id": t.ID, "state": string(t.State)})
	})

	// demo 聚合状态（前端轮询）
	top.HandleFunc("GET /api/admin/state", func(w http.ResponseWriter, r *http.Request) {
		all, _ := deps.Store.Approval.All()
		pending := make([]map[string]any, 0, len(all))
		tickets := make([]map[string]any, 0, len(all))
		for _, t := range all {
			tickets = append(tickets, map[string]any{
				"id": t.ID, "user_id": t.UserID, "agent_id": t.SourceAgentID,
				"title": titleOf(t), "context": contextOf(t),
				"price": priceOf(t), "status": string(t.State),
				"created_at": t.CreatedAt,
			})
			if t.UserID != demoUserID {
				continue
			}
			if t.State == approval.StateCompleted || t.State == approval.StateRejected || t.State == approval.StateFailed {
				continue
			}
			pending = append(pending, map[string]any{
				"id":        t.ID,
				"type":      string(t.Type),
				"from":      t.SourceAgentID,
				"state":     string(t.State),
				"payload":   t.Payload,
				"deadline":  t.Deadline,
				"createdAt": t.CreatedAt,
			})
		}
		// 账本摘要（对齐清单 ledgers）。
		ledgerEntries := []map[string]any{}
		if l, ok := deps.Store.Ledger.(interface{ List() []*ledger.Entry }); ok {
			for _, e := range l.List() {
				ledgerEntries = append(ledgerEntries, map[string]any{
					"message_id":   e.MessageID,
					"sender_id":    e.SenderID,
					"receiver_id":  e.ReceiverID,
					"amount_cents": e.AmountCents,
					"status":       e.Status.String(),
				})
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"userId":   demoUserID,
			"pending":  pending,
			"tickets":  tickets,
			"audits":   auditEntries(deps),
			"ledgers":  ledgerEntries,
			"serverAt": time.Now(),
		})
	})

	// 健康检查
	top.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "app": "im2agent-demo"})
	})

	// v0.9-prod：可选鉴权中间件。设置 ADMIN_TOKEN 后，/api/admin/* 与 /admin/ 需 Bearer。
	adminToken := os.Getenv("ADMIN_TOKEN")
	var handler http.Handler = top
	if adminToken != "" {
		handler = withAdminAuth(top, adminToken)
		log.Printf("  鉴权已启用：/api/admin/* 与 /admin/ 需要 Authorization: Bearer <token>")
	}

	// ---- 运维界面（v0.4-admin）----
	// GET /api/admin/agents  Agent 能力注册表
	top.HandleFunc("GET /api/admin/agents", func(w http.ResponseWriter, r *http.Request) {
		caps, _ := deps.Store.Capability.ListCapabilities(r.Context())
		out := make([]map[string]any, 0, len(caps))
		for _, c := range caps {
			out = append(out, map[string]any{
				"agentId":                    c.AgentID,
				"trustLevelMin":              c.TrustLevelMin.String(),
				"allowedSkills":              c.AllowedSkills,
				"budgetCentsPerDay":          c.BudgetCentsPerDay,
				"requiresApprovalAboveCents": c.RequiresApprovalAboveCents,
			})
		}
		writeJSON(w, http.StatusOK, map[string]any{"agents": out})
	})

	// GET /api/admin/approvals  全部审批单（含终态）
	top.HandleFunc("GET /api/admin/approvals", func(w http.ResponseWriter, r *http.Request) {
		all, _ := deps.Store.Approval.All()
		out := make([]map[string]any, 0, len(all))
		for _, t := range all {
			out = append(out, map[string]any{
				"id":        t.ID,
				"userId":    t.UserID,
				"from":      t.SourceAgentID,
				"type":      string(t.Type),
				"state":     string(t.State),
				"payload":   t.Payload,
				"createdAt": t.CreatedAt,
				"deadline":  t.Deadline,
				"events":    t.Events,
			})
		}
		writeJSON(w, http.StatusOK, map[string]any{"tickets": out})
	})

	// GET /api/admin/ledger  付费账本全量流水 + 当日用量
	top.HandleFunc("GET /api/admin/ledger", func(w http.ResponseWriter, r *http.Request) {
		sent, limit := deps.Store.Ledger.DailyUsage(demoUserID)
		type entryView struct {
			MessageID   string    `json:"messageId"`
			SenderID    uint64    `json:"senderId"`
			ReceiverID  uint64    `json:"receiverId"`
			Mode        string    `json:"mode"`
			AmountCents int64     `json:"amountCents"`
			Status      string    `json:"status"`
			ChargedAt   time.Time `json:"chargedAt,omitempty"`
			CardID      string    `json:"approvalCardId,omitempty"`
		}
		entries := []entryView{}
		if l, ok := deps.Store.Ledger.(interface{ List() []*ledger.Entry }); ok {
			for _, e := range l.List() {
				mode := "send_charge"
				if e.Mode == ledger.ModeReplyCharge {
					mode = "reply_charge"
				}
				entries = append(entries, entryView{
					MessageID: e.MessageID, SenderID: e.SenderID, ReceiverID: e.ReceiverID,
					Mode: mode, AmountCents: e.AmountCents, Status: e.Status.String(),
					ChargedAt: e.ChargedAt, CardID: e.ApprovalCardID,
				})
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"entries":   entries,
			"dailySent": sent, "dailyLimit": limit,
		})
	})

	// GET /api/admin/audit  审计事件流
	top.HandleFunc("GET /api/admin/audit", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"events": auditEntries(deps),
		})
	})

	// ---- v0.7-llm 大模型驱动 ----
	// GET /api/llm/status  前端判断是否要显示"AI 起草"按钮
	top.HandleFunc("GET /api/llm/status", func(w http.ResponseWriter, r *http.Request) {
		model := os.Getenv("LLM_MODEL")
		if model == "" && llmReady {
			model = "(default)"
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"ready":  llmReady,
			"model":  model,
			"baseUrl": os.Getenv("OPENAI_BASE_URL"),
		})
	})

	// POST /api/llm/draft  让 Agent 起草一段回复（无 key 时返回 hardcode 草稿）
	top.HandleFunc("POST /api/llm/draft", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			StrangerAgent string `json:"strangerAgent"`
			Category      string `json:"category"`
			Content       string `json:"content"`
			Tone          string `json:"tone"` // polite / brief / decline
		}
		_ = json.NewDecoder(r.Body).Decode(&body)

		tone := body.Tone
		if tone == "" {
			tone = "polite"
		}

		// 无 LLM 配置：返回本地模板草稿（demo 仍可演示）
		if !llmReady {
			writeJSON(w, http.StatusOK, map[string]any{
				"draft": localDraft(body.Category, body.Content, tone),
				"via":   "template",
				"note":  "未配置 OPENAI_API_KEY，返回本地模板；设置环境变量后切换为大模型生成",
			})
			return
		}

		system := "你是用户的私人秘书 Agent，替用户给陌生人商务消息起草中文回复。要求：简短（80 字内）、专业、不暴露个人隐私、不承诺具体时间或金额。语气：" + tone
		user := fmt.Sprintf("陌生人：%s（%s）\n原话：%s\n请起草回复：",
			body.StrangerAgent, body.Category, body.Content)

		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()
		draft, err := llmClient.Chat(ctx, llm.ChatRequest{
			Messages: []llm.Message{
				{Role: "system", Content: system},
				{Role: "user", Content: user},
			},
			Temperature: 0.3,
			MaxTokens:   200,
		})
		if err != nil {
			writeJSON(w, http.StatusOK, map[string]any{
				"draft": localDraft(body.Category, body.Content, tone),
				"via":   "template_fallback",
				"note":  "LLM 调用失败：" + err.Error(),
			})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"draft": strings.TrimSpace(draft),
			"via":   "llm",
		})
	})

	// 静态前端（挂根子树；/api/... 与 /healthz 由更具体的注册优先匹配）
	sub, _ := fs.Sub(webFS, "web")
	top.Handle("/", http.FileServer(http.FS(sub)))

	// 运维界面 /admin/
	adminSub, _ := fs.Sub(webFS, "web/admin")
	top.Handle("/admin/", http.StripPrefix("/admin/", http.FileServer(http.FS(adminSub))))

	log.Printf("IM2Agent demo listening on http://localhost%s", *addr)
	log.Printf("  本人 userID=%d；家庭 Agent 已授权 L1，物业 Agent 已授权 L2", demoUserID)
	log.Printf("  已 seed：1 条家庭采购审批 + 1 条陌生人付费来意")
	if err := http.ListenAndServe(*addr, handler); err != nil {
		log.Fatal(err)
	}
}

// seed 预置演示数据：能力声明、两条待审卡片。
func seed(deps *api.Deps, orch *api.Orchestrator) {
	ctx := context.Background()

	// 1) family-agent：最低信任档位 L1 家庭；采购超 2000 元自动转审批
	_ = deps.Store.Capability.UpsertCapability(ctx, trustzone.Capability{
		AgentID:                    "family-agent",
		TrustLevelMin:              trustzone.LevelFamily,
		AllowedSkills:              []string{"home-maintenance", "family-purchase", "family-schedule"},
		RequiresApprovalAboveCents: 200000,
	})

	// 2) property-agent：最低信任档位 L2 信任，物业报修类 Skill
	_ = deps.Store.Capability.UpsertCapability(ctx, trustzone.Capability{
		AgentID:       "property-agent",
		TrustLevelMin: trustzone.LevelTrusted,
		AllowedSkills: []string{"property-repair", "utility-report"},
	})

	// 3) 家庭 Agent 发起大额采购审批（2500 元人体工学椅 > 2000 阈值）
	_, _ = deps.Engine.CreateTicket(demoUserID, "family-agent", approval.TicketPayment,
		map[string]any{
			"item":          "人体工学椅（保友金豪 B）",
			"amountCents":   250000,
			"reason":        "腰部不适，旧椅已用 6 年；比价 3 家，最低 2499",
			"skillId":       "family-purchase",
			"scope":         "purchase",
			"isStranger":    false,
		},
		time.Now().Add(30*time.Minute))

	// 4) 陌生人付费来意：商务合作，发送即扣费 1 元
	messageID := id.UUIDv4()
	_, _ = deps.Store.Ledger.Reserve(messageID, demoUserID, demoUserID, ledger.ModeSendCharge, 100)
	_, _ = deps.Engine.CreateTicket(demoUserID, "stranger-filter-agent", approval.TicketPayment,
		map[string]any{
			"category":     "商务合作",
			"content":      "我们做企业 IM 一体化方案，希望就 Agent 接入层沟通 20 分钟",
			"amountCents":  100,
			"billingMode":   int(ledger.ModeSendCharge),
			"strangerAgent": "biz-agent",
			"messageId":     messageID,
			"isStranger":    true,
		},
		time.Now().Add(30*time.Minute))

	_ = orch // orch 供后续审计/协商端点复用
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

// titleOf / contextOf / priceOf 从 Payload 取出清单约定字段（缺失时给零值，不报错）。
func titleOf(t *approval.Ticket) string {
	if s, ok := t.Payload["title"].(string); ok {
		return s
	}
	return string(t.Type)
}

func contextOf(t *approval.Ticket) string {
	if s, ok := t.Payload["context"].(string); ok {
		return s
	}
	return ""
}

func priceOf(t *approval.Ticket) int {
	switch v := t.Payload["price"].(type) {
	case float64:
		return int(v)
	case int:
		return v
	}
	return 0
}

// auditEntries 从内存审计 sink 取出全部事件（运维界面用）。
func auditEntries(deps *api.Deps) []map[string]any {
	sink, ok := deps.Store.Audit.(*audit.MemorySink)
	if !ok {
		return []map[string]any{}
	}
	evs := sink.Entries()
	out := make([]map[string]any, 0, len(evs))
	for _, e := range evs {
		out = append(out, map[string]any{
			"at":        e.At,
			"messageId": e.MessageID,
			"userId":    e.UserID,
			"agentId":   e.AgentID,
			"action":    string(e.Action),
			"decision":  e.Decision,
			"detail":    e.Detail,
		})
	}
	return out
}

// withAdminAuth 对 /api/admin/* 和 /admin/ 路径加 Bearer Token 校验。
// 其他路径（业务 API、healthz、静态前端）保持公开。
func withAdminAuth(next http.Handler, token string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/admin/") || strings.HasPrefix(r.URL.Path, "/admin/") {
			auth := r.Header.Get("Authorization")
			if auth != "Bearer "+token {
				writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "unauthorized"})
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// localDraft 无 LLM 时的本地模板草稿（demo 降级路径）。
func localDraft(category, content, tone string) string {
	switch tone {
	case "brief":
		return "你好，收到。请先发一份简短的方案摘要到邮箱，我看完后再约时间。"
	case "decline":
		return "你好，目前我们的 IM 一体化方案方向暂不匹配，谢谢联系，祝好。"
	default: // polite
		return fmt.Sprintf("你好，收到关于「%s」的合作意向。麻烦先简单介绍下你们的产品形态和已落地案例，我评估后再回复是否安排沟通。", category)
	}
}

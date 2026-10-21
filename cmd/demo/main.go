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
	"time"

	"im2agent/internal/api"
	"im2agent/internal/approval"
	"im2agent/internal/audit"
	"im2agent/internal/id"
	"im2agent/internal/ledger"
	"im2agent/internal/trustzone"
)

//go:embed web
var webFS embed.FS

// demo 用户 ID（本人）。
const demoUserID uint64 = 100

func main() {
	addr := flag.String("addr", ":8080", "监听地址")
	flag.Parse()

	deps, err := api.NewDeps("", "")
	if err != nil {
		log.Fatalf("init deps: %v", err)
	}
	orch := api.NewOrchestrator(*deps)

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

	// demo 聚合状态（前端轮询）
	top.HandleFunc("GET /api/admin/state", func(w http.ResponseWriter, r *http.Request) {
		all, _ := deps.Store.Approval.All()
		pending := make([]map[string]any, 0, len(all))
		for _, t := range all {
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
		writeJSON(w, http.StatusOK, map[string]any{
			"userId":   demoUserID,
			"pending":  pending,
			"serverAt":  time.Now(),
		})
	})

	// 健康检查
	top.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "app": "im2agent-demo"})
	})

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

	// 静态前端（挂根子树；/api/... 与 /healthz 由更具体的注册优先匹配）
	sub, _ := fs.Sub(webFS, "web")
	top.Handle("/", http.FileServer(http.FS(sub)))

	// 运维界面 /admin/
	adminSub, _ := fs.Sub(webFS, "web/admin")
	top.Handle("/admin/", http.StripPrefix("/admin/", http.FileServer(http.FS(adminSub))))

	log.Printf("IM2Agent demo listening on http://localhost%s", *addr)
	log.Printf("  本人 userID=%d；家庭 Agent 已授权 L1，物业 Agent 已授权 L2", demoUserID)
	log.Printf("  已 seed：1 条家庭采购审批 + 1 条陌生人付费来意")
	if err := http.ListenAndServe(*addr, top); err != nil {
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

var _ = fmt.Sprintf

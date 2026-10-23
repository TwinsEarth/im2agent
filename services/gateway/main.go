// gateway：统一 API 网关（v0.5.0）
//
// 把 7 个服务的 HTTP API 合并到一个端口，统一路由、认证、限流。
// 运行：go run ./services/gateway -port 8080
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"

	_ "github.com/jackc/pgx/v5/stdlib"

	"human-agent-im/internal/push"
	"human-agent-im/internal/api"
	"human-agent-im/internal/auth"
	"human-agent-im/internal/ratelimit"
	"human-agent-im/internal/security"
	"human-agent-im/internal/masking"
	"human-agent-im/internal/version"
)

func main() {
	port := flag.Int("port", 8080, "listen port")
	selftest := flag.Bool("selftest", false, "run self-test")
	flag.Parse()

	if *selftest {
		runSelfTest()
		return
	}

	deps, err := api.NewDepsFromEnv()
	if err != nil {
		log.Fatalf("deps: %v", err)
	}

	mux := http.NewServeMux()

	// 健康检查（不经过认证/限流）
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		api.HealthHandler("gateway")(w, r)
	})

	// 根路径：欢迎页（不经过认证/限流），方便用户直接打开 IP 验证
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		json.NewEncoder(w).Encode(map[string]any{
			"service": "IM2A Gateway",
			"status":  "ok",
			"version": version.Version,
			"commit":  version.GitCommit,
			"docs":    "/healthz, /v1/events, /v1/trust/, /v1/approvals/, /v1/negotiations/, /v1/paid/, /v1/compliance/, /v1/msg/, /v1/memory/, /v1/distill/, /v1/model/",
			"auth":    "Bearer token required",
		})
	})

	// 中间件链：认证 → 限流
	token := os.Getenv("GATEWAY_TOKEN")
	auditor := security.New(deps.DB)
	corsAllowed := []string{"https://geefish.cn", "https://www.geefish.cn"}
	if v := os.Getenv("CORS_ALLOWED"); v != "" {
		corsAllowed = strings.Split(v, ",")
	}
	// 中间件链（外→内）：CORS 白名单 → 安全审计落库 → 限流 → 认证 → 安全响应头 → 业务
	wrap := func(h http.Handler) http.Handler {
		h = auth.RequireToken(h, token)
		h = ratelimit.IPLimit(h, 100, 200) // 100 rps, burst 200
		h = auditor.Audit(h)               // 捕获 401/403/429 与正常请求，落 security_events
		h = security.CORS(corsAllowed, h)  // 预检在认证前；非白名单来源拒绝
		h = security.Headers(h)            // 所有响应（含 401/429）均带安全头
		h = security.MaxBody(2<<20, h)     // 统一 2MiB 请求体上限，缓解内存放大 DoS
		return h
	}

	// v1.1.3：通用 WS 端点 /v1/ws?topic=xxx[&access_token=...]（v1.1 审查：纳入认证+限流，修复匿名越权订阅）
	mux.Handle("/v1/ws", wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		topic := r.URL.Query().Get("topic")
		if topic == "" {
			writeJSON(w, 400, map[string]any{"error": "missing topic"})
			return
		}
		deps.Stream.HandleWS(topic, w, r)
	})))
	// v1.1.4：WS 连接统计（需认证）
	mux.Handle("/v1/ws/stats", wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, deps.Stream.Stats())
	})))
	// 事件流查询（经过认证）— 用 deps.Bus（与 memory/distill/model 同源）
	mux.Handle("/v1/events", wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		events := deps.Bus.List()
		json.NewEncoder(w).Encode(map[string]any{"events": events, "count": len(events)})
	})))

	// 各服务路由（按前缀分发，经过中间件）
	mux.Handle("/v1/trust/", wrap(deps.RelationMux("relation-svc")))
	mux.Handle("/v1/agents/", wrap(deps.RelationMux("relation-svc")))
	mux.Handle("/v1/approvals", wrap(deps.ApprovalMux("approval-engine")))
	mux.Handle("/v1/approvals/", wrap(deps.ApprovalMux("approval-engine")))
	mux.Handle("/v1/negotiations", wrap(deps.NegotiationMux("negotiation-svc")))
	mux.Handle("/v1/negotiations/", wrap(deps.NegotiationMux("negotiation-svc")))
	mux.Handle("/v1/a2a/", wrap(deps.A2AMux()))
	mux.Handle("/v1/directory/", wrap(deps.DirectoryMux()))
	mux.Handle("/v1/exp/", wrap(deps.ExperienceMux()))
	mux.Handle("/v1/paid/", wrap(deps.PaymentMux("payment-svc")))
	mux.Handle("/v1/paidflow/", wrap(deps.PaidFlowMux("paid-flow-svc")))
	mux.Handle("/v1/compliance/", wrap(deps.ComplianceMux("compliance-gate")))
	mux.Handle("/v1/msg/", wrap(api.NewMsgMuxWithStore("msg-svc", deps.Store.Message)))

	// Context 记忆（v0.6）：三层记忆 + 子 Agent 隔离
	mux.Handle("/v1/memory/", wrap(deps.MemoryMux("memory-svc")))

	// 蒸馏闭环（v0.7）：审批反馈→阈值自适应
	mux.Handle("/v1/distill/", wrap(deps.DistillMux("distill-svc")))

	// L5 模型层（v0.8）：端侧桩 + 云端兼容
	// v1.2.2：腾讯云服务器到服务器回调不携带用户 Bearer；webhook 走独立 HMAC 验签（?key 或签名头）。
	// 其余 /v1/im/ 接口仍强制用户 Token；整体保留限流。
	imH := deps.IMMux()
	mux.Handle("/v1/im/", ratelimit.IPLimit(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/v1/im/webhook" {
			imH.ServeHTTP(w, r)
			return
		}
		auth.RequireToken(imH, token).ServeHTTP(w, r)
	}), 100, 200))
	// v1.7.4：离线推送管理 API
	pushBase := os.Getenv("TIM_BASE_URL")
	if pushBase == "" {
		pushBase = "https://console.tim.qq.com" // 与 internal/im 默认端点一致，避免未配置时静默空 URL
	}
	deps.PushClient = &push.Client{BaseURL: pushBase, SDKAppID: "1600168829", HTTP: http.DefaultClient}
	mux.Handle("/v1/push/", wrap(deps.PushMux()))
	mux.Handle("/v1/support/", wrap(deps.SupportMux()))
	mux.Handle("/v1/moderation/", wrap(deps.ModerationMux()))
	mux.Handle("/v1/intent/", wrap(deps.LLMIntentMux()))
	mux.Handle("/v1/guard/", wrap(deps.LLMGuardMux()))
	mux.Handle("/v1/distill/feedback", wrap(deps.LLMDistillMux()))
	mux.Handle("/v1/route/", wrap(deps.LLMRouterMux()))
	mux.Handle("/v1/capability/", wrap(deps.CapabilityMux()))
	mux.Handle("/v1/ledger/", wrap(deps.PaidMux()))
	mux.Handle("GET /v1/sse/", wrap(http.HandlerFunc(deps.SSEStream)))
	mux.Handle("POST /v1/sse/publish", wrap(http.HandlerFunc(deps.SSEPublish)))
	mux.Handle("GET /v1/agent/logs", wrap(http.HandlerFunc(deps.AgentLog)))
	mux.Handle("POST /v1/watch/approve", wrap(http.HandlerFunc(deps.WatchApprove)))
	mux.Handle("POST /v1/watch/reject", wrap(http.HandlerFunc(deps.WatchReject)))
	mux.Handle("POST /v1/watch/escalate", wrap(http.HandlerFunc(deps.WatchEscalate)))
	mux.Handle("POST /v1/device/register", wrap(http.HandlerFunc(deps.RegisterDevice)))
	mux.Handle("POST /v1/sync/state", wrap(http.HandlerFunc(deps.SyncState)))
	mux.Handle("GET /v1/sync/state/", wrap(http.HandlerFunc(deps.GetState)))
	mux.Handle("POST /v1/offline/submit", wrap(http.HandlerFunc(deps.OfflineSubmit)))
	mux.Handle("GET /v1/offline/pending", wrap(http.HandlerFunc(deps.OfflineList)))
	mux.Handle("POST /v1/offline/sync", wrap(http.HandlerFunc(deps.OfflineSync)))
	// v1.7 运维
	mux.Handle("GET /v1/admin/metrics", wrap(http.HandlerFunc(deps.AdminMetrics)))
	mux.Handle("GET /v1/admin/audit", wrap(http.HandlerFunc(deps.AdminAudit)))
	mux.Handle("GET /v1/admin/health", wrap(http.HandlerFunc(deps.AdminHealth)))
	// v1.7.6：运营后台总入口——仪表盘真实聚合（独立管理员 Token，缺省回退网关 Token）+ 页面
	adminToken := os.Getenv("IM2A_ADMIN_TOKEN")
	if adminToken == "" { adminToken = token }
	adminH := auth.RequireToken(http.HandlerFunc(deps.AdminMux().ServeHTTP), adminToken)
	mux.Handle("/v1/admin/", adminH)
	mux.Handle("GET /v1/admin/security", auth.RequireToken(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if deps.DB == nil {
			http.Error(w, "db not configured", http.StatusServiceUnavailable)
			return
		}
		list, err := auditor.List(r.Context(), r.URL.Query().Get("type"), 200)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		json.NewEncoder(w).Encode(map[string]any{"events": list})
	}), adminToken))
	mux.Handle("GET /v1/egress/tickets", auth.RequireToken(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if deps.DB == nil {
			http.Error(w, "db not configured", http.StatusServiceUnavailable)
			return
		}
		role := r.URL.Query().Get("role")
		if role == "" {
			role = masking.RoleOther // 缺省最小暴露
		}
		mk := masking.New(role)
		out, n, err := mk.EgressTickets(r.Context(), deps.DB, "gateway:"+mk.Role, "egress_tickets")
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		json.NewEncoder(w).Encode(map[string]any{"role": mk.Role, "fields_masked": n, "tickets": out})
	}), adminToken))
	mux.Handle("/admin/", deps.AdminPage())
	mux.Handle("POST /v1/edge/pipeline", wrap(http.HandlerFunc(deps.EdgePipeline)))
	mux.Handle("GET /v1/edge/egress/report", wrap(http.HandlerFunc(deps.EgressReport)))
	mux.Handle("/v1/model/", wrap(api.ModelMux("model-svc", *deps)))

	// 编排器（特殊路由）
	orch := api.NewOrchestrator(*deps)
	mux.Handle("/v1/agents/register", wrap(orch.Mux("agent-orchestrator")))
	mux.Handle("/v1/gate/", wrap(orch.Mux("agent-orchestrator")))
	mux.Handle("/v1/stranger/", wrap(orch.Mux("agent-orchestrator")))
	mux.Handle("/v1/orchestrator/", wrap(orch.Mux("agent-orchestrator")))
	mux.Handle("/v1/audit", wrap(orch.Mux("agent-orchestrator")))
	mux.Handle("/mcp", wrap(orch.Mux("agent-orchestrator")))
	mux.Handle("/mcp/", wrap(orch.Mux("agent-orchestrator")))
	mux.Handle("/mcp/skills", wrap(orch.Mux("agent-orchestrator")))

	// v1.2.3：启动审批超时升级
	deps.StartApprovalEscalator()

	addr := ":" + itoa(*port)
	log.Printf("gateway listening on %s", addr)
	log.Fatal(http.ListenAndServe(addr, mux))
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	buf := make([]byte, 0, 6)
	for n > 0 {
		buf = append([]byte{byte('0' + n%10)}, buf...)
		n /= 10
	}
	return string(buf)
}

func runSelfTest() {
	log.Println("[gateway] SELF-TEST: 统一 API 网关路由分发（healthz + msg + relation）")
	deps, err := api.NewDepsFromEnv()
	if err != nil {
		log.Fatalf("deps: %v", err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", api.HealthHandler("gateway"))
	mux.Handle("/v1/trust/", deps.RelationMux("relation-svc"))
	mux.Handle("/v1/msg/", api.NewMsgMux("msg-svc"))

	// 模拟请求
	ts := httptest.NewServer(mux)
	defer ts.Close()

	check := func(name, path, method string, body []byte) {
		req, _ := http.NewRequest(method, ts.URL+path, bytes.NewReader(body))
		resp, err := ts.Client().Do(req)
		if err != nil {
			log.Fatalf("[FAIL] %s: %v", name, err)
		}
		resp.Body.Close()
		if resp.StatusCode != 200 {
			log.Fatalf("[FAIL] %s: status=%d", name, resp.StatusCode)
		}
		log.Printf("  [PASS] %s → %d", name, resp.StatusCode)
	}

	check("healthz", "/healthz", "GET", nil)
	check("msg seq", "/v1/msg/seq", "POST", []byte(`{"userId":1}`))
	check("trust grant", "/v1/trust/grant", "POST", []byte(`{"userId":1,"targetId":2,"level":3}`))

	log.Println("[gateway] SELF-TEST PASS: 网关路由分发正常")
}

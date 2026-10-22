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

	"human-agent-im/internal/api"
	"human-agent-im/internal/auth"
	"human-agent-im/internal/ratelimit"
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

	// 事件流查询（经过认证）— 用 deps.Bus（与 memory/distill/model 同源）
	mux.HandleFunc("/v1/events", func(w http.ResponseWriter, r *http.Request) {
		events := deps.Bus.List()
		json.NewEncoder(w).Encode(map[string]any{"events": events, "count": len(events)})
	})

	// 中间件链：认证 → 限流
	token := os.Getenv("GATEWAY_TOKEN")
	wrap := func(h http.Handler) http.Handler {
		h = auth.RequireToken(h, token)
		h = ratelimit.IPLimit(h, 100, 200) // 100 rps, burst 200
		return h
	}

	// 各服务路由（按前缀分发，经过中间件）
	mux.Handle("/v1/trust/", wrap(deps.RelationMux("relation-svc")))
	mux.Handle("/v1/agents/", wrap(deps.RelationMux("relation-svc")))
	mux.Handle("/v1/approvals", wrap(deps.ApprovalMux("approval-engine")))
	mux.Handle("/v1/approvals/", wrap(deps.ApprovalMux("approval-engine")))
	mux.Handle("/v1/negotiations", wrap(deps.NegotiationMux("negotiation-svc")))
	mux.Handle("/v1/negotiations/", wrap(deps.NegotiationMux("negotiation-svc")))
	mux.Handle("/v1/paid/", wrap(deps.PaymentMux("payment-svc")))
	mux.Handle("/v1/paidflow/", wrap(deps.PaidFlowMux("paid-flow-svc")))
	mux.Handle("/v1/compliance/", wrap(deps.ComplianceMux("compliance-gate")))
	mux.Handle("/v1/msg/", wrap(api.NewMsgMuxWithStore("msg-svc", deps.Store.Message)))

	// Context 记忆（v0.6）：三层记忆 + 子 Agent 隔离
	mux.Handle("/v1/memory/", wrap(deps.MemoryMux("memory-svc")))

	// 蒸馏闭环（v0.7）：审批反馈→阈值自适应
	mux.Handle("/v1/distill/", wrap(deps.DistillMux("distill-svc")))

	// L5 模型层（v0.8）：端侧桩 + 云端兼容
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

	addr := ":" + itoa(*port)
	log.Printf("gateway listening on %s", addr)
	log.Fatal(http.ListenAndServe(addr, mux))
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

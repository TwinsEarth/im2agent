package orchestration

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// v2.3.16 D4：端到端 smoke 测试。
// Planner 接口返回原始 JSON，Commander 自己 ParsePlan。

type stubPlanner struct {
	plan Plan
}

func (s *stubPlanner) Plan(ctx context.Context, userText, recentSummary string) (string, error) {
	b, _ := json.Marshal(s.plan)
	return string(b), nil
}

func TestD4_ChatPathNoTool(t *testing.T) {
	cmd := New(&stubPlanner{plan: SafeChatPlan("你好呀")})
	r := NewRunner(cmd, DefaultGate(), NewExitGuard(), NewDistillStats())
	res := r.Run(context.Background(), "你好", "", 0)
	if !res.Allowed || res.NeedApprove {
		t.Fatalf("chat should auto-allow: %+v", res)
	}
	if !strings.Contains(res.Reply, "你好") {
		t.Fatalf("reply wrong: %q", res.Reply)
	}
}

func TestD4_AllowedTool(t *testing.T) {
	p := Plan{
		Activation: ActivationRespond,
		Intent:     IntentTask,
		Confidence: 0.9,
		Subtasks: []SubTask{
			{
				SubAgent: "schedule",
				Goal:     "订会议",
				Steps: []ToolStep{
					{Tool: "calendar.add", Args: map[string]any{"title": "周会"}},
				},
			},
		},
		ReplyHint: "已帮你订好周会。",
	}
	cmd := New(&stubPlanner{plan: p})
	r := NewRunner(cmd, DefaultGate(), NewExitGuard(), NewDistillStats())
	res := r.Run(context.Background(), "帮我订周会", "", 3)
	if !res.Allowed || res.NeedApprove {
		t.Fatalf("schedule.create should be allowed: %+v", res)
	}
	if !strings.Contains(res.Reply, "周会") {
		t.Fatalf("reply: %q", res.Reply)
	}
}

func TestD4_HighRiskDenied(t *testing.T) {
	p := Plan{
		Activation: ActivationRespond,
		Intent:     IntentTask,
		Confidence: 0.9,
		Subtasks: []SubTask{
			{
				SubAgent: "procurement",
				Goal:     "下单",
				Steps: []ToolStep{
					{Tool: "purchase.checkout", Args: map[string]any{"amount": 100}},
				},
			},
		},
	}
	cmd := New(&stubPlanner{plan: p})
	r := NewRunner(cmd, DefaultGate(), NewExitGuard(), NewDistillStats())
	res := r.Run(context.Background(), "买", "", 1)
	if res.Allowed {
		t.Fatalf("high-risk with low trust must be denied: %+v", res)
	}
	if strings.Contains(res.Reply, "wxid") || strings.Contains(res.Reply, "ticket_") {
		t.Fatalf("reply leaks internals: %q", res.Reply)
	}
}

func TestD4_NeedsApprovalOverBudget(t *testing.T) {
	p := Plan{
		Activation: ActivationRespond,
		Intent:     IntentTask,
		Confidence: 0.9,
		Subtasks: []SubTask{
			{
				SubAgent: "procurement",
				Goal:     "买大件",
				Steps: []ToolStep{
					{Tool: "purchase.checkout", Args: map[string]any{"amount": 800}},
				},
			},
		},
	}
	cmd := New(&stubPlanner{plan: p})
	r := NewRunner(cmd, DefaultGate(), NewExitGuard(), NewDistillStats())
	res := r.Run(context.Background(), "买个 800 块的东西", "", 4)
	if !res.NeedApprove || res.Allowed {
		t.Fatalf("over budget should need approval: %+v", res)
	}
	if !strings.Contains(res.Reply, "批准") {
		t.Fatalf("reply should ask approval: %q", res.Reply)
	}
}

func TestD4_StatsAccumulate(t *testing.T) {
	cmd := New(&stubPlanner{plan: SafeChatPlan("hi")})
	stats := NewDistillStats()
	r := NewRunner(cmd, DefaultGate(), NewExitGuard(), stats)
	for i := 0; i < 5; i++ {
		r.Run(context.Background(), "hi", "", 0)
	}
	snap := stats.Snapshot()
	if snap.Total != 5 {
		t.Fatalf("total should be 5, got %+v", snap)
	}
}

func TestD4_NilSafe(t *testing.T) {
	var r *Runner
	res := r.Run(context.Background(), "x", "", 0)
	if res.Reply == "" {
		t.Fatalf("nil runner should return safe text")
	}
}

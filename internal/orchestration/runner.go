package orchestration

import (
	"context"
	"fmt"
)

// v2.3.16 D4：端到端编排 Runner（composition root）。
//
// 前面 v2.3.0–v2.3.15 把所有零件都造好了（Commander / PermissionGate /
// ExitGuard / ProceduralMemory / DistillStats / CommanderMemory），
// 但没有一根管子把它们串起来。这个 Runner 就是那根管子：
//
//	userText
//	   → Commander.Plan（意图路由）
//	   → 遍历 Subtasks，对每个 ToolStep 调 PermissionGate.Check
//	   → 全部 Allow → 合成给用户的回复，过 ExitGuard
//	   → 任意 NeedsApproval → 上抛审批（不直接发）
//	   → 任意 Deny → 友好拒绝
//	   → DistillStats 记录裁决结果
//
// 不依赖 PG / HTTP / 微信，可在单测里完整跑通。

type Runner struct {
	Commander *Commander
	Gate      *PermissionGate
	Guard     *ExitGuard
	Stats     *DistillStats
}

func NewRunner(cmd *Commander, gate *PermissionGate, guard *ExitGuard, stats *DistillStats) *Runner {
	if guard == nil {
		guard = NewExitGuard()
	}
	if stats == nil {
		stats = NewDistillStats()
	}
	return &Runner{Commander: cmd, Gate: gate, Guard: guard, Stats: stats}
}

// RunnerResult 一次编排的结果。
type RunnerResult struct {
	Reply      string // 可以直接发微信的文案
	Allowed    bool   // 是否全部自动放行
	NeedApprove bool  // 是否需要上抛审批
	Reason     string
	Plan       Plan
}

// Run 跑一次编排。trustLevel 是当前用户对该用户的信任档（0–5）。
func (r *Runner) Run(ctx context.Context, userText, recentSummary string, trustLevel int) RunnerResult {
	if r == nil || r.Commander == nil {
		return RunnerResult{Reply: "编排未初始化。", Reason: "runner nil"}
	}
	plan := r.Commander.Plan(ctx, userText, recentSummary)

	res := RunnerResult{Plan: plan, Allowed: true}

	// 纯闲聊/澄清：直接过护栏发回复
	if len(plan.Subtasks) == 0 {
		reply := plan.ReplyHint
		if reply == "" {
			reply = "收到。"
		}
		res.Reply = r.Guard.Sanitize(reply)
		r.Stats.Record("approved")
		return res
	}

	for _, st := range plan.Subtasks {
		for _, step := range st.Steps {
			dec := r.Gate.Check(st.SubAgent, step, trustLevel)
			if !dec.Allow {
				res.Allowed = false
				res.Reason = dec.Reason
				res.Reply = r.Guard.Sanitize(fmt.Sprintf("这件事我还不能直接办：%s", dec.Reason))
				r.Stats.Record("rejected")
				return res
			}
			if dec.NeedsApproval {
				res.NeedApprove = true
				res.Allowed = false
				res.Reason = dec.Reason
				res.Reply = r.Guard.Sanitize(fmt.Sprintf("需要你批准：%s", dec.Reason))
				r.Stats.Record("pending")
				return res
			}
		}
	}
	// 全部放行
	reply := plan.ReplyHint
	if reply == "" {
		reply = "已办。"
	}
	res.Reply = r.Guard.Sanitize(reply)
	r.Stats.Record("approved")
	return res
}

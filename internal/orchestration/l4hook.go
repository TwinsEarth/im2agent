package orchestration

import (
	"context"
	"os"
	"strings"
)

// v2.3.17：L4 灰度接线钩子。
//
// 设计原则：默认 off，对生产零影响；L4_ENABLED=on 时才介入。
// 用法：inqueue worker 在调旧 handler 之前先调 L4Hook.MaybeHandle；
// 返回 handled=true 时按 reply 处理（reply 为空表示静默不回），
// 旧链路不再跑；handled=false 时交回旧链路（由完整 chat 模型回答）。

type L4Hook struct {
	Runner *Runner
	// TrustLevel 默认信任档（生产里按用户查表，先固定 3）。
	TrustLevel int
}

// EnvBool 把 on/1/true/yes 视为开（大小写不敏感），与 main.go 的 "on" 约定对齐。
func EnvBool(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "on", "1", "true", "yes":
		return true
	}
	return false
}

// NewL4Hook 从环境变量读开关。off 或 Runner=nil 时返回 nil（调用方 nil-safe）。
func NewL4Hook(r *Runner) *L4Hook {
	if !EnvBool(os.Getenv("L4_ENABLED")) || r == nil {
		return nil
	}
	return &L4Hook{Runner: r, TrustLevel: 3}
}

// MaybeHandle 走编排管线。
// handled=false（Passthrough）：闲聊/转发/协商/系统/安全降级，交回旧链路，由完整 chat 模型回答。
// handled=true 且 reply 非空：把 reply 发回微信。
// handled=true 且 reply 为空：静默（如群聊未 @），不发也不回旧链路。
func (h *L4Hook) MaybeHandle(ctx context.Context, userText, recentSummary string) (reply string, handled bool) {
	if h == nil || h.Runner == nil {
		return "", false
	}
	res := h.Runner.Run(ctx, userText, recentSummary, h.TrustLevel)
	if res.Passthrough {
		return "", false
	}
	return res.Reply, true
}

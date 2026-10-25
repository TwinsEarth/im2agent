package orchestration

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

type hookPlanner struct{}

func (hookPlanner) Plan(ctx context.Context, s, r string) (string, error) {
	b, _ := json.Marshal(SafeChatPlan("你好呀，L4 已接管"))
	return string(b), nil
}

func TestL4Hook_DefaultOff(t *testing.T) {
	os.Unsetenv("L4_ENABLED")
	h := NewL4Hook(NewRunner(New(hookPlanner{}), DefaultGate(), NewExitGuard(), NewDistillStats()))
	if h != nil {
		t.Fatalf("default off should return nil hook")
	}
}

func TestL4Hook_On(t *testing.T) {
	os.Setenv("L4_ENABLED", "1")
	defer os.Unsetenv("L4_ENABLED")
	h := NewL4Hook(NewRunner(New(hookPlanner{}), DefaultGate(), NewExitGuard(), NewDistillStats()))
	if h == nil {
		t.Fatalf("on should return hook")
	}
	reply, handled := h.MaybeHandle(context.Background(), "hi", "")
	if !handled {
		t.Fatalf("should be handled")
	}
	if !strings.Contains(reply, "L4") {
		t.Fatalf("reply: %q", reply)
	}
}

func TestL4Hook_NilSafe(t *testing.T) {
	var h *L4Hook
	reply, handled := h.MaybeHandle(context.Background(), "x", "")
	if handled || reply != "" {
		t.Fatalf("nil hook must not handle")
	}
}

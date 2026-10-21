package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestChat_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("missing auth header")
		}
		var req ChatRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.Model != "test-model" || len(req.Messages) != 2 {
			t.Errorf("bad request: %+v", req)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]any{"role": "assistant", "content": "这是草稿回复"}},
			},
		})
	}))
	defer srv.Close()

	c, err := New(srv.URL, "test-key", "test-model")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	got, err := c.Chat(context.Background(), ChatRequest{
		Messages: []Message{
			{Role: "system", Content: "你是助手"},
			{Role: "user", Content: "你好"},
		},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if got != "这是草稿回复" {
		t.Errorf("got = %q", got)
	}
}

func TestChat_NotConfigured(t *testing.T) {
	if _, err := New("", "", ""); err != ErrNotConfigured {
		t.Errorf("expected ErrNotConfigured, got %v", err)
	}
	c, _ := New("http://x", "key", "m")
	// 显式置空模拟
	c.APIKey = ""
	if _, err := c.Chat(context.Background(), ChatRequest{}); err != ErrNotConfigured {
		t.Errorf("expected ErrNotConfigured, got %v", err)
	}
}

func TestChat_APIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"message": "bad key"}})
	}))
	defer srv.Close()

	c, _ := New(srv.URL, "wrong", "m")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := c.Chat(ctx, ChatRequest{}); err == nil {
		t.Fatal("expected error, got nil")
	}
}

// v0.5.0-llm：模板档三语气
func TestTemplateDraft(t *testing.T) {
	cases := map[string]string{
		"polite":  "合作意向",
		"brief":   "方案摘要",
		"decline": "暂不匹配",
	}
	for tone, mustContain := range cases {
		got := TemplateDraft(DraftParams{Category: "IM2Agent", Tone: tone})
		if !strings.Contains(got, mustContain) {
			t.Errorf("tone=%s: got %q, want contains %q", tone, got, mustContain)
		}
	}
	// 未知语气回退 polite
	if got := TemplateDraft(DraftParams{Category: "X", Tone: "unknown"}); !strings.Contains(got, "合作意向") {
		t.Errorf("unknown tone should fall back to polite, got %q", got)
	}
}

// v0.5.0-llm：DraftWithFallback 三档
func TestDraftWithFallback_TemplateWhenNil(t *testing.T) {
	draft, via, err := (*Client)(nil).DraftWithFallback(context.Background(), "s", "u", DraftParams{Tone: "brief"})
	if err != nil || via != "template" || !strings.Contains(draft, "方案摘要") {
		t.Errorf("nil client should hit template: via=%s err=%v draft=%q", via, err, draft)
	}
}

func TestDraftWithFallback_FallbackOnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	c, _ := New(srv.URL, "key", "m")
	draft, via, err := c.DraftWithFallback(context.Background(), "s", "u", DraftParams{Tone: "decline"})
	if via != "template_fallback" || err == nil || !strings.Contains(draft, "暂不匹配") {
		t.Errorf("5xx should fall back: via=%s err=%v draft=%q", via, err, draft)
	}
}

func TestDraftWithFallback_LLMSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{"message": map[string]any{"role": "assistant", "content": "好的，下周详谈"}}},
		})
	}))
	defer srv.Close()
	c, _ := New(srv.URL, "key", "m")
	draft, via, err := c.DraftWithFallback(context.Background(), "s", "u", DraftParams{Tone: "polite"})
	if via != "llm" || err != nil || draft != "好的，下周详谈" {
		t.Errorf("llm success: via=%s err=%v draft=%q", via, err, draft)
	}
}

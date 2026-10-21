package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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

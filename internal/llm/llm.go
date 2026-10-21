// Package llm 提供 OpenAI 兼容 Chat Completion 客户端（纯 stdlib）。
//
// 设计目标：
//   - 不引任何第三方 SDK（保持零依赖、离线可编译）；
//   - 兼容 OpenAI / DeepSeek / 通义 / 本地 vLLM / Ollama OpenAI 兼容端点；
//   - 无 API Key 时返回 ErrNotConfigured，调用方优雅降级为 hardcode 草稿；
//   - 超时、重试、错误码统一收敛。
package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

// ErrNotConfigured 未配置 API Key / Base URL，调用方应降级为本地草稿。
var ErrNotConfigured = errors.New("llm: not configured (set OPENAI_API_KEY)")

// Client OpenAI 兼容客户端。
type Client struct {
	BaseURL string // 例如 https://api.openai.com/v1 或 https://api.deepseek.com/v1
	APIKey  string
	Model   string // 例如 gpt-4o-mini / deepseek-chat / qwen-turbo
	HTTP    *http.Client
}

// New 从环境友好的参数构造客户端；apiKey 为空返回 (nil, ErrNotConfigured)。
func New(baseURL, apiKey, model string) (*Client, error) {
	if apiKey == "" {
		return nil, ErrNotConfigured
	}
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}
	if model == "" {
		model = "gpt-4o-mini"
	}
	return &Client{
		BaseURL: baseURL,
		APIKey:  apiKey,
		Model:   model,
		HTTP:    &http.Client{Timeout: 30 * time.Second},
	}, nil
}

// Message chat 消息。
type Message struct {
	Role    string `json:"role"` // system / user / assistant
	Content string `json:"content"`
}

// ChatRequest 请求体。
type ChatRequest struct {
	Model    string    `json:"model"`
	Messages []Message `json:"messages"`
	// Temperature 留零则由服务端默认。
	Temperature float64 `json:"temperature,omitempty"`
	MaxTokens   int     `json:"max_tokens,omitempty"`
}

type chatResponse struct {
	Choices []struct {
		Message Message `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error,omitempty"`
}

// Chat 同步调用一次 chat completion，返回 assistant 文本。
func (c *Client) Chat(ctx context.Context, req ChatRequest) (string, error) {
	if c == nil || c.APIKey == "" {
		return "", ErrNotConfigured
	}
	if req.Model == "" {
		req.Model = c.Model
	}

	body, err := json.Marshal(req)
	if err != nil {
		return "", err
	}

	url := c.BaseURL + "/chat/completions"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.APIKey)

	resp, err := c.HTTP.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("llm: request: %w", err)
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("llm: http %d: %s", resp.StatusCode, string(raw[:min(len(raw), 512)]))
	}

	var cr chatResponse
	if err := json.Unmarshal(raw, &cr); err != nil {
		return "", fmt.Errorf("llm: decode: %w", err)
	}
	if cr.Error != nil {
		return "", fmt.Errorf("llm: api: %s", cr.Error.Message)
	}
	if len(cr.Choices) == 0 {
		return "", errors.New("llm: empty choices")
	}
	return cr.Choices[0].Message.Content, nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

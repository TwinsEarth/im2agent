// Package llm 提供 OpenAI 兼容 Chat Completion 客户端（纯 stdlib）。
//
// 设计目标（v0.5.0-llm 三档降级）：
//   - 不引任何第三方 SDK（保持零依赖、离线可编译）；
//   - 兼容 OpenAI / DeepSeek / 通义 / 本地 vLLM / Ollama OpenAI 兼容端点；
//   - 三档降级：Template（离线 text/template）→ Fallback（超时/5xx 回退模板）→ 真实 LLM；
//   - 真实 LLM 调用 3s 超时，失败立刻回退模板，端到端不卡 UI。
package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"text/template"
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
		// 3s 硬超时：清单 v0.5.0 要求 LLM 慢或 5xx 时立刻回退模板，不卡审批链路
		HTTP: &http.Client{Timeout: 3 * time.Second},
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

// DraftParams 模板档输入。
type DraftParams struct {
	Category string // 来意类别（如"合作""采购""招聘"）
	Content  string // 陌生人原话
	Tone     string // polite / brief / decline
}

// templateByTone 离线模板（完全无 LLM 也能跑）。
var templateByTone = map[string]*template.Template{
	"polite":  template.Must(template.New("polite").Parse("你好，收到关于「{{.Category}}」的合作意向。麻烦先简单介绍下你们的产品形态和已落地案例，我评估后再回复是否安排沟通。")),
	"brief":   template.Must(template.New("brief").Parse("你好，收到。请先发一份简短的方案摘要到邮箱，我看完后再约时间。")),
	"decline": template.Must(template.New("decline").Parse("你好，目前我们的方向暂不匹配，谢谢联系，祝好。")),
}

// TemplateDraft 模板档：完全离线，用 text/template 生成审批/回复卡片文案。
// 这是三档降级的最底档，永远不依赖网络。
func TemplateDraft(p DraftParams) string {
	t := templateByTone[p.Tone]
	if t == nil {
		t = templateByTone["polite"]
	}
	var buf bytes.Buffer
	_ = t.Execute(&buf, p)
	return buf.String()
}

// DraftWithFallback 三档合一：
//   - c 为 nil 或未配置 → 返回 "template" + 模板文案；
//   - Chat 成功 → 返回 "llm" + LLM 文案；
//   - Chat 超时 / 5xx / 网络错 → 返回 "template_fallback" + 模板文案（err 仅作 note）。
//
// 返回值：draft 文案，via（llm/template/template_fallback），err（仅 fallback 时带原因）。
func (c *Client) DraftWithFallback(ctx context.Context, sysPrompt, userPrompt string, p DraftParams) (draft, via string, err error) {
	if c == nil || c.APIKey == "" {
		return TemplateDraft(p), "template", nil
	}
	draft, err = c.Chat(ctx, ChatRequest{
		Messages: []Message{
			{Role: "system", Content: sysPrompt},
			{Role: "user", Content: userPrompt},
		},
		Temperature: 0.3,
		MaxTokens:  200,
	})
	if err != nil {
		return TemplateDraft(p), "template_fallback", err
	}
	return draft, "llm", nil
}

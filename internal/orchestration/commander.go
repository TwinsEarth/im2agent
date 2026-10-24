package orchestration

import (
	"context"
	"encoding/json"
	"strings"
	"time"
)

// Planner 是 Commander 背后的轻量 flash 模型（温度 0）。
// 生产实现用 internal/model.Chat；测试用 stub。
type Planner interface {
	Plan(ctx context.Context, userText, recentSummary string) (string, error)
}

// Commander 只做决策，不执行。
type Commander struct {
	planner Planner
	timeout time.Duration
}

func New(p Planner) *Commander {
	return &Commander{planner: p, timeout: 6 * time.Second}
}

// systemPrompt 给模型的管家指令（不暴露系统标识，输出严格 JSON）。
const systemPrompt = `你是用户的个人行政管家。判断用户这句话并只输出 JSON（不要任何解释/代码块）：
{
 "activation":"respond|ignore",
 "intent":"chat|task|relay|nego_reply|system",
 "confidence":0.0-1.0,
 "need_clarify":true/false,
 "clarify_question":"",
 "subtasks":[{"sub_agent":"schedule|procurement|legal|parenting|property|master","goal":"","deps":[],"steps":[]}],
 "reply_hint":""
}
规则：闲聊/问问题/打招呼→chat；要我替你办事（提醒/采购/报修/约时间）→task；说"发给XX"→relay；花钱/发消息给别人/下单/预约一律 requires_confirmation=true；收件人/时间/金额说不清→need_clarify=true；群里没@你→activation=ignore。`

// Plan 决策入口。任何错误/超时/非法 JSON/低置信都安全降级 SafeChatPlan。
func (c *Commander) Plan(ctx context.Context, userText, recentSummary string) Plan {
	if c == nil || c.planner == nil {
		return SafeChatPlan("")
	}
	callCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	raw, err := c.planner.Plan(callCtx, userText, recentSummary)
	if err != nil {
		return SafeChatPlan("")
	}
	p, ok := ParsePlan(raw)
	if !ok {
		return SafeChatPlan("")
	}
	if p.Confidence < 0.4 {
		return SafeChatPlan(p.ReplyHint)
	}
	return p
}

// ParsePlan 从模型输出中抽 JSON 并校验。
func ParsePlan(raw string) (Plan, bool) {
	raw = strings.TrimSpace(raw)
	// 容忍模型包 ```json ... ``` 或前后废话
	if i := strings.Index(raw, "{"); i >= 0 {
		if j := strings.LastIndex(raw, "}"); j > i {
			raw = raw[i : j+1]
		}
	}
	var p Plan
	dec := json.NewDecoder(strings.NewReader(raw))
	if err := dec.Decode(&p); err != nil {
		return Plan{}, false
	}
	// 基础合法性
	switch p.Intent {
	case IntentChat, IntentTask, IntentRelay, IntentNegoReply, IntentSystem:
	default:
		return Plan{}, false
	}
	if p.Confidence < 0 || p.Confidence > 1 {
		return Plan{}, false
	}
	return p, true
}

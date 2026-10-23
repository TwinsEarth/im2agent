// Package edge v1.4.1: 端侧意图分类（规则可插拔，不出端）。
package edge

import "strings"

// Classifier 意图分类器接口（规则/小模型可插拔）。
type Classifier interface {
	Classify(text string) Intent
}

type Intent struct {
	Category string  // wakeup/chat/command/unknown
	Confidence float64
}

// RuleClassifier 基于关键词的规则分类器（端侧默认）。
type RuleClassifier struct {
	wakeWords []string
}

func NewRuleClassifier() *RuleClassifier {
	return &RuleClassifier{wakeWords: []string{"小爱同学", "嘿助手", "唤醒"}}
}

func (r *RuleClassifier) Classify(text string) Intent {
	t := strings.ToLower(text)
	for _, w := range r.wakeWords {
		if strings.Contains(t, strings.ToLower(w)) {
			return Intent{Category: "wakeup", Confidence: 0.95}
		}
	}
	if strings.Contains(t, "?" ) || strings.Contains(t, "？") {
		return Intent{Category: "chat", Confidence: 0.7}
	}
	if strings.Contains(t, "打开") || strings.Contains(t, "关闭") || strings.Contains(t, "设置") {
		return Intent{Category: "command", Confidence: 0.8}
	}
	return Intent{Category: "unknown", Confidence: 0.3}
}

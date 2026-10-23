// v1.4.0: 端侧智能统一入口——唤醒词 → 意图粗分类 → 敏感前置过滤。
// 所有处理在端侧完成，原始音频不离开设备。
package edge

type Pipeline struct {
	classifier Classifier
	filter     *SensitiveFilter
}

func NewPipeline() *Pipeline {
	return &Pipeline{
		classifier: NewRuleClassifier(),
		filter:     NewSensitiveFilter(),
	}
}

// Result 端侧处理结果。
type Result struct {
	Wakeup      bool
	Category    string
	Confidence  float64
	Blocked     bool
	RedactedText string
	ShouldUpload bool // 只有通过过滤的文本才允许出端
}

func (p *Pipeline) Process(text string) Result {
	intent := p.classifier.Classify(text)
	blocked, redacted := p.filter.Filter(text)
	return Result{
		Wakeup:       intent.Category == "wakeup",
		Category:     intent.Category,
		Confidence:   intent.Confidence,
		Blocked:      blocked,
		RedactedText: redacted,
		ShouldUpload: !blocked && intent.Category != "unknown",
	}
}

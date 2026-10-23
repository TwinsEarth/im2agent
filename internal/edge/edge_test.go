package edge

import "testing"

// v1.4.1: 意图分类——唤醒词/命令/闲聊/未知
func TestV141Intent(t *testing.T) {
	c := NewRuleClassifier()
	if got := c.Classify("嘿助手明天天气").Category; got != "wakeup" {
		t.Errorf("唤醒词应 wakeup, got %s", got)
	}
	if got := c.Classify("打开空调").Category; got != "command" {
		t.Errorf("打开空调应 command, got %s", got)
	}
	if got := c.Classify("你好吗?").Category; got != "chat" {
		t.Errorf("问句应 chat, got %s", got)
	}
	if got := c.Classify("今天天气").Category; got != "unknown" {
		t.Errorf("无关键词应 unknown, got %s", got)
	}
}

// v1.4.2: 敏感词前置过滤
func TestV142Sensitive(t *testing.T) {
	f := NewSensitiveFilter()
	blocked, redacted := f.Filter("我的密码是123456")
	if !blocked {
		t.Error("含密码应拦截")
	}
	if redacted != "我的[已拦截]是123456" {
		t.Errorf("脱敏结果不对: %s", redacted)
	}
	ok, _ := f.Filter("明天开会")
	if ok {
		t.Error("正常文本不应拦截")
	}
}

// v1.4.0: 端侧 pipeline 统一入口
func TestV140Pipeline(t *testing.T) {
	p := NewPipeline()
	// 唤醒词 + 敏感 → 不允许出端
	r := p.Process("嘿助手我的密码是123")
	if !r.Wakeup { t.Error("应识别唤醒词") }
	if !r.Blocked { t.Error("敏感词应拦截") }
	if r.ShouldUpload { t.Error("敏感文本不应出端") }

	// 正常命令 → 允许出端
	r2 := p.Process("打开空调")
	if r2.Category != "command" { t.Errorf("应 command, got %s", r2.Category) }
	if !r2.ShouldUpload { t.Error("正常命令应允许出端") }
}

// v1.4.3: BLE 设备接入
func TestV143BLE(t *testing.T) {
	g := NewBLEGateway()
	d, err := g.Connect("earbud-001", DeviceEarbud)
	if err != nil { t.Fatal(err) }
	if d.Type != DeviceEarbud { t.Error("设备类型不对") }
	if _, err := g.Connect("earbud-001", DeviceEarbud); err == nil {
		t.Error("重复连接应报错")
	}
	g.Connect("glasses-002", DeviceGlasses)
	if len(g.List()) != 2 { t.Errorf("应有 2 设备, got %d", len(g.List())) }
	g.Disconnect("earbud-001")
	if len(g.List()) != 1 { t.Errorf("断开后应剩 1, got %d", len(g.List())) }
}

// v1.4.4: 音频不出端审计
func TestV144Audit(t *testing.T) {
	a := NewAuditLog()
	// 正常文本出端 → 允许
	if !a.Log("api.im2a.chat/v1/intent", false) {
		t.Error("白名单文本请求应允许")
	}
	// 携带音频 → 拒绝
	if a.Log("api.im2a.chat/v1/intent", true) {
		t.Error("音频出端应拒绝")
	}
	// 非白名单 → 拒绝
	if a.Log("evil.com/steal", false) {
		t.Error("非白名单应拒绝")
	}
	total, leaks, violations := a.Report()
	if total != 3 { t.Errorf("总请求应 3, got %d", total) }
	if leaks != 1 { t.Errorf("音频泄露应 1, got %d", leaks) }
	if len(violations) != 2 { t.Errorf("违规应 2, got %d", len(violations)) }
}

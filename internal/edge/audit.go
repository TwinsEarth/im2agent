// v1.4.4: "音频不出端" 可验证证明——出端流量白名单审计。
package edge

import "sync"

// Whitelist 允许出端的目标（只有文本 API，不含音频流）。
var Whitelist = map[string]bool{
	"api.im2a.chat/v1/intent": true,
	"api.im2a.chat/v1/route":  true,
	"api.im2a.chat/v1/guard":  true,
}

type AuditEntry struct {
	Endpoint  string
	HasAudio  bool // 该次出端是否携带音频
	Allowed   bool
}

// AuditLog 记录所有出端请求，证明无音频出端。
type AuditLog struct {
	mu     sync.Mutex
	entries []AuditEntry
}

func NewAuditLog() *AuditLog { return &AuditLog{} }

func (a *AuditLog) Log(endpoint string, hasAudio bool) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	allowed := Whitelist[endpoint] && !hasAudio
	a.entries = append(a.entries, AuditEntry{Endpoint: endpoint, HasAudio: hasAudio, Allowed: allowed})
	return allowed
}

// Report 生成审计报告。
func (a *AuditLog) Report() (total int, audioLeak int, violations []AuditEntry) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, e := range a.entries {
		total++
		if e.HasAudio {
			audioLeak++
		}
		if !e.Allowed {
			violations = append(violations, e)
		}
	}
	return total, audioLeak, violations
}

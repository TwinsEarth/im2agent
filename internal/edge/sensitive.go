// v1.4.2: 端侧敏感词前置过滤——拦截后不出端。
package edge

import "strings"

type SensitiveFilter struct {
	words []string
}

func NewSensitiveFilter() *SensitiveFilter {
	return &SensitiveFilter{words: []string{
		"密码", "身份证", "银行卡", "手机号", "家庭住址", "工资",
	}}
}

// Filter 返回是否拦截 + 脱敏后文本。
func (f *SensitiveFilter) Filter(text string) (blocked bool, redacted string) {
	redacted = text
	for _, w := range f.words {
		if strings.Contains(text, w) {
			blocked = true
			redacted = strings.ReplaceAll(redacted, w, "[已拦截]")
		}
	}
	return blocked, redacted
}

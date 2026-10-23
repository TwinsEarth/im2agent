package im

// 真实腾讯云出站测试：必须 env TIM_SDK_APP_ID / TIM_SECRET_KEY 已配置，否则 skip。
// 运行：TIM_SDK_APP_ID=... TIM_SECRET_KEY=... go test ./internal/im/ -run TestLiveSendC2C -v
import (
	"os"
	"testing"
)

func TestLiveSendC2C(t *testing.T) {
	appID, _ := parseInt64(os.Getenv("TIM_SDK_APP_ID"))
	secret := os.Getenv("TIM_SECRET_KEY")
	if appID == 0 || secret == "" {
		t.Skip("TIM creds not set")
	}
	c := New(Config{SDKAppID: appID, SecretKey: secret, AdminIdentifier: "administrator"})
	// 先给自己发（administrator 作为应用默认管理员账号一般已存在）
	rcpt, err := c.SendC2C(SendMessageReq{
		FromUserID: "administrator",
		ToUserID:   "administrator",
		Text:       "IM2A live e2e ping from v1.2 real TIM channel",
	})
	if err != nil {
		t.Fatalf("SendC2C real call failed: %v", err)
	}
	if rcpt.MsgKey == "" {
		t.Errorf("expected real MsgKey from Tencent Cloud, got empty")
	}
	t.Logf("LIVE OK: got real Tencent MsgKey=%s at=%d", rcpt.MsgKey, rcpt.At)
}

func parseInt64(s string) (int64, error) {
	var n int64
	for _, ch := range s {
		if ch < '0' || ch > '9' {
			return 0, errBad
		}
		n = n*10 + int64(ch-'0')
	}
	return n, nil
}

var errBad = errParse{}

type errParse struct{}

func (errParse) Error() string { return "parse int" }

package im

// 真实腾讯云出站测试：必须 env TIM_SDK_APP_ID / TIM_SECRET_KEY 已配置，否则 skip。
import (
	"os"
	"strconv"
	"testing"
)

func liveClient(t *testing.T) *Client {
	t.Helper()
	appID, err := strconv.ParseInt(os.Getenv("TIM_SDK_APP_ID"), 10, 64)
	if err != nil || appID == 0 {
		t.Skip("TIM creds not set")
	}
	secret := os.Getenv("TIM_SECRET_KEY")
	if secret == "" {
		t.Skip("TIM creds not set")
	}
	return New(Config{SDKAppID: appID, SecretKey: secret, AdminIdentifier: "administrator"})
}

func TestLiveImportAndSendC2C(t *testing.T) {
	c := liveClient(t)
	for _, acc := range []struct{ id, nick string }{
		{"im2a_alice", "IM2A Alice"},
		{"im2a_bob", "IM2A Bob"},
	} {
		if err := c.ImportAccount(acc.id, acc.nick); err != nil {
			t.Fatalf("ImportAccount(%s) failed: %v", acc.id, err)
		}
		t.Logf("imported OK: %s", acc.id)
	}
	rcpt, err := c.SendC2C(SendMessageReq{
		FromUserID: "administrator",
		ToUserID:   "im2a_alice",
		Text:       "IM2A live e2e: admin -> alice via real Tencent Cloud channel",
	})
	if err != nil {
		t.Fatalf("SendC2C admin->alice failed: %v", err)
	}
	if rcpt.MsgKey == "" {
		t.Fatal("expected real MsgKey admin->alice, got empty")
	}
	t.Logf("LIVE OK admin->alice MsgKey=%s at=%d", rcpt.MsgKey, rcpt.At)

	rcpt2, err := c.SendC2C(SendMessageReq{
		FromUserID: "im2a_alice",
		ToUserID:   "im2a_bob",
		Text:       "IM2A live e2e: alice -> bob",
	})
	if err != nil {
		t.Fatalf("SendC2C alice->bob failed: %v", err)
	}
	if rcpt2.MsgKey == "" {
		t.Fatal("expected real MsgKey alice->bob, got empty")
	}
	t.Logf("LIVE OK alice->bob MsgKey=%s", rcpt2.MsgKey)
}

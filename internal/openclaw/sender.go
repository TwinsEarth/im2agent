package openclaw

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// ErrSendFailed openclaw 出站发送失败（deliveryStatus 非 sent 或 CLI 错误）。
var ErrSendFailed = errors.New("openclaw: outbound send failed")

// Sender 通过 openclaw CLI 主动向微信发送消息（IM2A → openclaw → 微信）。
//
// 等价于：
//
//	openclaw message send --channel openclaw-weixin --account <accountId> \
//	  --target <wxid> -m "<text>" --json
//
// IM2A gateway 以 root 运行，HOME=/root 可直接读到 openclaw 的 gateway token 与账号。
type Sender struct {
	bin       string
	channel   string
	home      string
	extraPATH string
	timeout   time.Duration
	dryRun    bool
}

// NewSender 从环境变量读取覆盖项：
// OPENCLAW_BIN（默认 /opt/node-v22.23.4-linux-x64/bin/openclaw）、
// OPENCLAW_HOME（默认 /root）、OPENCLAW_CHANNEL（默认 openclaw-weixin）、
// OPENCLAW_DRY_RUN=1 时只构造 payload 不真实发送（测试用）。
func NewSender() *Sender {
	bin := os.Getenv("OPENCLAW_BIN")
	if bin == "" {
		bin = "/opt/node-v22.23.4-linux-x64/bin/openclaw"
	}
	home := os.Getenv("OPENCLAW_HOME")
	if home == "" {
		home = "/root"
	}
	ch := os.Getenv("OPENCLAW_CHANNEL")
	if ch == "" {
		ch = "openclaw-weixin"
	}
	return &Sender{bin: bin, channel: ch, home: home,
		extraPATH: "/opt/node-v22.23.4-linux-x64/bin", timeout: 45 * time.Second,
		dryRun: os.Getenv("OPENCLAW_DRY_RUN") == "1"}
}

// sendCLIOutput 是 openclaw message send --json 的输出子集。
type sendCLIOutput struct {
	MessageID string `json:"messageId"`
	Payload   struct {
		DeliveryStatus string `json:"deliveryStatus"`
		Result         struct {
			MessageID string `json:"messageId"`
		} `json:"result"`
	} `json:"payload"`
}

// Send 由指定 openclaw 账号（bot）给 targetWxid 发送一条文本消息，
// 成功返回 openclaw messageId；deliveryStatus 非 sent 视为失败。
// dryRun=true 时追加 --dry-run，真实 exec 但不发出微信，返回 "dry-run:<target>"。
func (s *Sender) Send(ctx context.Context, accountID, targetWxid, text string) (string, error) {
	if accountID == "" || targetWxid == "" || strings.TrimSpace(text) == "" {
		return "", fmt.Errorf("%w: account/target/text required", ErrSendFailed)
	}
	if s.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, s.timeout)
		defer cancel()
	}
	args := []string{
		"message", "send",
		"--channel", s.channel,
		"--account", accountID,
		"--target", targetWxid,
		"-m", text,
		"--json",
	}
	if s.dryRun {
		args = append(args, "--dry-run")
	}
	cmd := exec.CommandContext(ctx, s.bin, args...)
	cmd.Env = append(os.Environ(),
		"HOME="+s.home,
		"PATH="+s.extraPATH+":"+os.Getenv("PATH"),
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%w: %v: %s", ErrSendFailed, err, strings.TrimSpace(string(out)))
	}
	raw := strings.TrimSpace(string(out))
	if i := strings.Index(raw, "{"); i >= 0 {
		raw = raw[i:]
	}
	var parsed sendCLIOutput
	if jerr := json.Unmarshal([]byte(raw), &parsed); jerr != nil {
		return "", fmt.Errorf("%w: parse output: %v: %s", ErrSendFailed, jerr, raw)
	}
	if s.dryRun {
		return "dry-run:" + targetWxid, nil
	}
	if parsed.Payload.DeliveryStatus != "sent" {
		return "", fmt.Errorf("%w: deliveryStatus=%s", ErrSendFailed, parsed.Payload.DeliveryStatus)
	}
	mid := parsed.MessageID
	if mid == "" {
		mid = parsed.Payload.Result.MessageID
	}
	if mid == "" {
		return "", fmt.Errorf("%w: empty messageId", ErrSendFailed)
	}
	return mid, nil
}

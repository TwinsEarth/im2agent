package openclaw

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// C3：账号级熔断。errcode=-14 会话/登录过期后，该账号暂停 10 分钟，避免雪崩/被风控。
var (
	pauseMu       sync.Mutex
	pausedUntil   = map[string]time.Time{}
	pauseDuration = 10 * time.Minute
)

// IsPaused C3：账号是否处于熔断暂停中。
func IsPaused(accountID string) bool {
	pauseMu.Lock()
	defer pauseMu.Unlock()
	return time.Now().Before(pausedUntil[accountID])
}

func pauseAccount(accountID string) {
	pauseMu.Lock()
	pausedUntil[accountID] = time.Now().Add(pauseDuration)
	pauseMu.Unlock()
}

// C4：异步补发窗口。同步 10s 超时/瞬时失败的消息进 outbox，后台按退避补发，不丢消息。
type outboxJob struct {
	accountID, targetWxid, text string
}

var (
	outbox     = make(chan outboxJob, 1024)
	outboxOnce sync.Once
)

// D4：出站并发上限 10。每次发送 openclaw CLI 子进程都吃内存，
// 用加权信号量把并发发送压到 10，超出排队，防 OOM、稳定承载。
var sendSem = make(chan struct{}, 10)

func startOutboxWorker() {
	outboxOnce.Do(func() {
		go func() {
			for job := range outbox {
				// 后台补发：最多退避 5 轮，尊重账号熔断
				for i := 0; i < 6; i++ {
					if IsPaused(job.accountID) {
						time.Sleep(30 * time.Second)
						continue
					}
					ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
					_, err := (&Sender{bin: defaultBin(), home: "/root", channel: "openclaw-weixin", extraPATH: "/opt/node-v22.23.4-linux-x64/bin", timeout: 10 * time.Second}).sendOnce(ctx, job.accountID, job.targetWxid, job.text)
					cancel()
					if err == nil {
						break
					}
					if errors.Is(err, ErrTokenExpired) {
						pauseAccount(job.accountID)
					}
					time.Sleep(time.Duration(2<<uint(i)) * time.Second)
				}
			}
		}()
	})
}

func defaultBin() string {
	if b := os.Getenv("OPENCLAW_BIN"); b != "" {
		return b
	}
	return "/opt/node-v22.23.4-linux-x64/bin/openclaw"
}

// ErrSendFailed openclaw 出站发送失败（deliveryStatus 非 sent 或 CLI 错误）。
var ErrSendFailed = errors.New("openclaw: outbound send failed")

// C1：可分类的出站错误，区分"真限流"与"token 失效"，避免盲目重试。
var (
	// ErrTokenExpired context/登录 token 失效（errcode -14/-2 等），需重新登录/扫码，重试无用。
	ErrTokenExpired = errors.New("openclaw: token expired / relogin required")
	// ErrRateLimited 真限流（429/too many），可稍后重试。
	ErrRateLimited = errors.New("openclaw: rate limited")
)

// classifySendError C1：从 CLI 输出识别错误类型。
func classifySendError(out []byte) error {
	s := strings.ToLower(string(out))
	switch {
	case strings.Contains(s, "-14") || strings.Contains(s, "-2") && strings.Contains(s, "token"),
		strings.Contains(s, "token expired"), strings.Contains(s, "login expired"),
		strings.Contains(s, "登录已失效"), strings.Contains(s, "重新登录"),
		strings.Contains(s, "context token"):
		return fmt.Errorf("%w: %s", ErrTokenExpired, strings.TrimSpace(string(out)))
	case strings.Contains(s, "429") || strings.Contains(s, "too many"),
		strings.Contains(s, "rate limit"), strings.Contains(s, "限流"):
		return fmt.Errorf("%w: %s", ErrRateLimited, strings.TrimSpace(string(out)))
	}
	return nil
}

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

// backoffPlan C2：指数退避（2s→4s→8s→16s→30s），瞬时抖动自动恢复；token 失效不重试。
var backoffPlan = []time.Duration{2 * time.Second, 4 * time.Second, 8 * time.Second, 16 * time.Second, 30 * time.Second}

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
		extraPATH: "/opt/node-v22.23.4-linux-x64/bin", timeout: 10 * time.Second, // C4：10s 硬超时，不卡死
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
// sendOnce 单次发送（原逻辑）。
func (s *Sender) sendOnce(ctx context.Context, accountID, targetWxid, text string) (string, error) {
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
	sendSem <- struct{}{}
	defer func() { <-sendSem }()
	cmd := exec.CommandContext(ctx, s.bin, args...)
	cmd.Env = append(os.Environ(),
		"HOME="+s.home,
		"PATH="+s.extraPATH+":"+os.Getenv("PATH"),
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		if ce := classifySendError(out); ce != nil {
			return "", ce
		}
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
		if ce := classifySendError([]byte(raw)); ce != nil {
			return "", ce
		}
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

// Send C2：带指数退避重试。token 失效（ErrTokenExpired）立即返回不重试；
// 其它瞬时错误（网络抖动/限流）按 backoffPlan 退避重试，最多 5 次。
func (s *Sender) Send(ctx context.Context, accountID, targetWxid, text string) (string, error) {
	// C3：账号熔断期直接返回，不发请求。
	if IsPaused(accountID) {
		return "", fmt.Errorf("%w: account paused for %s after session expiry", ErrTokenExpired, pauseDuration)
	}
	var lastErr error
	for attempt := 0; attempt <= len(backoffPlan); attempt++ {
		if attempt > 0 {
			wait := backoffPlan[attempt-1]
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-time.After(wait):
			}
		}
		mid, err := s.sendOnce(ctx, accountID, targetWxid, text)
		if err == nil {
			return mid, nil
		}
		lastErr = err
		// token 失效：熔断该账号 10 分钟，不重试、不进补发
		if errors.Is(err, ErrTokenExpired) {
			pauseAccount(accountID)
			return "", err
		}
	}
	// C4：同步退避仍失败（含 10s 超时）→ 进异步补发窗口，不丢消息、不卡死调用方。
	startOutboxWorker()
	select {
	case outbox <- outboxJob{accountID, targetWxid, text}:
		return "queued:" + targetWxid, nil
	default:
		return "", lastErr
	}
}

// SendTyping C5：发送"正在输入"状态。微信个人号暂无 typing 接口，
// 当前以 B7 的即时占位语承担"正在输入"体感；保留方法便于后续 openclaw 支持时接入。
func (s *Sender) SendTyping(ctx context.Context, accountID, targetWxid string) error {
	// 预留：若 openclaw 后续提供 typing action，在此调用。
	return nil
}

// OutboxStats A7：出站排队/并发/熔断可观测（admin 自查看"当前第几个/预计等待"）。
type OutboxStat struct {
	OutboxDepth    int             `json:"outboxDepth"`
	OutboxCapacity int             `json:"outboxCapacity"`
	ActiveSends    int             `json:"activeSends"`
	MaxConcurrency int             `json:"maxConcurrency"`
	PausedAccounts []PausedAccount `json:"pausedAccounts"`
}

type PausedAccount struct {
	AccountID    string `json:"accountId"`
	Until        string `json:"until"`
	RemainingSec int    `json:"remainingSec"`
}

// OutboxStats 快照当前出站状态（不阻塞发送）。
func SnapshotOutbox() OutboxStat {
	st := OutboxStat{
		OutboxDepth:    len(outbox),
		OutboxCapacity: cap(outbox),
		ActiveSends:    len(sendSem),
		MaxConcurrency: cap(sendSem),
	}
	pauseMu.Lock()
	now := time.Now()
	for acc, until := range pausedUntil {
		if now.Before(until) {
			st.PausedAccounts = append(st.PausedAccounts, PausedAccount{
				AccountID:    acc,
				Until:        until.Format(time.RFC3339),
				RemainingSec: int(time.Until(until).Seconds()),
			})
		}
	}
	pauseMu.Unlock()
	if st.PausedAccounts == nil {
		st.PausedAccounts = []PausedAccount{}
	}
	return st
}

package openclaw

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"errors"
	"io"
	"log"
	"os/exec"
	"regexp"
	"sync"
	"time"
)

var poolQRRe = regexp.MustCompile(`https://liteapp\.weixin\.qq\.com/q/\S+`)

const (
	PoolMaxConcurrency = 10
	PoolSessionTTL     = 5 * time.Minute
	PoolWaitURL       = 30 * time.Second
)

type poolProc struct {
	cancel  context.CancelFunc
	qrURL   string
	started time.Time
}

// Pool 每用户独立 profile + 独立进程，并发上限 15。
type Pool struct {
	mu       sync.Mutex
	procs    map[string]*poolProc
	sem      chan struct{} // 并发信号量
}

func NewPool() *Pool {
	return &Pool{
		procs: make(map[string]*poolProc),
		sem:    make(chan struct{}, PoolMaxConcurrency),
	}
}

// memAvailMB 读取 /proc/meminfo 的 MemAvailable。
func memAvailMB() int {
	f, err := os.Open("/proc/meminfo")
	if err != nil { return 1024 }
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var k string; var v int; var u string
		if _, err := fmt.Sscanf(sc.Text(), "%s %d %s", &k, &v, &u); err == nil && k == "MemAvailable" {
			return v / 1024
		}
	}
	return 1024
}

// Start 为指定 owner 起一个独立 login 子进程，返回二维码 URL。
func (p *Pool) Start(owner string) (string, error) {
	if memAvailMB() < 500 {
		return "", errors.New("openclaw: 内存不足（<500MB），请稍后再试")
	}
	p.mu.Lock()
	if existing, ok := p.procs[owner]; ok && existing.qrURL != "" && time.Since(existing.started) < PoolSessionTTL {
		p.mu.Unlock()
		return existing.qrURL, nil
	}
	p.mu.Unlock()

	select {
	case p.sem <- struct{}{}:
	default:
		return "", errors.New("openclaw: 并发已满（10），请稍后再试")
	}

	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx,
		"/opt/node-v22.23.4-linux-x64/bin/openclaw",
		"channels", "login", "--channel", "openclaw-weixin",
		"--account", "im2a_"+owner,
	)
	cmd.Dir = "/root"
	out, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		<-p.sem
		return "", err
	}
	cmd.Stderr = cmd.Stdout
	if err := cmd.Start(); err != nil {
		cancel()
		<-p.sem
		return "", err
	}

	proc := &poolProc{cancel: cancel, started: time.Now()}
	p.mu.Lock()
	if old, ok := p.procs[owner]; ok {
		old.cancel()
	}
	p.procs[owner] = proc
	p.mu.Unlock()

	urlCh := make(chan string, 1)
	go func() {
		buf := make([]byte, 4096)
		for {
			n, rerr := out.Read(buf)
			if n > 0 {
				if m := poolQRRe.Find(buf[:n]); len(m) > 0 {
					select {
					case urlCh <- string(m):
					default:
					}
				}
			}
			if rerr == io.EOF || ctx.Err() != nil {
				return
			}
		}
	}()

	select {
	case u := <-urlCh:
		proc.qrURL = u
		go func() {
			time.Sleep(PoolSessionTTL)
			p.mu.Lock()
			if p.procs[owner] == proc {
				cancel()
				delete(p.procs, owner)
			}
			p.mu.Unlock()
			<-p.sem
		}()
		return u, nil
	case <-time.After(PoolWaitURL):
		cancel()
		p.mu.Lock()
		delete(p.procs, owner)
		p.mu.Unlock()
		<-p.sem
		return "", errors.New("openclaw: 30s 内未拿到二维码")
	}
}

// Status 返回进程池快照。
func (p *Pool) Status() map[string]any {
	p.mu.Lock()
	defer p.mu.Unlock()
	items := map[string]any{}
	for owner, pr := range p.procs {
		items[owner] = map[string]any{
			"qrUrl":     pr.qrURL,
			"expiresIn": int((PoolSessionTTL - time.Since(pr.started)).Seconds()),
		}
	}
	return map[string]any{
		"active":     len(p.procs),
		"max":        PoolMaxConcurrency,
		"released":   len(p.sem) == 0,
		"profiles":   items,
	}
}

func init() { _ = log.Default }

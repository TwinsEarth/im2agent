package openclaw

import (
	"encoding/json"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// AccountFile 对应 /root/.openclaw/openclaw-weixin/accounts/*.json
type AccountFile struct {
	UserID string `json:"userId"`
	Token  string `json:"token"`
}

// Watcher 扫描 OpenClaw 账号目录，新账号出现时自动回调本地 auto-bind。
type Watcher struct {
	dir     string
	cbURL   string
	seen    map[string]bool
	client  *http.Client
	stopCh  chan struct{}
}

func NewWatcher(dir, cbURL string) *Watcher {
	return &Watcher{
		dir:    dir,
		cbURL:  cbURL,
		seen:   map[string]bool{},
		client: &http.Client{Timeout: 5 * time.Second},
		stopCh: make(chan struct{}),
	}
}

func (w *Watcher) Start() {
	// 启动时先扫一次，把已存在账号标记为已见（避免误触发）
	_ = w.scanOnce()
	go w.loop()
	log.Printf("[openclaw-watcher] watching %s", w.dir)
}

func (w *Watcher) loop() {
	t := time.NewTicker(2 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-w.stopCh:
			return
		case <-t.C:
			_ = w.scan()
		}
	}
}

func (w *Watcher) scanOnce() error {
	entries, err := fs.ReadDir(os.DirFS(w.dir), ".")
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		w.seen[filepath.Join(w.dir, e.Name())] = true
	}
	return nil
}

func (w *Watcher) scan() error {
	entries, err := fs.ReadDir(os.DirFS(w.dir), ".")
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		full := filepath.Join(w.dir, e.Name())
		if w.seen[full] {
			continue
		}
		body, err := os.ReadFile(full)
		if err != nil {
			continue
		}
		var af AccountFile
		if err := json.Unmarshal(body, &af); err != nil || af.UserID == "" {
			continue
		}
		w.seen[full] = true
		log.Printf("[openclaw-watcher] new account %s wxid=%s", e.Name(), af.UserID)
		go w.bind(af.UserID)
	}
	return nil
}

func (w *Watcher) bind(wxid string) {
	payload := `{"wxid":"` + wxid + `","device":"ios","owner":"admin"}`
	req, _ := http.NewRequest("POST", w.cbURL, strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer im2a_demo_token")
	resp, err := w.client.Do(req)
	if err != nil {
		log.Printf("[openclaw-watcher] bind err wxid=%s err=%v", wxid, err)
		return
	}
	defer resp.Body.Close()
	log.Printf("[openclaw-watcher] bind wxid=%s status=%d", wxid, resp.StatusCode)
}

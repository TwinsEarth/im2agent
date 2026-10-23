package api

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"human-agent-im/internal/push"
)

// v1.7.4：推送管理 HTTP API 真实闭环——录凭据/入队/列表/手动重试触发真实出站。
func TestV174_PushAdminAPI(t *testing.T) {
	d := v12Deps(t)
	suf := fmt.Sprintf("%d", time.Now().UnixNano())
	uid := "v174_" + suf

	var calledPath, calledBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		calledPath = r.URL.Path
		calledBody = string(b)
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	d.PushClient = &push.Client{BaseURL: srv.URL, SDKAppID: "1600168829", HTTP: srv.Client()}

	h := d.PushMux()
	do := func(method, path, body string) *httptest.ResponseRecorder {
		var r *http.Request
		if body == "" {
			r = httptest.NewRequest(method, path, nil)
		} else {
			r = httptest.NewRequest(method, path, bytes.NewReader([]byte(body)))
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)
		return rec
	}

	// 1) 录凭据（证书非空）
	rec := do("POST", "/v1/push/credentials",
		fmt.Sprintf(`{"user_id":%q,"platform":"apns","cert_ref":"cert-1","device_token":"tok-1"}`, uid))
	if rec.Code != 200 {
		t.Fatalf("upsert credential code=%d %s", rec.Code, rec.Body.String())
	}
	// 真实落库断言
	var cRef string
	d.DB.QueryRow(`SELECT cert_ref FROM im_push_credentials WHERE user_id=$1 AND platform='apns'`, uid).Scan(&cRef)
	if cRef != "cert-1" {
		t.Fatalf("credential not persisted, got %q", cRef)
	}

	// 2) 入队一条离线任务（真实落库）
	if err := push.Enqueue(context.Background(), d.DB, push.OfflinePushMsg{
		UserID: uid, MsgKey: "mk_" + uid, Title: "审批", Body: "待批",
	}); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	// 3) 列表查到 pending
	rec = do("GET", "/v1/push/tasks?user_id="+uid, "")
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "pending") {
		t.Fatalf("list tasks code=%d body=%s", rec.Code, rec.Body.String())
	}

	// 4) 手动重试 → 真实下发到 fake 腾讯云
	rec = do("POST", "/v1/push/retry?user_id="+uid, "")
	if rec.Code != 200 {
		t.Fatalf("retry code=%d %s", rec.Code, rec.Body.String())
	}
	if calledPath != "/v4/openim/offlinepush" {
		t.Fatalf("fake tencent not hit, path=%q", calledPath)
	}
	if !strings.Contains(calledBody, "OfflinePushInfo") {
		t.Fatalf("outbound body missing OfflinePushInfo: %s", calledBody)
	}

	// 5) 任务状态变 sent
	var status string
	d.DB.QueryRow(`SELECT status FROM im_push_tasks WHERE user_id=$1 ORDER BY id DESC LIMIT 1`, uid).Scan(&status)
	if status != "sent" {
		t.Fatalf("task must be sent after retry, got %q", status)
	}
}

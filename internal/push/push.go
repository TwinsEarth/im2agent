// Package push v1.7.3：腾讯云离线推送。
// 真实逻辑：推送任务落 PG 队列；Dispatch 时按凭据调用腾讯云 IM 离线推送 REST。
// 证书未接入时任务保持 pending 并记录 last_error，绝不 panic、绝不静默丢消息。
package push

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
)

// Credential 推送凭据（无证书时 CertRef 为空，仍可落库占位）。
type Credential struct {
	UserID      string
	Platform    string
	CertRef     string
	DeviceToken string
}

// OfflinePushMsg 待推送内容。
type OfflinePushMsg struct {
	UserID string
	MsgKey string
	Title  string
	Body   string
}

// Enqueue 离线消息落推送队列（pending）。真实落库。
func Enqueue(ctx context.Context, db *sql.DB, m OfflinePushMsg) error {
	_, err := db.ExecContext(ctx,
		`INSERT INTO im_push_tasks(user_id, msg_key, title, body) VALUES($1,$2,$3,$4)`,
		m.UserID, m.MsgKey, m.Title, m.Body)
	return err
}

// Client 腾讯云离线推送 REST 客户端。BaseURL 可注入（测试指向 httptest）。
type Client struct {
	BaseURL  string
	SDKAppID string
	HTTP     *http.Client
}

// SendOfflinePush 真实 HTTP 调用腾讯云离线推送端点。
func (cl *Client) SendOfflinePush(ctx context.Context, cred Credential, m OfflinePushMsg) (*http.Response, error) {
	if cl.BaseURL == "" {
		return nil, fmt.Errorf("push: empty base url")
	}
	payload := map[string]any{
		"UserIDList": []string{m.UserID},
		"MsgKey":     m.MsgKey,
		"OfflinePushInfo": map[string]any{
			"Title": m.Title,
			"Desc":  m.Body,
			"PushInfo": map[string]any{
				"Platform":    cred.Platform,
				"CertRef":     cred.CertRef,
				"DeviceToken": cred.DeviceToken,
			},
		},
	}
	b, _ := json.Marshal(payload)
	url := cl.BaseURL + "/v4/openim/offlinepush?sdkappid=" + cl.SDKAppID
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	return cl.HTTP.Do(req)
}

// DispatchPending 取出 pending 任务，按凭据真实下发；成功置 sent，失败累加 attempts。
func DispatchPending(ctx context.Context, db *sql.DB, cl *Client) (sent, attempted int, err error) {
	rows, err := db.QueryContext(ctx,
		`SELECT id, user_id, msg_key, title, body FROM im_push_tasks WHERE status='pending' ORDER BY id LIMIT 50`)
	if err != nil {
		return 0, 0, err
	}
	defer rows.Close()
	type task struct {
		id                          int64
		userID, msgKey, title, body string
	}
	var tasks []task
	for rows.Next() {
		var t task
		if e := rows.Scan(&t.id, &t.userID, &t.msgKey, &t.title, &t.body); e != nil {
			break
		}
		tasks = append(tasks, t)
	}
	for _, t := range tasks {
		attempted++
		var cred Credential
		cre := db.QueryRowContext(ctx,
			`SELECT user_id, platform, COALESCE(cert_ref,''), COALESCE(device_token,'')
			 FROM im_push_credentials WHERE user_id=$1 ORDER BY platform LIMIT 1`, t.userID)
		if e := cre.Scan(&cred.UserID, &cred.Platform, &cred.CertRef, &cred.DeviceToken); e != nil {
			if _, err := db.ExecContext(ctx,
				`UPDATE im_push_tasks SET attempts=attempts+1, last_error=$2, updated_at=now() WHERE id=$1`,
				t.id, "no_credential"); err != nil {
				log.Printf("push task %d mark no_credential: %v", t.id, err)
			}
			continue
		}
		if cred.CertRef == "" {
			if _, err := db.ExecContext(ctx,
				`UPDATE im_push_tasks SET attempts=attempts+1, last_error=$2, updated_at=now() WHERE id=$1`,
				t.id, "cert_not_configured"); err != nil {
				log.Printf("push task %d mark cert_not_configured: %v", t.id, err)
			}
			continue
		}
		resp, e := cl.SendOfflinePush(ctx, cred, OfflinePushMsg{t.userID, t.msgKey, t.title, t.body})
		if e != nil || resp == nil || resp.StatusCode >= 300 {
			msg := "http_error"
			if resp != nil {
				msg = fmt.Sprintf("http_%d", resp.StatusCode)
			}
			if _, err := db.ExecContext(ctx,
				`UPDATE im_push_tasks SET attempts=attempts+1, last_error=$2, updated_at=now() WHERE id=$1`,
				t.id, msg); err != nil {
				log.Printf("push task %d mark %s: %v", t.id, msg, err)
			}
			log.Printf("push task %d failed: %s", t.id, msg)
			continue
		}
		resp.Body.Close()
		if _, err := db.ExecContext(ctx,
			`UPDATE im_push_tasks SET status='sent', last_error='', updated_at=now() WHERE id=$1`, t.id); err != nil {
			log.Printf("push task %d mark sent: %v", t.id, err)
		}
		sent++
	}
	return sent, attempted, nil
}

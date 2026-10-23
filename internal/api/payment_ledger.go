// v1.5.0: 陌生人付费账本 PG 落账。
package api

import (
	"encoding/json"
	"net/http"
)

type PaidChargeRequest struct {
	MessageID  string `json:"messageId"`
	SenderID   string `json:"senderId"`
	ReceiverID string `json:"receiverId"`
	AmountCents int64 `json:"amountCents"`
	Mode       string `json:"mode"` // send / pay_open
}

// paidCharge 发送即扣费。
func (d *Deps) paidCharge(w http.ResponseWriter, r *http.Request) {
	var req PaidChargeRequest
	json.NewDecoder(r.Body).Decode(&req)
	if req.MessageID == "" || req.SenderID == "" || req.ReceiverID == "" {
		writeErr(w, 400, "messageId/senderId/receiverId required")
		return
	}
	if req.AmountCents <= 0 {
		writeErr(w, 400, "amount must > 0")
		return
	}

	// v1.5.2: 日限额（每个 sender 每天最多 10 次扣费）
	const dailyLimit = 10
	if d.DB != nil {
		var todayCount int
		_ = d.DB.QueryRow(`SELECT COUNT(*) FROM payment_ledger_legacy_v09 WHERE sender_id=$1 AND created_at >= CURRENT_DATE`,
			req.SenderID).Scan(&todayCount)
		if todayCount >= dailyLimit {
			writeErr(w, 429, "daily limit exceeded (10/day)")
			return
		}
	}

	// 幂等：message_id 唯一冲突则返回原结果
	if d.DB != nil {
		var status string
		err := d.DB.QueryRow(`SELECT status FROM payment_ledger_legacy_v09 WHERE message_id=$1`, req.MessageID).Scan(&status)
		if err == nil {
			writeJSON(w, 200, map[string]any{"messageId": req.MessageID, "status": status, "idempotent": true})
			return
		}
		_, err = d.DB.Exec(`INSERT INTO payment_ledger_legacy_v09(message_id, sender_id, receiver_id, amount_cents, mode, status)
			VALUES($1,$2,$3,$4,$5,$6)`,
			req.MessageID, req.SenderID, req.ReceiverID, req.AmountCents, req.Mode, "charged")
		if err != nil {
			writeErr(w, 500, "ledger insert failed: "+err.Error())
			return
		}
	}
	writeJSON(w, 200, map[string]any{"messageId": req.MessageID, "status": "charged", "amountCents": req.AmountCents})
}

// paidList 查账本。
func (d *Deps) paidList(w http.ResponseWriter, r *http.Request) {
	uid := r.URL.Query().Get("userId")
	list := []map[string]any{}
	if d.DB != nil {
		rows, _ := d.DB.Query(`SELECT message_id, sender_id, receiver_id, amount_cents, mode, status, created_at
			FROM payment_ledger_legacy_v09 WHERE sender_id=$1 OR receiver_id=$1 ORDER BY id DESC LIMIT 50`, uid)
		if rows != nil {
			defer rows.Close()
			for rows.Next() {
				var mid, sid, rid, mode, status, ts string
				var amt int64
				rows.Scan(&mid, &sid, &rid, &amt, &mode, &status, &ts)
				list = append(list, map[string]any{
					"messageId": mid, "senderId": sid, "receiverId": rid,
					"amountCents": amt, "mode": mode, "status": status, "createdAt": ts,
				})
			}
		}
	}
	writeJSON(w, 200, map[string]any{"userId": uid, "entries": list, "count": len(list)})
}


// paidOpen v1.5.1: 回复才扣费（pay_open 模式）。
// 收件人查看消息并回复时，才向发送方扣费。
func (d *Deps) paidOpen(w http.ResponseWriter, r *http.Request) {
	var req struct {
		MessageID  string `json:"messageId"`
		ReceiverID string `json:"receiverId"`
		Replied    bool   `json:"replied"`
	}
	json.NewDecoder(r.Body).Decode(&req)
	if req.MessageID == "" || req.ReceiverID == "" {
		writeErr(w, 400, "messageId/receiverId required")
		return
	}
	if !req.Replied {
		// 仅查看未回复，不扣费
		writeJSON(w, 200, map[string]any{"messageId": req.MessageID, "charged": false, "reason": "not replied"})
		return
	}
	// 已回复 → 扣费（从 pending 转 charged）
	if d.DB != nil {
		var sender, receiver string
		var amount int64
		var status string
		err := d.DB.QueryRow(`SELECT sender_id, receiver_id, amount_cents, status FROM payment_ledger_legacy_v09 WHERE message_id=$1`,
			req.MessageID).Scan(&sender, &receiver, &amount, &status)
		if err != nil {
			writeErr(w, 404, "message not found")
			return
		}
		if receiver != req.ReceiverID {
			writeErr(w, 403, "not the receiver")
			return
		}
		if status == "charged" {
			writeJSON(w, 200, map[string]any{"messageId": req.MessageID, "charged": true, "idempotent": true})
			return
		}
		_, _ = d.DB.Exec(`UPDATE payment_ledger_legacy_v09 SET status=$1, mode=$2 WHERE message_id=$3`,
			"charged", "pay_open", req.MessageID)
		writeJSON(w, 200, map[string]any{"messageId": req.MessageID, "charged": true, "amountCents": amount, "mode": "pay_open"})
		return
	}
	writeJSON(w, 200, map[string]any{"messageId": req.MessageID, "charged": true, "mode": "pay_open"})
}


// paidRefund v1.5.3: 退款。
func (d *Deps) paidRefund(w http.ResponseWriter, r *http.Request) {
	var req struct { MessageID string `json:"messageId"`; Reason string `json:"reason"` }
	json.NewDecoder(r.Body).Decode(&req)
	if req.MessageID == "" { writeErr(w, 400, "messageId required"); return }
	if d.DB != nil {
		var status string
		d.DB.QueryRow(`SELECT status FROM payment_ledger_legacy_v09 WHERE message_id=$1`, req.MessageID).Scan(&status)
		if status == "refunded" {
			writeJSON(w, 200, map[string]any{"messageId": req.MessageID, "refunded": true, "idempotent": true})
			return
		}
		_, err := d.DB.Exec(`UPDATE payment_ledger_legacy_v09 SET status=$1 WHERE message_id=$2`, "refunded", req.MessageID)
		if err != nil { writeErr(w, 500, err.Error()); return }
	}
	writeJSON(w, 200, map[string]any{"messageId": req.MessageID, "refunded": true})
}

// blacklistAdd v1.5.3: 加黑名单。
func (d *Deps) blacklistAdd(w http.ResponseWriter, r *http.Request) {
	var req struct { UserID string `json:"userId"`; Reason string `json:"reason"` }
	json.NewDecoder(r.Body).Decode(&req)
	if req.UserID == "" { writeErr(w, 400, "userId required"); return }
	if d.DB != nil {
		_, err := d.DB.Exec(`INSERT INTO blacklist(user_id, reason) VALUES($1,$2) ON CONFLICT (user_id) DO NOTHING`,
			req.UserID, req.Reason)
		if err != nil { writeErr(w, 500, err.Error()); return }
	}
	writeJSON(w, 200, map[string]any{"userId": req.UserID, "blacklisted": true})
}

// PaidMux 付费账本路由。
func (d *Deps) PaidMux() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/ledger/charge", d.paidCharge)
	mux.HandleFunc("GET /v1/ledger/", d.paidList)
	mux.HandleFunc("POST /v1/ledger/open", d.paidOpen)
	mux.HandleFunc("POST /v1/ledger/refund", d.paidRefund)
	mux.HandleFunc("POST /v1/ledger/blacklist", d.blacklistAdd)
	return mux
}

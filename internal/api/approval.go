// approval-engine HTTP 端点：审批单生命周期、事件溯源、超时升级。
package api

import (
	"errors"
	"net/http"
	"time"

	"im2agent/internal/approval"
	"im2agent/internal/audit"
)

// CreateApprovalRequest 创建审批单。
type CreateApprovalRequest struct {
	UserID        uint64         `json:"userId"`
	SourceAgentID string         `json:"sourceAgentId"`
	Type          string         `json:"type"` // NEGOTIATION/PAYMENT/SKILL_CALL/EXTERNAL_ACTION
	Payload       map[string]any `json:"payload"`
	DeadlineUnix  int64          `json:"deadlineUnix"` // 0 用默认 30 分钟
}

// DecideRequest 审批决策。
type DecideRequest struct {
	By    string         `json:"by"`
	Note  string         `json:"note"`
	Patch map[string]any `json:"patch,omitempty"` // modify 时使用
}

// ApprovalMux 挂载 approval-engine 路由。
func (d *Deps) ApprovalMux(serviceName string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", HealthHandler(serviceName))

	mux.HandleFunc("POST /v1/approvals", func(w http.ResponseWriter, r *http.Request) {
		var req CreateApprovalRequest
		if err := decode(r, &req); err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		if req.UserID == 0 || req.SourceAgentID == "" {
			writeErr(w, http.StatusBadRequest, "userId and sourceAgentId required")
			return
		}
		tt := approval.TicketType(req.Type)
		if tt == "" {
			tt = approval.TicketNegotiation
		}
		deadline := time.Now().Add(30 * time.Minute)
		if req.DeadlineUnix > 0 {
			deadline = time.Unix(req.DeadlineUnix, 0)
		}
		t, err := d.Engine.CreateTicket(req.UserID, req.SourceAgentID, tt, req.Payload, deadline)
		if err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		_ = d.Store.Audit.Write(audit.Entry{UserID: req.UserID, AgentID: req.SourceAgentID,
			Action: audit.ActionEscalate, Decision: "CREATED", Detail: map[string]any{"ticketId": t.ID}})
		writeJSON(w, http.StatusCreated, ticketJSON(t))
	})

	mux.HandleFunc("POST /v1/approvals/{id}/approve", func(w http.ResponseWriter, r *http.Request) {
		var req DecideRequest
		_ = decode(r, &req)
		t, err := d.Engine.Approve(r.PathValue("id"), defaultBy(req.By), req.Note)
		if err != nil {
			approvalErr(w, err)
			return
		}
		_ = d.Store.Audit.Write(audit.Entry{UserID: t.UserID, AgentID: t.SourceAgentID,
			MessageID: t.ID, Action: audit.ActionApprove, Decision: "APPROVED"})
		writeJSON(w, http.StatusOK, ticketJSON(t))
	})

	mux.HandleFunc("POST /v1/approvals/{id}/reject", func(w http.ResponseWriter, r *http.Request) {
		var req DecideRequest
		_ = decode(r, &req)
		t, err := d.Engine.Reject(r.PathValue("id"), defaultBy(req.By), req.Note)
		if err != nil {
			approvalErr(w, err)
			return
		}
		_ = d.Store.Audit.Write(audit.Entry{UserID: t.UserID, AgentID: t.SourceAgentID,
			MessageID: t.ID, Action: audit.ActionReject, Decision: "REJECTED"})
		writeJSON(w, http.StatusOK, ticketJSON(t))
	})

	mux.HandleFunc("POST /v1/approvals/{id}/modify", func(w http.ResponseWriter, r *http.Request) {
		var req DecideRequest
		_ = decode(r, &req)
		t, err := d.Engine.Modify(r.PathValue("id"), req.Patch, defaultBy(req.By), req.Note)
		if err != nil {
			approvalErr(w, err)
			return
		}
		writeJSON(w, http.StatusOK, ticketJSON(t))
	})

	mux.HandleFunc("POST /v1/approvals/{id}/complete", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		var req DecideRequest
		_ = decode(r, &req)
		if _, err := d.Engine.BeginExecuting(id, defaultBy(req.By)); err != nil {
			approvalErr(w, err)
			return
		}
		t, err := d.Engine.Complete(id, defaultBy(req.By))
		if err != nil {
			approvalErr(w, err)
			return
		}
		writeJSON(w, http.StatusOK, ticketJSON(t))
	})

	mux.HandleFunc("GET /v1/approvals/pending", func(w http.ResponseWriter, r *http.Request) {
		user := atou(r.URL.Query().Get("user"))
		list, err := d.Engine.ListPending(user)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		out := make([]map[string]any, 0, len(list))
		for _, t := range list {
			out = append(out, ticketJSON(t))
		}
		writeJSON(w, http.StatusOK, map[string]any{"tickets": out})
	})

	mux.HandleFunc("GET /v1/approvals/{id}", func(w http.ResponseWriter, r *http.Request) {
		t, err := d.Engine.Get(r.PathValue("id"))
		if err != nil {
			approvalErr(w, err)
			return
		}
		writeJSON(w, http.StatusOK, ticketJSON(t))
	})

	// 超时扫描：由定时器/运维周期调用，30 分钟→手表，2 小时→短信。
	mux.HandleFunc("POST /v1/approvals/timeouts", func(w http.ResponseWriter, r *http.Request) {
		now := time.Now()
		escalated, err := d.Engine.CheckTimeouts(now)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		out := make([]map[string]any, 0, len(escalated))
		for _, t := range escalated {
			out = append(out, map[string]any{"ticketId": t.ID, "escalation": t.Escalation,
				"note": t.EscalationNote})
		}
		writeJSON(w, http.StatusOK, map[string]any{"escalated": out, "count": len(out)})
	})

	return mux
}

func approvalErr(w http.ResponseWriter, err error) {
	if errors.Is(err, approval.ErrNotFound) {
		writeErr(w, http.StatusNotFound, err.Error())
		return
	}
	if errors.Is(err, approval.ErrInvalidTransition) {
		writeErr(w, http.StatusConflict, err.Error())
		return
	}
	writeErr(w, http.StatusBadRequest, err.Error())
}

func defaultBy(by string) string {
	if by == "" {
		return "user"
	}
	return by
}

func ticketJSON(t *approval.Ticket) map[string]any {
	events := make([]map[string]any, 0, len(t.Events))
	for _, e := range t.Events {
		events = append(events, map[string]any{
			"type": string(e.Type), "by": e.By, "note": e.Note, "atUnix": e.At.Unix(),
		})
	}
	return map[string]any{
		"id": t.ID, "userId": t.UserID, "sourceAgentId": t.SourceAgentID,
		"type": string(t.Type), "state": string(t.State), "payload": t.Payload,
		"escalation": t.Escalation, "escalationNote": t.EscalationNote,
		"createdAtUnix": t.CreatedAt.Unix(), "deadlineUnix": deadlineUnix(t.Deadline),
		"events": events,
	}
}

func deadlineUnix(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.Unix()
}

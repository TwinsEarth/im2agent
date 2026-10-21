// negotiation-svc HTTP 端点：Agent 协商 8 态 Task、轮次/token 熔断。
package api

import (
	"errors"
	"net/http"
	"time"

	"im2agent/internal/negotiation"
	"im2agent/internal/store"
)

// CreateNegotiationRequest 创建协商会话。
type CreateNegotiationRequest struct {
	ID          string `json:"id"`
	FromAgentID string `json:"fromAgentId"`
	ToAgentID   string `json:"toAgentId"`
	MaxRounds   int    `json:"maxRounds,omitempty"`
	TokenBudget uint64 `json:"tokenBudget,omitempty"`
}

// TurnRequest 一轮协商。
type TurnRequest struct {
	FromAgentID string         `json:"fromAgentId"`
	ToAgentID   string         `json:"toAgentId"`
	Intent      string         `json:"intent" // PROPOSE/COUNTER/ACCEPT/REJECT/REQUEST_INFO/REQUEST_AUTH
	Payload     map[string]any `json:"payload"`
	Artifacts   []string       `json:"artifacts,omitempty"`
}

// BindApprovalRequest 绑定审批单（AUTH_REQUIRED 后）。
type BindApprovalRequest struct {
	TicketID string `json:"ticketId"`
}

// NegotiationMux 挂载 negotiation-svc 路由。
func (d *Deps) NegotiationMux(serviceName string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", HealthHandler(serviceName))

	mux.HandleFunc("POST /v1/negotiations", func(w http.ResponseWriter, r *http.Request) {
		var req CreateNegotiationRequest
		if err := decode(r, &req); err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		if req.ID == "" || req.FromAgentID == "" || req.ToAgentID == "" {
			writeErr(w, http.StatusBadRequest, "id/fromAgentId/toAgentId required")
			return
		}
		maxRounds := req.MaxRounds
		if maxRounds <= 0 {
			maxRounds = negotiation.DefaultMaxRounds
		}
		s := negotiation.NewSession(req.ID, maxRounds, req.TokenBudget)
		s.Deadline = time.Now().Add(30 * time.Minute)
		if err := d.Store.Negotiation.SaveSession(r.Context(), s); err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, sessionJSON(s))
	})

	mux.HandleFunc("POST /v1/negotiations/{id}/turns", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		s, err := d.Store.Negotiation.LoadSession(r.Context(), id)
		if err != nil {
			negotiationErr(w, err)
			return
		}
		var req TurnRequest
		if err := decode(r, &req); err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		state, err := s.Apply(negotiation.Turn{
			SessionID: id, FromAgentID: req.FromAgentID, ToAgentID: req.ToAgentID,
			Intent: negotiation.Intent(req.Intent), Payload: req.Payload, ArtifactRefs: req.Artifacts,
			DeadlineUnix: time.Now().Add(30 * time.Minute).Unix(),
		})
		if err != nil {
			// 熔断（轮次/token 预算）：上抛人工，返回 422 但保留会话状态
			_ = d.Store.Negotiation.SaveSession(r.Context(), s)
			writeErr(w, http.StatusUnprocessableEntity, err.Error()+", state="+string(state))
			return
		}
		if err := d.Store.Negotiation.SaveSession(r.Context(), s); err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, sessionJSON(s))
	})

	mux.HandleFunc("POST /v1/negotiations/{id}/bind-approval", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		s, err := d.Store.Negotiation.LoadSession(r.Context(), id)
		if err != nil {
			negotiationErr(w, err)
			return
		}
		var req BindApprovalRequest
		if err := decode(r, &req); err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		s.BindApprovalTicket(req.TicketID)
		if err := d.Store.Negotiation.SaveSession(r.Context(), s); err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, sessionJSON(s))
	})

	mux.HandleFunc("GET /v1/negotiations/{id}", func(w http.ResponseWriter, r *http.Request) {
		s, err := d.Store.Negotiation.LoadSession(r.Context(), r.PathValue("id"))
		if err != nil {
			negotiationErr(w, err)
			return
		}
		writeJSON(w, http.StatusOK, sessionJSON(s))
	})

	return mux
}

func negotiationErr(w http.ResponseWriter, err error) {
	if errors.Is(err, store.ErrNotFound) {
		writeErr(w, http.StatusNotFound, err.Error())
		return
	}
	writeErr(w, http.StatusInternalServerError, err.Error())
}

func sessionJSON(s *negotiation.Session) map[string]any {
	turns := make([]map[string]any, 0, len(s.Turns))
	for _, t := range s.Turns {
		turns = append(turns, map[string]any{
			"from": t.FromAgentID, "to": t.ToAgentID,
			"intent": string(t.Intent), "payload": t.Payload, "artifacts": t.ArtifactRefs,
		})
	}
	return map[string]any{
		"id": s.ID, "state": string(s.State), "round": s.Round, "maxRounds": s.MaxRounds,
		"tokenUsed": s.TokenUsed, "tokenBudget": s.TokenBudget,
		"approvalTicketId": s.ApprovalTicketID, "turns": turns,
	}
}

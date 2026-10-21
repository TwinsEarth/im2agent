// relation-svc HTTP 端点：关系链授权、五档信任门控、Agent 能力声明。
package api

import (
	"net/http"

	"im2agent/internal/audit"
	"im2agent/internal/trustzone"
)

// GrantTrustRequest 授权/变更信任档位。
type GrantTrustRequest struct {
	UserID   uint64 `json:"userId"`
	TargetID uint64 `json:"targetId"`
	Level    int    `json:"level"` // 0..4
}

// GateCheckRequest 能力门控检查。
type GateCheckRequest struct {
	AgentID     string          `json:"agentId"`
	SkillID     string          `json:"skillId"`
	Scope       trustzone.Scope `json:"scope"`
	TrustLevel  int             `json:"trustLevel"`
	AmountCents int64           `json:"amountCents"`
	TargetUID   uint64          `json:"targetUid"`
}

// RelationMux 挂载 relation-svc 的全部路由。
func (d *Deps) RelationMux(serviceName string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", HealthHandler(serviceName))

	mux.HandleFunc("POST /v1/trust/grant", func(w http.ResponseWriter, r *http.Request) {
		var req GrantTrustRequest
		if err := decode(r, &req); err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		if req.Level < 0 || req.Level > 4 || req.UserID == 0 || req.TargetID == 0 {
			writeErr(w, http.StatusBadRequest, "invalid userId/targetId/level")
			return
		}
		if err := d.Store.Relation.GrantTrust(r.Context(), req.UserID, req.TargetID, trustzone.Level(req.Level)); err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	})

	mux.HandleFunc("GET /v1/trust/get", func(w http.ResponseWriter, r *http.Request) {
		var userID, targetID uint64
		// 用查询参数：?user=&target=
		userID = atou(r.URL.Query().Get("user"))
		targetID = atou(r.URL.Query().Get("target"))
		lv, err := d.Store.Relation.GetTrust(r.Context(), userID, targetID)
		if err != nil {
			// 无记录按陌生档返回（4 级），不算错误
			writeJSON(w, http.StatusOK, map[string]any{"level": int(trustzone.LevelStranger), "granted": false})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"level": int(lv), "granted": true})
	})

	mux.HandleFunc("POST /v1/trust/check", func(w http.ResponseWriter, r *http.Request) {
		var req GateCheckRequest
		if err := decode(r, &req); err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		cap, err := d.Store.Capability.GetCapability(r.Context(), req.AgentID)
		if err != nil {
			writeErr(w, http.StatusNotFound, "capability not found: "+req.AgentID)
			return
		}
		dec := cap.Check(trustzone.CallContext{
			SkillID: req.SkillID, Scope: req.Scope, TrustLevel: trustzone.Level(req.TrustLevel),
			AmountCents: req.AmountCents, TargetUID: req.TargetUID,
		})
		// 门控结果入审计
		action := audit.ActionGateAllow
		if !dec.Allowed && !dec.RequiresApproval {
			action = audit.ActionGateDeny
		} else if dec.RequiresApproval {
			action = audit.ActionGateApproval
		}
		_ = d.Store.Audit.Write(audit.Entry{
			AgentID: req.AgentID, Action: action, Decision: dec.String(),
			Detail: map[string]any{"skill": req.SkillID, "scope": string(req.Scope)},
		})
		code := http.StatusOK
		if !dec.Allowed && !dec.RequiresApproval {
			code = http.StatusForbidden
		} else if dec.RequiresApproval {
			code = http.StatusAccepted
		}
		writeJSON(w, code, map[string]any{"decision": dec.String(), "reason": dec.Reason})
	})

	mux.HandleFunc("POST /v1/agents/capabilities", func(w http.ResponseWriter, r *http.Request) {
		var cap trustzone.Capability
		if err := decode(r, &cap); err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		if cap.AgentID == "" {
			writeErr(w, http.StatusBadRequest, "agentId required")
			return
		}
		if err := d.Store.Capability.UpsertCapability(r.Context(), cap); err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "agentId": cap.AgentID})
	})

	return mux
}

func atou(s string) uint64 {
	var n uint64
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0
		}
		n = n*10 + uint64(c-'0')
	}
	return n
}

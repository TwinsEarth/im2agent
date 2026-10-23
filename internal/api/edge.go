package api

import (
	"encoding/json"
	"net/http"

	"human-agent-im/internal/edge"
)

// v1.4 端侧智能接线。
//
// 说明（如实）：真机上端侧管道（唤醒/意图/敏感/出端裁决）应在 Flutter/端侧运行；
// 本服务端端点是开发联调替身，用同一套 internal/edge 逻辑跑，并把每次"出端裁决"
// 真实落 audit_events，从而让"音频不出端"成为可从数据库审计、而非口头声明的承诺。

// 端侧允许回连的 host（与 EgressGuard 白名单一致）。
var edgePipeline = edge.NewPipeline(
	[]string{"api.im2a.chat", "106.53.197.47", "localhost"},
	"https://api.im2a.chat/v1/intent",
)

type edgePipelineReq struct {
	Text string `json:"text"`
}

// EdgePipeline POST /v1/edge/pipeline
// 端侧文本进来：意图粗分类 → 敏感过滤 → 出端字节裁决 → 落 audit_events。
func (d *Deps) EdgePipeline(w http.ResponseWriter, r *http.Request) {
	var req edgePipelineReq
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "bad request body")
		return
	}
	if req.Text == "" {
		writeErr(w, http.StatusBadRequest, "text is required")
		return
	}

	res := edgePipeline.Process(req.Text)
	allowed, reason, body := edgePipeline.Egress(res)

	decision := "allow"
	if !allowed {
		decision = "block"
	}
	detail, _ := json.Marshal(map[string]any{
		"category":       res.Category,
		"blocked":        res.Blocked,
		"hits":           res.Hits,
		"should_upload":  res.ShouldUpload,
		"egress_allowed": allowed,
		"reason":         reason,
		"redacted":       res.RedactedText,
		"body_bytes":     len(body),
	})
	if d.DB != nil {
		// 真实落库：每次端侧出端裁决都可审计
		if _, err := d.DB.Exec(
			`INSERT INTO audit_events(agent_id, action, decision, detail) VALUES($1,$2,$3,$4)`,
			"edge", "edge_egress", decision, detail,
		); err != nil {
			writeErr(w, http.StatusInternalServerError, "audit write failed: "+err.Error())
			return
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"wakeup":         res.Wakeup,
		"category":       res.Category,
		"confidence":     res.Confidence,
		"blocked":        res.Blocked,
		"hits":           res.Hits,
		"should_upload":  res.ShouldUpload,
		"egress_allowed": allowed,
		"reason":         reason,
		"redacted_text":   res.RedactedText,
	})
}

// EgressReport GET /v1/edge/egress/report
// 直接从 audit_events 真实统计"音频不出端"的拦截情况。
func (d *Deps) EgressReport(w http.ResponseWriter, r *http.Request) {
	if d.DB == nil {
		writeErr(w, http.StatusServiceUnavailable, "db not configured")
		return
	}
	var total, blocked int
	if err := d.DB.QueryRow(
		`SELECT COUNT(*), COUNT(*) FILTER (WHERE decision='block')
		   FROM audit_events WHERE action='edge_egress'`,
	).Scan(&total, &blocked); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}

	violations := []map[string]any{}
	rows, err := d.DB.Query(
		`SELECT decision, detail, occurred_at
		   FROM audit_events WHERE action='edge_egress' AND decision='block'
		  ORDER BY occurred_at DESC LIMIT 20`)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var dec, occ string
			var raw json.RawMessage
			_ = rows.Scan(&dec, &raw, &occ)
			violations = append(violations, map[string]any{
				"decision": dec, "detail": raw, "at": occ,
			})
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"total":     total,
		"blocked":   blocked,
		"violations": violations,
		"promise":   "every edge egress decision is persisted; raw audio bytes are rejected by EgressGuard before network.",
	})
}

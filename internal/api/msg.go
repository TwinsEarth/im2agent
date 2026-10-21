// msg-svc HTTP 端点：消息序号生成（64 分片锁 + FNV），运维监控。
package api

import (
	"net/http"

	"im2agent/internal/msgseq"
)

// SeqRequest 单条序号请求。
type SeqRequest struct {
	UserID uint64 `json:"userId"`
}

// SeqBatchRequest 批量序号请求。
type SeqBatchRequest struct {
	UserID uint64 `json:"userId"`
	N      int    `json:"n"`
}

// NewMsgMux 挂载 msg-svc 路由（内含进程内分片生成器）。
func NewMsgMux(serviceName string) http.Handler {
	gen := msgseq.New(1024)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", HealthHandler(serviceName))

	mux.HandleFunc("POST /v1/msg/seq", func(w http.ResponseWriter, r *http.Request) {
		var req SeqRequest
		if err := decode(r, &req); err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		if req.UserID == 0 {
			writeErr(w, http.StatusBadRequest, "userId required")
			return
		}
		seq, err := gen.Next(req.UserID)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"userId": req.UserID, "seq": seq})
	})

	mux.HandleFunc("POST /v1/msg/seq/batch", func(w http.ResponseWriter, r *http.Request) {
		var req SeqBatchRequest
		if err := decode(r, &req); err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		seqs, err := gen.NextBatch(req.UserID, req.N)
		if err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"userId": req.UserID, "seqs": seqs, "count": len(seqs)})
	})

	mux.HandleFunc("GET /v1/msg/stats", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"hotShards": gen.HotShardIDs(),
			"summary":   gen.Summary(),
		})
	})

	return mux
}

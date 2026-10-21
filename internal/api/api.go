// Package api 提供各服务的 HTTP/JSON 业务端点。
//
// 仅用 net/http 标准库（Go 1.22+ 方法路由与路径值）。
// v0.2 在 gRPC 代码生成不可用的离线环境下，先以 HTTP/JSON 等价传输落地服务化；
// proto 契约已在 proto/ 定义，具备 gRPC 环境后 make proto 切换，业务语义不变。
package api

import (
	"encoding/json"
	"net/http"
	"time"

	"im2agent/internal/approval"
	"im2agent/internal/skillruntime"
	"im2agent/internal/store"
)

// Deps 服务共享依赖（按服务注入需要的子集，未用到的字段可空）。
type Deps struct {
	Store  *store.Store
	Skills *skillruntime.Runtime
	Engine *approval.Engine
}

// HealthHandler 健康检查。
func HealthHandler(serviceName string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"service": serviceName, "status": "ok", "time": time.Now().Unix(),
		})
	}
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

// errBody 统一错误响应。
type errBody struct {
	Error string `json:"error"`
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, errBody{Error: msg})
}

// decode 解析请求体（空体允许）。
func decode(r *http.Request, dst any) error {
	if r.Body == nil {
		return nil
	}
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		if err.Error() == "EOF" {
			return nil
		}
		return err
	}
	return nil
}

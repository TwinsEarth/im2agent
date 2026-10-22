// Package auth 提供 HTTP Bearer Token 认证中间件。
//
// v0.5.1 最小实现：静态 token 校验（生产切换到 PostgreSQL + etcd）。
// 用法：
//
//	mux.Handle("/v1/", auth.RequireToken(handler, "secret-token"))
package auth

import (
	"net/http"
	"strings"
)

// RequireToken 校验 Authorization: Bearer <token>。
// token 为空时跳过校验（开发模式）。
func RequireToken(next http.Handler, token string) http.Handler {
	if token == "" {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if auth == "" || !strings.HasPrefix(auth, "Bearer ") {
			http.Error(w, `{"error":"missing bearer token"}`, http.StatusUnauthorized)
			return
		}
		got := strings.TrimPrefix(auth, "Bearer ")
		if got != token {
			http.Error(w, `{"error":"invalid token"}`, http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

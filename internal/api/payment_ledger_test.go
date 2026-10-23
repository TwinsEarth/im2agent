package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// v1.5.0: 付费账本——无 DB 时走内存路径，验证参数校验和幂等响应结构。
func TestV150ChargeValidation(t *testing.T) {
	d := &Deps{} // DB=nil

	// 缺 messageId → 400
	req := httptest.NewRequest("POST", "/", strings.NewReader(`{"senderId":"s","receiverId":"r","amountCents":10}`))
	w := httptest.NewRecorder()
	d.paidCharge(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("缺 messageId 应 400, got %d", w.Code)
	}

	// amount <= 0 → 400
	req2 := httptest.NewRequest("POST", "/", strings.NewReader(`{"messageId":"m1","senderId":"s","receiverId":"r","amountCents":0}`))
	w2 := httptest.NewRecorder()
	d.paidCharge(w2, req2)
	if w2.Code != http.StatusBadRequest {
		t.Errorf("amount=0 应 400, got %d", w2.Code)
	}

	// 正常请求（无 DB 时直接返回 charged）
	req3 := httptest.NewRequest("POST", "/", strings.NewReader(`{"messageId":"m2","senderId":"s","receiverId":"r","amountCents":100}`))
	w3 := httptest.NewRecorder()
	d.paidCharge(w3, req3)
	if !strings.Contains(w3.Body.String(), `"status":"charged"`) {
		t.Errorf("正常请求应 charged, got %s", w3.Body.String())
	}
}

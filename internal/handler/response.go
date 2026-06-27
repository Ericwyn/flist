package handler

import (
	"encoding/json"
	"net/http"
)

// Envelope 是统一响应信封。
type Envelope struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data"`
}

// WriteJSON 写出指定 HTTP 状态码与信封。
func WriteJSON(w http.ResponseWriter, status int, env Envelope) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(env)
}

// OK 写出成功响应（HTTP 200, code 0）。
func OK(w http.ResponseWriter, data any) {
	WriteJSON(w, http.StatusOK, Envelope{Code: 0, Message: "success", Data: data})
}

// Fail 写出错误响应，HTTP 状态码与业务错误码可不同。
func Fail(w http.ResponseWriter, status, code int, message string) {
	WriteJSON(w, status, Envelope{Code: code, Message: message, Data: nil})
}

// decodeJSON 限制请求体大小并解析为目标结构。非上传接口 body ≤ 1MB。
func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	return dec.Decode(dst)
}

package errcode

import (
	"fmt"
	"net/http"
)

// Error 是业务错误：Code 返回给前端，status 决定 HTTP 状态码。
type Error struct {
	Code   int
	Msg    string
	status int
}

func New(code int, msg string, status int) *Error {
	return &Error{Code: code, Msg: msg, status: status}
}

func (e *Error) Error() string {
	return fmt.Sprintf("code=%d msg=%s", e.Code, e.Msg)
}

func (e *Error) HTTPStatus() int { return e.status }

// WithMsg 返回一个替换了提示文案的副本，不修改预定义的错误。
func (e *Error) WithMsg(msg string) *Error {
	return &Error{Code: e.Code, Msg: msg, status: e.status}
}

// 通用错误 1xxxx
var (
	ErrInternal      = New(10000, "服务内部错误", http.StatusInternalServerError)
	ErrInvalidParams = New(10001, "参数错误", http.StatusBadRequest)
	ErrNotFound      = New(10002, "资源不存在", http.StatusNotFound)
	ErrUnauthorized  = New(10003, "未登录或登录已过期", http.StatusUnauthorized)
	ErrForbidden     = New(10004, "没有权限", http.StatusForbidden)
	ErrTooManyReqs   = New(10005, "请求过于频繁", http.StatusTooManyRequests)
	ErrTokenExpired  = New(10006, "登录已过期，请重新登录", http.StatusUnauthorized)
)

// 用户模块 2xxxx
var (
	ErrUserNotFound      = New(20001, "用户不存在", http.StatusNotFound)
	ErrUserExists        = New(20002, "用户名已存在", http.StatusConflict)
	ErrInvalidCredential = New(20003, "用户名或密码错误", http.StatusUnauthorized)
)

// 画布模块 3xxxx
var (
	ErrCanvasNotFound = New(30001, "画布不存在", http.StatusNotFound)
	ErrCanvasConflict = New(30002, "画布已被修改，请刷新后重试", http.StatusConflict)
	ErrCanvasPayload  = New(30003, "画布内容必须是 JSON 对象", http.StatusBadRequest)
	ErrCanvasNoChange = New(30004, "title 和 payload_json 至少传一个", http.StatusBadRequest)
)

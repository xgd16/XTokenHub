// Package resp 提供 Gin 统一响应封装：{code, message, data}。
package resp

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"xtokenhub/internal/pkg/errs"
)

// Envelope 统一响应体。
type Envelope struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// OK 成功响应，data 可为 nil。
func OK(c *gin.Context, data any) {
	c.JSON(http.StatusOK, Envelope{Code: errs.CodeOK, Message: "ok", Data: data})
}

// Page 分页响应结构。
type Page struct {
	Items   any   `json:"items"`
	Total   int64 `json:"total"`
	Page    int   `json:"page"`
	PerPage int   `json:"per_page"`
}

// OKPage 成功的分页响应。
func OKPage(c *gin.Context, items any, total int64, page, perPage int) {
	OK(c, Page{Items: items, Total: total, Page: page, PerPage: perPage})
}

// Fail 按业务错误码返回响应，HTTP 状态保持 200（由 code 区分业务结果），
// 系统级错误（code>=1000 且非业务预期）返回对应 HTTP 状态以便网关/前端处理。
func Fail(c *gin.Context, err error) {
	ae := errs.From(err)
	status := http.StatusOK
	switch ae.Code {
	case errs.CodeInvalidParams:
		status = http.StatusBadRequest
	case errs.CodeNotFound:
		status = http.StatusNotFound
	case errs.CodeInternal, errs.CodeUnknown:
		status = http.StatusInternalServerError
	}
	c.JSON(status, Envelope{Code: ae.Code, Message: ae.Message})
}

// FailMsg 以指定业务码返回失败响应。
func FailMsg(c *gin.Context, code int, message string) {
	c.JSON(http.StatusOK, Envelope{Code: code, Message: message})
}

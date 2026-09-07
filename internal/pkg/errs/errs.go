// Package errs 定义全局业务错误码与错误类型，供各层统一使用。
package errs

import (
	"errors"
	"fmt"
)

// Code 业务错误码。
// 0 表示成功；1xxx 通用；2xxx 渠道；3xxx 网关/上游。
const (
	CodeOK            = 0
	CodeUnknown       = 1000
	CodeInvalidParams = 1001
	CodeNotFound      = 1002
	CodeInternal      = 1003
	CodeNoChannel     = 3001 // 没有可用渠道
	CodeUpstream      = 3002 // 上游返回错误
	CodeConvertFailed = 3003 // 协议转换失败
	CodeProbeFailed   = 3004 // 渠道探测失败
)

// AppError 携带业务错误码的错误，可跨层传递并由 handler 转为统一响应。
type AppError struct {
	Code    int
	Message string
	Err     error // 底层错误，可空
}

func (e *AppError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("code=%d msg=%s: %v", e.Code, e.Message, e.Err)
	}
	return fmt.Sprintf("code=%d msg=%s", e.Code, e.Message)
}

func (e *AppError) Unwrap() error { return e.Err }

// New 构造业务错误。
func New(code int, message string) *AppError {
	return &AppError{Code: code, Message: message}
}

// Wrap 在底层错误之上包装业务错误码。
func Wrap(code int, message string, err error) *AppError {
	return &AppError{Code: code, Message: message, Err: err}
}

// From 将任意 error 归一化为 *AppError：已是 AppError 则原样返回，否则按未知错误处理。
func From(err error) *AppError {
	if err == nil {
		return nil
	}
	var ae *AppError
	if errors.As(err, &ae) {
		return ae
	}
	return &AppError{Code: CodeUnknown, Message: err.Error(), Err: err}
}

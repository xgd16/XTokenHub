package provider

import (
	"fmt"

	"xtokenhub/internal/model"
)

type baseURLError struct{ url string }

func (e *baseURLError) Error() string { return fmt.Sprintf("非法 BaseURL: %q", e.url) }

func errInvalidBaseURL(u string) error { return &baseURLError{url: u} }

type unsupportedProtocolError struct{ p model.Protocol }

func (e *unsupportedProtocolError) Error() string {
	return fmt.Sprintf("不支持的协议: %s", e.p)
}

func errUnsupportedProtocol(p model.Protocol) error { return &unsupportedProtocolError{p: p} }

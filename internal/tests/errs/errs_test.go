package errs_test

import (
	"xtokenhub/internal/pkg/errs"

	"errors"
	"fmt"
	"testing"
)

func TestAppErrorError(t *testing.T) {
	tests := []struct {
		name string
		err  *errs.AppError
		want string
	}{
		{"无底层错误", errs.New(errs.CodeNotFound, "记录不存在"), "code=1002 msg=记录不存在"},
		{"含底层错误", errs.Wrap(errs.CodeUpstream, "上游异常", errors.New("timeout")), "code=3002 msg=上游异常: timeout"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.err.Error(); got != tt.want {
				t.Errorf("Error() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFrom(t *testing.T) {
	orig := errs.New(errs.CodeNoChannel, "无可用渠道")

	tests := []struct {
		name string
		in   error
		code int
	}{
		{"nil 返回 nil", nil, -1},
		{"errs.AppError 原样", orig, errs.CodeNoChannel},
		{"包装后的 errs.AppError", fmt.Errorf("outer: %w", orig), errs.CodeNoChannel},
		{"普通错误归一化", errors.New("boom"), errs.CodeUnknown},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := errs.From(tt.in)
			if tt.in == nil {
				if got != nil {
					t.Fatalf("errs.From(nil) = %v, want nil", got)
				}
				return
			}
			if got.Code != tt.code {
				t.Errorf("errs.From(%v).Code = %d, want %d", tt.in, got.Code, tt.code)
			}
		})
	}
}

func TestWrapUnwrap(t *testing.T) {
	base := errors.New("io failure")
	werr := errs.Wrap(errs.CodeInternal, "内部错误", base)
	if !errors.Is(werr, base) {
		t.Error("Unwrap 链断裂: errors.Is(werr, base) = false")
	}
}

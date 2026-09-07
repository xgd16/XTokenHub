// Package logger 基于 log/slog 的结构化日志初始化。
package logger

import (
	"context"
	"io"
	"log/slog"
	"os"
	"strings"
)

// Level 日志级别。
type Level string

const (
	LevelDebug Level = "debug"
	LevelInfo  Level = "info"
	LevelWarn  Level = "warn"
	LevelError Level = "error"
)

// Options 日志初始化选项。
type Options struct {
	Level  Level
	Format string // json | text
	Output io.Writer
}

// Init 初始化全局 slog 默认 logger，返回替换前的旧 logger。
func Init(opts Options) *slog.Logger {
	if opts.Output == nil {
		opts.Output = os.Stdout
	}
	var lvl slog.Level
	switch Level(strings.ToLower(string(opts.Level))) {
	case LevelDebug:
		lvl = slog.LevelDebug
	case LevelWarn:
		lvl = slog.LevelWarn
	case LevelError:
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}
	var h slog.Handler
	hOpts := &slog.HandlerOptions{Level: lvl}
	if strings.EqualFold(opts.Format, "text") {
		h = slog.NewTextHandler(opts.Output, hOpts)
	} else {
		h = slog.NewJSONHandler(opts.Output, hOpts)
	}
	old := slog.Default()
	slog.SetDefault(slog.New(h))
	return old
}

// L 返回带 module 字段的日志便捷入口。
func L(module string) *slog.Logger {
	return slog.Default().With("module", module)
}

// Err 把 error 作为 slog 属性的便捷写法。
func Err(err error) slog.Attr {
	return slog.String("err", err.Error())
}

// Ctx 透传 context 的便捷写法（保持 API 简洁）。
var Ctx = context.Background

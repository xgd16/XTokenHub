package logger_test

import (
	"xtokenhub/internal/pkg/logger"

	"bytes"
	"encoding/json"
	"log/slog"
	"testing"
)

func TestInitJSONAndLevel(t *testing.T) {
	var buf bytes.Buffer
	old := logger.Init(logger.Options{Level: logger.LevelDebug, Format: "json", Output: &buf})
	defer slog.SetDefault(old)

	slog.Debug("hello", "k", "v")
	var rec map[string]any
	if err := json.Unmarshal(buf.Bytes(), &rec); err != nil {
		t.Fatalf("输出不是合法 JSON: %v", err)
	}
	if rec["msg"] != "hello" || rec["k"] != "v" {
		t.Errorf("字段缺失: %v", rec)
	}
}

func TestInitLevelFilter(t *testing.T) {
	var buf bytes.Buffer
	old := logger.Init(logger.Options{Level: logger.LevelWarn, Format: "text", Output: &buf})
	defer slog.SetDefault(old)

	slog.Info("被过滤")
	if buf.Len() != 0 {
		t.Errorf("info 不应输出: %q", buf.String())
	}
	slog.Warn("保留", "module", "x")
	if buf.Len() == 0 {
		t.Error("warn 应输出")
	}
}

func TestL(t *testing.T) {
	var buf bytes.Buffer
	old := logger.Init(logger.Options{Level: logger.LevelInfo, Format: "json", Output: &buf})
	defer slog.SetDefault(old)

	logger.L("gateway").Info("test")
	var rec map[string]any
	_ = json.Unmarshal(buf.Bytes(), &rec)
	if rec["module"] != "gateway" {
		t.Errorf("module 字段 = %v", rec["module"])
	}
}

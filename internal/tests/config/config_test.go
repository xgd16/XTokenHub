package config_test

import (
	"xtokenhub/internal/config"

	"os"
	"path/filepath"
	"testing"
)

func writeTemp(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadDefaultsWhenFileMissing(t *testing.T) {
	cfg, err := config.Load("")
	if err != nil {
		t.Fatalf("config.Load(\"\") err = %v", err)
	}
	if cfg.Server.Port != 8080 || cfg.Addr() != "0.0.0.0:8080" {
		t.Errorf("默认值错误: %+v", cfg.Server)
	}
	if cfg.Database.Path != "data/xtokenhub.db" || cfg.Gateway.UpstreamTimeout != 300 {
		t.Errorf("默认值错误: %+v / %+v", cfg.Database, cfg.Gateway)
	}
	if !cfg.Gateway.RequireKey {
		t.Error("gateway.require_key 默认应为 true")
	}
}

func TestLoadFromYAML(t *testing.T) {
	path := writeTemp(t, `
server:
  port: 9090
  mode: release
database:
  path: /tmp/x.db
gateway:
  upstream_timeout: 60
`)
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Server.Port != 9090 || cfg.Server.Mode != "release" || cfg.Addr() != "0.0.0.0:9090" {
		t.Errorf("yaml 未生效: %+v", cfg.Server)
	}
	if cfg.Database.Path != "/tmp/x.db" || cfg.Gateway.UpstreamTimeout != 60 {
		t.Errorf("yaml 未生效: %+v", cfg)
	}
	// 未覆盖的字段保持默认
	if cfg.WS.PingInterval != 30 {
		t.Errorf("默认值丢失: ws.ping_interval=%d", cfg.WS.PingInterval)
	}
}

func TestEnvOverride(t *testing.T) {
	path := writeTemp(t, "server:\n  port: 9090\n")
	t.Setenv("XT_HUB_SERVER__PORT", "7777")
	t.Setenv("XT_HUB_DATABASE__PATH", "/tmp/env.db")
	t.Setenv("XT_HUB_GATEWAY__REQUIRE_KEY", "false")

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Server.Port != 7777 {
		t.Errorf("env 覆盖失败: port=%d", cfg.Server.Port)
	}
	if cfg.Database.Path != "/tmp/env.db" {
		t.Errorf("env 覆盖失败: path=%s", cfg.Database.Path)
	}
	if cfg.Gateway.RequireKey {
		t.Error("env 覆盖失败: require_key 应为 false")
	}
}

func TestValidateErrors(t *testing.T) {
	if _, err := config.Load(writeTemp(t, "server:\n  port: 0\n")); err == nil {
		t.Error("port=0 应报错")
	}
	if _, err := config.Load(writeTemp(t, "server:\n  port: 8080\ndatabase:\n  path: \"\"\n")); err == nil {
		t.Error("path 为空应报错")
	}
	if _, err := config.Load(filepath.Join(t.TempDir(), "nope.yaml")); err == nil {
		t.Error("文件不存在应报错")
	}
}

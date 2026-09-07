// Package config 基于 Viper 的配置加载：默认值 <- yaml 文件 <- 环境变量覆盖。
package config

import (
	"fmt"
	"strings"

	"github.com/spf13/viper"
)

// Config 全局配置。
type Config struct {
	Server   ServerConfig   `mapstructure:"server"`
	Database DatabaseConfig `mapstructure:"database"`
	Log      LogConfig      `mapstructure:"log"`
	WS       WSConfig       `mapstructure:"ws"`
	Gateway  GatewayConfig  `mapstructure:"gateway"`
}

type ServerConfig struct {
	Host string `mapstructure:"host"`
	Port int    `mapstructure:"port"`
	Mode string `mapstructure:"mode"` // debug | release | test (gin)
}

type DatabaseConfig struct {
	// Driver 固定 sqlite（纯 Go 驱动）。
	Driver string `mapstructure:"driver"`
	// Path SQLite 文件路径，":memory:" 用于测试。
	Path string `mapstructure:"path"`
	// EnableLog 开启 GORM SQL 日志。
	EnableLog bool `mapstructure:"enable_log"`
}

type LogConfig struct {
	Level  string `mapstructure:"level"`  // debug|info|warn|error
	Format string `mapstructure:"format"` // json|text
}

type WSConfig struct {
	// PingInterval 秒，服务端 ping 间隔。
	PingInterval int `mapstructure:"ping_interval"`
	// WriteTimeout 秒，单次写超时。
	WriteTimeout int `mapstructure:"write_timeout"`
}

type GatewayConfig struct {
	// UpstreamTimeout 秒，非流式上游请求超时。
	UpstreamTimeout int `mapstructure:"upstream_timeout"`
	// MaxBodyBytes 网关请求体上限。
	MaxBodyBytes int64 `mapstructure:"max_body_bytes"`
	// RequireKey 网关是否强制校验 API Key（Authorization: Bearer / x-api-key）。
	RequireKey bool `mapstructure:"require_key"`
}

// Load 读取配置：priority env( XT_HUB_ 前缀 ) > yaml > 内置默认值。
func Load(path string) (*Config, error) {
	v := viper.New()
	setDefaults(v)

	if path != "" {
		v.SetConfigFile(path)
		v.SetConfigType("yaml")
		if err := v.ReadInConfig(); err != nil {
			return nil, fmt.Errorf("读取配置文件 %s: %w", path, err)
		}
	}

	// 环境变量覆盖：XT_HUB_SERVER__PORT -> server.port
	v.SetEnvPrefix("XT_HUB")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "__"))
	v.AutomaticEnv()

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("解析配置: %w", err)
	}
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func setDefaults(v *viper.Viper) {
	v.SetDefault("server.host", "0.0.0.0")
	v.SetDefault("server.port", 8080)
	v.SetDefault("server.mode", "debug")
	v.SetDefault("database.driver", "sqlite")
	v.SetDefault("database.path", "data/xtokenhub.db")
	v.SetDefault("database.enable_log", false)
	v.SetDefault("log.level", "info")
	v.SetDefault("log.format", "json")
	v.SetDefault("ws.ping_interval", 30)
	v.SetDefault("ws.write_timeout", 10)
	v.SetDefault("gateway.upstream_timeout", 300)
	v.SetDefault("gateway.max_body_bytes", 20<<20) // 20MB
	v.SetDefault("gateway.require_key", true)
}

func (c *Config) validate() error {
	if c.Server.Port <= 0 || c.Server.Port > 65535 {
		return fmt.Errorf("server.port 非法: %d", c.Server.Port)
	}
	if c.Database.Path == "" {
		return fmt.Errorf("database.path 不能为空")
	}
	return nil
}

// Addr 监听地址。
func (c *Config) Addr() string {
	return fmt.Sprintf("%s:%d", c.Server.Host, c.Server.Port)
}

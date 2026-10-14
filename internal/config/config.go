// Package config 基于 Viper 的配置加载：默认值 <- yaml 文件 <- 环境变量覆盖。
package config

import (
	"fmt"
	"strings"

	"github.com/spf13/viper"
)

// MaxOpenConnsLimit 数据库连接池上限：SQLite 写操作本身串行，连接过多只会增加
// 内存占用与写锁竞争，16 已远超实际需要。
const MaxOpenConnsLimit = 16

// Config 全局配置。
type Config struct {
	Server    ServerConfig    `mapstructure:"server"`
	Database  DatabaseConfig  `mapstructure:"database"`
	Log       LogConfig       `mapstructure:"log"`
	WS        WSConfig        `mapstructure:"ws"`
	Gateway   GatewayConfig   `mapstructure:"gateway"`
	Retention RetentionConfig `mapstructure:"retention"`
	Pricing   PricingConfig   `mapstructure:"pricing"`
	Holiday   HolidayConfig   `mapstructure:"holiday"`
	Billing   BillingConfig   `mapstructure:"billing"`
	Memory    MemoryConfig    `mapstructure:"memory"`
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
	// MaxOpenConns 连接池上限。WAL 模式下读可并发，故 > 1 才能真正并行执行查询；
	// 写仍由 SQLite 串行化，busy_timeout 负责写锁竞争的重试。
	MaxOpenConns int `mapstructure:"max_open_conns"`
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

type RetentionConfig struct {
	// Enabled 是否启用后台定时清理 request_logs。
	Enabled bool `mapstructure:"enabled"`
	// MaxDays 保留最近 N 天日志，超期行自动删除。
	MaxDays int `mapstructure:"max_days"`
	// IntervalHours 清理运行周期（小时）。
	IntervalHours int `mapstructure:"interval_hours"`
	// BatchSize 单批删除行数，分批执行避免长事务。
	BatchSize int `mapstructure:"batch_size"`
	// Vacuum 每次清理生效后执行 VACUUM 回收磁盘空间（独占锁，默认关闭）。
	Vacuum bool `mapstructure:"vacuum"`
}

// PricingConfig 模型价格表配置。
type PricingConfig struct {
	// Enabled 是否启用计价（关闭后价格表不出网、费用恒为 0）。
	Enabled bool `mapstructure:"enabled"`
	// AutoSync 是否按 SyncIntervalHours 定时同步公开价格表。
	AutoSync bool `mapstructure:"auto_sync"`
	// SyncIntervalHours 定时同步周期（小时）。
	SyncIntervalHours int `mapstructure:"sync_interval_hours"`
	// SourceURL 价格表地址，留空使用内置的 LiteLLM 公开价格表。
	SourceURL string `mapstructure:"source_url"`
	// TimeoutSeconds 价格表拉取超时（秒）。
	TimeoutSeconds int `mapstructure:"timeout_seconds"`
}

// HolidayConfig 节假日年历配置（峰谷规则 !H / W 记号的判定依据）。
type HolidayConfig struct {
	// Enabled 是否启用节假日年历（关闭后 !H / W 记号视为无数据，按星期判定）。
	Enabled bool `mapstructure:"enabled"`
	// AutoSync 是否按 SyncIntervalHours 定时同步年历。
	AutoSync bool `mapstructure:"auto_sync"`
	// SyncIntervalHours 同步周期（小时）。节假日安排一年一变，新安排公布后一周内自动拿到。
	SyncIntervalHours int `mapstructure:"sync_interval_hours"`
	// SourceURL 年历接口地址，{year} 为年份占位符；留空使用内置 timor.tech 年历。
	SourceURL string `mapstructure:"source_url"`
	// TimeoutSeconds 年历拉取超时（秒）。
	TimeoutSeconds int `mapstructure:"timeout_seconds"`
}

// BillingConfig 计费展示默认值（仅首次初始化入库，之后以设置页的值为准）。
type BillingConfig struct {
	// DisplayCurrency 默认展示币种：USD | CNY。
	DisplayCurrency string `mapstructure:"display_currency"`
	// USDRate USD -> CNY 汇率，手工配置。
	USDRate float64 `mapstructure:"usd_cny_rate"`
	// MonthlyBudgetUSD 月度预算，0 = 不设（仅用于预测提示，不拦截请求）。
	MonthlyBudgetUSD float64 `mapstructure:"monthly_budget_usd"`
}

// MemoryConfig 记忆库配置（Agent 记忆 API / 控制台 / MCP 共用）。
type MemoryConfig struct {
	// Enabled 是否启用记忆库（关闭后 /memory/v1 与 /mcp 不注册）。
	Enabled bool `mapstructure:"enabled"`
	// RequireKey 记忆 API 与 MCP 是否强制校验 API Key（鉴权头同网关）。
	RequireKey bool `mapstructure:"require_key"`
	// SearchLimitDefault 检索默认返回条数。
	SearchLimitDefault int `mapstructure:"search_limit_default"`
	// SearchLimitMax 检索返回条数上限。
	SearchLimitMax int `mapstructure:"search_limit_max"`
	// ClassifierBatchSize 单次 LLM 分类调用送的记忆条数。
	ClassifierBatchSize int `mapstructure:"classifier_batch_size"`
	// ClassifierTimeoutSeconds 单次分类补全超时（秒）。
	ClassifierTimeoutSeconds int `mapstructure:"classifier_timeout_seconds"`
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
	v.SetDefault("database.max_open_conns", 4)
	v.SetDefault("log.level", "info")
	v.SetDefault("log.format", "json")
	v.SetDefault("ws.ping_interval", 30)
	v.SetDefault("ws.write_timeout", 10)
	v.SetDefault("gateway.upstream_timeout", 300)
	v.SetDefault("gateway.max_body_bytes", 20<<20) // 20MB
	v.SetDefault("gateway.require_key", true)
	v.SetDefault("retention.enabled", true)
	v.SetDefault("retention.max_days", 90)
	v.SetDefault("retention.interval_hours", 24)
	v.SetDefault("retention.batch_size", 1000)
	v.SetDefault("retention.vacuum", false)
	v.SetDefault("pricing.enabled", true)
	v.SetDefault("pricing.auto_sync", true)
	v.SetDefault("pricing.sync_interval_hours", 24)
	v.SetDefault("pricing.source_url", "")
	v.SetDefault("pricing.timeout_seconds", 20)
	v.SetDefault("holiday.enabled", true)
	v.SetDefault("holiday.auto_sync", true)
	v.SetDefault("holiday.sync_interval_hours", 168) // 一周
	v.SetDefault("holiday.source_url", "")
	v.SetDefault("holiday.timeout_seconds", 20)
	v.SetDefault("billing.display_currency", "USD")
	v.SetDefault("billing.usd_cny_rate", 0)
	v.SetDefault("billing.monthly_budget_usd", 0)
	v.SetDefault("memory.enabled", true)
	v.SetDefault("memory.require_key", true)
	v.SetDefault("memory.search_limit_default", 10)
	v.SetDefault("memory.search_limit_max", 50)
	v.SetDefault("memory.classifier_batch_size", 20)
	v.SetDefault("memory.classifier_timeout_seconds", 60)
}

func (c *Config) validate() error {
	if c.Server.Port <= 0 || c.Server.Port > 65535 {
		return fmt.Errorf("server.port 非法: %d", c.Server.Port)
	}
	if c.Database.Path == "" {
		return fmt.Errorf("database.path 不能为空")
	}
	if c.Database.MaxOpenConns < 1 || c.Database.MaxOpenConns > MaxOpenConnsLimit {
		return fmt.Errorf("database.max_open_conns 非法: %d（允许 1~%d）", c.Database.MaxOpenConns, MaxOpenConnsLimit)
	}
	if c.Retention.MaxDays < 1 {
		return fmt.Errorf("retention.max_days 非法: %d", c.Retention.MaxDays)
	}
	if c.Retention.IntervalHours < 1 {
		return fmt.Errorf("retention.interval_hours 非法: %d", c.Retention.IntervalHours)
	}
	if c.Retention.BatchSize < 100 {
		return fmt.Errorf("retention.batch_size 非法: %d", c.Retention.BatchSize)
	}
	if c.Pricing.Enabled {
		if c.Pricing.SyncIntervalHours < 1 {
			return fmt.Errorf("pricing.sync_interval_hours 非法: %d", c.Pricing.SyncIntervalHours)
		}
		if c.Pricing.TimeoutSeconds < 1 {
			return fmt.Errorf("pricing.timeout_seconds 非法: %d", c.Pricing.TimeoutSeconds)
		}
		switch strings.ToUpper(c.Billing.DisplayCurrency) {
		case "USD", "CNY", "":
		default:
			return fmt.Errorf("billing.display_currency 非法: %s", c.Billing.DisplayCurrency)
		}
		if c.Billing.USDRate < 0 {
			return fmt.Errorf("billing.usd_cny_rate 非法: %v", c.Billing.USDRate)
		}
		if c.Billing.MonthlyBudgetUSD < 0 {
			return fmt.Errorf("billing.monthly_budget_usd 非法: %v", c.Billing.MonthlyBudgetUSD)
		}
	}
	if c.Holiday.Enabled {
		if c.Holiday.SyncIntervalHours < 1 {
			return fmt.Errorf("holiday.sync_interval_hours 非法: %d", c.Holiday.SyncIntervalHours)
		}
		if c.Holiday.TimeoutSeconds < 1 {
			return fmt.Errorf("holiday.timeout_seconds 非法: %d", c.Holiday.TimeoutSeconds)
		}
	}
	if c.Memory.Enabled {
		if c.Memory.SearchLimitDefault < 1 || c.Memory.SearchLimitDefault > 200 {
			return fmt.Errorf("memory.search_limit_default 非法: %d（允许 1~200）", c.Memory.SearchLimitDefault)
		}
		if c.Memory.SearchLimitMax < c.Memory.SearchLimitDefault || c.Memory.SearchLimitMax > 500 {
			return fmt.Errorf("memory.search_limit_max 非法: %d（允许 search_limit_default~500）", c.Memory.SearchLimitMax)
		}
		if c.Memory.ClassifierBatchSize < 1 || c.Memory.ClassifierBatchSize > 100 {
			return fmt.Errorf("memory.classifier_batch_size 非法: %d（允许 1~100）", c.Memory.ClassifierBatchSize)
		}
		if c.Memory.ClassifierTimeoutSeconds < 1 {
			return fmt.Errorf("memory.classifier_timeout_seconds 非法: %d", c.Memory.ClassifierTimeoutSeconds)
		}
	}
	return nil
}

// Addr 监听地址。
func (c *Config) Addr() string {
	return fmt.Sprintf("%s:%d", c.Server.Host, c.Server.Port)
}

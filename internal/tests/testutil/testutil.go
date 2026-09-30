// Package testutil 测试共享工具（非测试文件，可被多个测试包导入）。
package testutil

import (
	"context"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"xtokenhub/internal/aggregation"
	"xtokenhub/internal/model"
	"xtokenhub/internal/provider"
	"xtokenhub/internal/repository"
)

// NewMemoryDB 每个用例独享 :memory: SQLite（单连接共享同一库）并完成迁移。
func NewMemoryDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("打开内存库: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("获取底层连接: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)
	if err := db.AutoMigrate(
		&model.Channel{}, &model.APIKey{}, &model.RequestLog{}, &model.CustomModel{},
		&model.SessionHeaderConfig{}, &model.ModelPrice{}, &model.BillingSettings{},
		&model.DailyUsageStat{}, &model.DailyModelStat{}, &model.DataSettings{},
		&model.ReasoningRewrite{}, &model.MimoSettings{},
	); err != nil {
		t.Fatalf("迁移: %v", err)
	}
	return db
}

// SeedDailyStats 用现存明细回填日级聚合表并推进水位线，返回聚合仓储。
// 混合口径统计（Lifetime/TrendByDay/Forecast 等）的完整历史日读聚合表，
// 测试需要先回填才能覆盖跨天数据。需要 data_settings 行存在（播种空行即可）。
func SeedDailyStats(t *testing.T, db *gorm.DB) repository.DailyStatsRepository {
	t.Helper()
	ctx := context.Background()
	daily := repository.NewDailyStatsRepository(db)
	settings := repository.NewDataSettingsRepository(db)
	if err := settings.Save(ctx, &model.DataSettings{ID: 1}); err != nil {
		t.Fatalf("播种数据管理设置: %v", err)
	}
	svc := aggregation.New(daily, settings)
	if _, err := svc.RebuildAll(ctx); err != nil {
		t.Fatalf("回填日级聚合: %v", err)
	}
	return daily
}

// StubPricingSource 固定价格表的价格源桩：Fetch 直接返回预设条目，不发网络请求。
// 结构化满足 service.PricingSource（此处不 import service，避免无谓依赖）。
type StubPricingSource struct {
	Entries []provider.ModelPriceEntry
	// Err 非 nil 时模拟拉取失败。
	Err error
	// Calls 记录 Fetch 调用次数，供断言同步行为。
	Calls int
}

// NewStubPricingSource 构造价格源桩。
func NewStubPricingSource(entries ...provider.ModelPriceEntry) *StubPricingSource {
	return &StubPricingSource{Entries: entries}
}

// Fetch 实现 service.PricingSource。
func (s *StubPricingSource) Fetch(_ context.Context, _ string) ([]provider.ModelPriceEntry, error) {
	s.Calls++
	if s.Err != nil {
		return nil, s.Err
	}
	return s.Entries, nil
}

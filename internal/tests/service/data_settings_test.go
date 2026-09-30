// Package service_test 数据管理设置业务测试：播种幂等、配置校验、
// 配置更新不覆盖后台任务写回的状态字段。
package service_test

import (
	"context"
	"testing"
	"time"

	"gorm.io/gorm"

	"xtokenhub/internal/aggregation"
	"xtokenhub/internal/cleanup"
	"xtokenhub/internal/model"
	"xtokenhub/internal/pkg/errs"
	"xtokenhub/internal/repository"
	"xtokenhub/internal/service"
	"xtokenhub/internal/tests/testutil"
)

func newDataSettingsSvc(t *testing.T) (*service.DataSettingsService, *gorm.DB, repository.DataSettingsRepository) {
	t.Helper()
	db := testutil.NewMemoryDB(t)
	daily := repository.NewDailyStatsRepository(db)
	dataRepo := repository.NewDataSettingsRepository(db)
	agg := aggregation.New(daily, dataRepo)
	retention := cleanup.NewRetention(repository.NewRequestLogRepository(db), db,
		func(context.Context) (cleanup.Settings, error) { return cleanup.Settings{}, nil }, nil)
	svc := service.NewDataSettingsService(dataRepo, agg, retention, model.DataSettings{
		CleanupEnabled:       true,
		CleanupMode:          model.CleanupModeDaily,
		CleanupRunHour:       4,
		CleanupIntervalHours: 24,
		CleanupMaxDays:       90,
		CleanupBatchSize:     1000,
		TrimHeaders:          true,
		TrimHeadersAfterDays: 7,
	})
	return svc, db, dataRepo
}

// TestDataSettingsEnsureDefaultsSeedsOnce 首次播种 yaml 默认值；再次调用不覆盖用户修改。
func TestDataSettingsEnsureDefaultsSeedsOnce(t *testing.T) {
	svc, _, _ := newDataSettingsSvc(t)
	ctx := context.Background()

	if err := svc.EnsureDefaults(ctx); err != nil {
		t.Fatal(err)
	}
	row, err := svc.Settings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !row.CleanupEnabled || row.CleanupMode != model.CleanupModeDaily || row.CleanupRunHour != 4 ||
		row.CleanupMaxDays != 90 || !row.TrimHeaders || row.TrimHeadersAfterDays != 7 {
		t.Fatalf("播种默认值异常: %+v", row)
	}

	// 用户改配置后重复 EnsureDefaults 不得回退
	if _, err := svc.Update(ctx, &service.UpdateDataSettingsInput{
		Enabled: true, Mode: model.CleanupModeInterval, IntervalHours: 12,
		MaxDays: 30, TrimHeaders: true, TrimHeadersAfterDays: 5,
	}); err != nil {
		t.Fatal(err)
	}
	if err := svc.EnsureDefaults(ctx); err != nil {
		t.Fatal(err)
	}
	row, _ = svc.Settings(ctx)
	if row.CleanupMaxDays != 30 || row.CleanupMode != model.CleanupModeInterval {
		t.Fatalf("重复播种覆盖了用户配置: %+v", row)
	}
}

// TestDataSettingsUpdateValidation 校验失败返回参数错误；合法值落库且状态字段不被覆盖。
func TestDataSettingsUpdateValidation(t *testing.T) {
	svc, _, dataRepo := newDataSettingsSvc(t)
	ctx := context.Background()
	if err := svc.EnsureDefaults(ctx); err != nil {
		t.Fatal(err)
	}

	cases := []service.UpdateDataSettingsInput{
		{Enabled: true, Mode: "hourly", MaxDays: 30},                                                          // 非法模式
		{Enabled: true, Mode: model.CleanupModeDaily, RunHour: 24, MaxDays: 30},                               // 时刻越界
		{Enabled: true, Mode: model.CleanupModeDaily, MaxDays: 0},                                             // 保留天数非法
		{Enabled: true, Mode: model.CleanupModeDaily, MaxDays: 30, BatchSize: 50},                             // 批量过小
		{Enabled: true, Mode: model.CleanupModeDaily, MaxDays: 7, TrimHeaders: true, TrimHeadersAfterDays: 7}, // 瘦身线 ≥ 保留线
	}
	for i, in := range cases {
		if _, err := svc.Update(ctx, &in); err == nil {
			t.Errorf("case %d 应返回校验错误", i)
		} else if e := errs.From(err); e.Code != errs.CodeInvalidParams {
			t.Errorf("case %d 错误码 = %d, want %d", i, e.Code, errs.CodeInvalidParams)
		}
	}

	// 后台任务先写回状态，再更新配置：状态字段必须原样保留
	if err := dataRepo.UpdateCleanupStatus(ctx, time.Now(), 20000, 7, 3, 120, ""); err != nil {
		t.Fatal(err)
	}
	saved, err := svc.Update(ctx, &service.UpdateDataSettingsInput{
		Enabled: true, Mode: model.CleanupModeDaily, RunHour: 3, MaxDays: 60,
		TrimHeaders: true, TrimHeadersAfterDays: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if saved.CleanupRunHour != 3 || saved.CleanupMaxDays != 60 || saved.TrimHeadersAfterDays != 10 {
		t.Fatalf("配置未生效: %+v", saved)
	}
	row, _ := svc.Settings(ctx)
	if row.LastDeletedRows != 7 || row.LastTrimmedRows != 3 || row.LastCleanupDay != 20000 {
		t.Fatalf("配置更新覆盖了任务状态: %+v", row)
	}
}

// TestDataSettingsAggregateStatus 全量重建后聚合状态可读（水位线 + 覆盖范围）。
func TestDataSettingsAggregateStatus(t *testing.T) {
	db := testutil.NewMemoryDB(t)
	logRepo := repository.NewRequestLogRepository(db)
	ctx := context.Background()
	if err := logRepo.Create(ctx, &model.RequestLog{
		Model: "m1", Protocol: model.ProtocolChatCompletions, ForwardMode: model.ForwardNativePassthrough,
		PromptTokens: 10, CreatedAt: time.Now().AddDate(0, 0, -3),
	}); err != nil {
		t.Fatal(err)
	}

	daily := testutil.SeedDailyStats(t, db)
	dataRepo := repository.NewDataSettingsRepository(db)
	agg := aggregation.New(daily, dataRepo)
	retention := cleanup.NewRetention(logRepo, db,
		func(context.Context) (cleanup.Settings, error) { return cleanup.Settings{}, nil }, nil)
	svc := service.NewDataSettingsService(dataRepo, agg, retention, model.DataSettings{})
	if err := svc.EnsureDefaults(ctx); err != nil {
		t.Fatal(err)
	}

	if err := svc.RebuildAggregate(ctx, 0); err != nil {
		t.Fatal(err)
	}
	st, err := svc.AggregateStatus(ctx)
	if err != nil {
		t.Fatal(err)
	}
	today := repository.DayBucketOf(time.Now())
	if st.WatermarkDay != today-2 || st.WatermarkDate != repository.DayBucketDate(today-2) {
		t.Fatalf("水位线异常: %+v, want day=%d date=%s", st, today-2, repository.DayBucketDate(today-2))
	}
	if st.EarliestDay == "" || st.LatestDay == "" {
		t.Fatalf("覆盖范围为空: %+v", st)
	}
}

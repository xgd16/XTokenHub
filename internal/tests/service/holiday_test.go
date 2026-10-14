package service_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"xtokenhub/internal/model"
	"xtokenhub/internal/provider"
	"xtokenhub/internal/repository"
	"xtokenhub/internal/service"
	"xtokenhub/internal/tests/testutil"
)

// newHolidayEnv 内存库 + 年历桩的服务装配。
func newHolidayEnv(t *testing.T) (*service.HolidayService, *testutil.StubHolidayFetcher, repository.HolidayDateRepository) {
	t.Helper()
	db := testutil.NewMemoryDB(t)
	repo := repository.NewHolidayDateRepository(db)
	fetcher := testutil.NewStubHolidayFetcher()
	svc := service.NewHolidayService(repo, fetcher, service.HolidayOptions{Enabled: true})
	return svc, fetcher, repo
}

// TestHolidaySync 拉取写库 + 缓存可查：放假日与补班日分别落到对应类型。
func TestHolidaySync(t *testing.T) {
	svc, fetcher, _ := newHolidayEnv(t)
	// Sync 按北京时区拉「今年/明年」，用例用运行时年份动态构造
	year := time.Now().In(model.BeijingZone).Year()
	dayHoliday := fmt.Sprintf("%d-10-01", year)
	dayMakeup := fmt.Sprintf("%d-10-10", year)
	dayPlain := fmt.Sprintf("%d-10-13", year)
	dayNext := fmt.Sprintf("%d-01-01", year+1)
	fetcher.Years[year] = []provider.HolidayEntry{
		{Date: dayHoliday, Holiday: true, Name: "国庆节"},
		{Date: dayMakeup, Holiday: false, Name: "国庆节后补班"},
	}
	fetcher.Years[year+1] = []provider.HolidayEntry{
		{Date: dayNext, Holiday: true, Name: "元旦"},
	}

	res, err := svc.Sync(context.Background())
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if res.Written != 3 {
		t.Errorf("Written = %d, want 3", res.Written)
	}

	if got := svc.HolidayKindOf(dayHoliday); got != model.HolidayKindHoliday {
		t.Errorf("%s = %q, want holiday", dayHoliday, got)
	}
	if got := svc.HolidayKindOf(dayMakeup); got != model.HolidayKindWorkday {
		t.Errorf("%s = %q, want workday", dayMakeup, got)
	}
	if got := svc.HolidayKindOf(dayPlain); got != model.HolidayKindNone {
		t.Errorf("%s = %q, want 无标注", dayPlain, got)
	}

	st := svc.Status(context.Background())
	if st.HolidayCount != 2 || st.WorkdayCount != 1 {
		t.Errorf("规模 = holiday %d / workday %d, want 2 / 1", st.HolidayCount, st.WorkdayCount)
	}
	if st.LastError != "" || st.LastSyncCount != 3 {
		t.Errorf("状态 = %+v", st)
	}
}

// TestHolidaySyncKeepsOldOnEmpty 某年返回空数据（安排未公布）时保留旧标注。
func TestHolidaySyncKeepsOldOnEmpty(t *testing.T) {
	svc, fetcher, _ := newHolidayEnv(t)
	year := time.Now().In(model.BeijingZone).Year()
	dayNext := fmt.Sprintf("%d-01-01", year+1)
	fetcher.Years[year] = []provider.HolidayEntry{{Date: fmt.Sprintf("%d-10-01", year), Holiday: true}}
	fetcher.Years[year+1] = []provider.HolidayEntry{{Date: dayNext, Holiday: true}}
	if _, err := svc.Sync(context.Background()); err != nil {
		t.Fatalf("首次 Sync: %v", err)
	}

	// 第二轮：次年拉到空数据（未公布），应跳过且不删已有行
	delete(fetcher.Years, year+1)
	res, err := svc.Sync(context.Background())
	if err != nil {
		t.Fatalf("二次 Sync: %v", err)
	}
	if len(res.Skipped) == 0 {
		t.Error("空数据年份应记入 Skipped")
	}
	if got := svc.HolidayKindOf(dayNext); got != model.HolidayKindHoliday {
		t.Errorf("%s = %q, want 保留 holiday", dayNext, got)
	}
}

// TestHolidaySyncError 拉取失败返回错误并记录状态。
func TestHolidaySyncError(t *testing.T) {
	svc, fetcher, _ := newHolidayEnv(t)
	fetcher.Err = errors.New("boom")
	if _, err := svc.Sync(context.Background()); err == nil {
		t.Fatal("拉取失败应返回错误")
	}
	if st := svc.Status(context.Background()); st.LastError == "" {
		t.Error("失败应记录 last_error")
	}
}

// TestHolidayDisabled 未启用时拒绝同步。
func TestHolidayDisabled(t *testing.T) {
	db := testutil.NewMemoryDB(t)
	svc := service.NewHolidayService(
		repository.NewHolidayDateRepository(db), testutil.NewStubHolidayFetcher(),
		service.HolidayOptions{Enabled: false},
	)
	if _, err := svc.Sync(context.Background()); err == nil {
		t.Error("未启用应拒绝同步")
	}
}

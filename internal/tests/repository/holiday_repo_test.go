package repository_test

import (
	"context"
	"testing"

	"xtokenhub/internal/model"
	"xtokenhub/internal/repository"
	"xtokenhub/internal/tests/testutil"
)

// TestHolidayReplaceYear 按年整体替换：先删该年旧标注再写入，其它年份不受影响。
func TestHolidayReplaceYear(t *testing.T) {
	db := testutil.NewMemoryDB(t)
	repo := repository.NewHolidayDateRepository(db)
	ctx := context.Background()

	rows2026 := []model.HolidayDate{
		{Date: "2026-10-01", Kind: model.HolidayKindHoliday, Name: "国庆节"},
		{Date: "2026-10-07", Kind: model.HolidayKindHoliday, Name: "国庆节"},
		{Date: "2026-10-10", Kind: model.HolidayKindWorkday, Name: "国庆节后补班"},
	}
	n, err := repo.ReplaceYear(ctx, 2026, rows2026)
	if err != nil || n != 3 {
		t.Fatalf("ReplaceYear = %d, %v, want 3, nil", n, err)
	}
	if _, err := repo.ReplaceYear(ctx, 2027, []model.HolidayDate{
		{Date: "2027-01-01", Kind: model.HolidayKindHoliday, Name: "元旦"},
	}); err != nil {
		t.Fatalf("ReplaceYear 2027: %v", err)
	}

	// 重新替换 2026：旧行清除，只留新的一条
	n, err = repo.ReplaceYear(ctx, 2026, []model.HolidayDate{
		{Date: "2026-05-01", Kind: model.HolidayKindHoliday, Name: "劳动节"},
	})
	if err != nil || n != 1 {
		t.Fatalf("二次 ReplaceYear = %d, %v, want 1, nil", n, err)
	}

	all, err := repo.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("总行数 = %d, want 2（2026 一条 + 2027 一条）", len(all))
	}
	if all[0].Date != "2026-05-01" || all[1].Date != "2027-01-01" {
		t.Errorf("List 顺序 = %v, want 按日期升序", []string{all[0].Date, all[1].Date})
	}
	if all[0].Year != 2026 || all[1].Year != 2027 {
		t.Errorf("年份回填 = %d,%d, want 2026,2027", all[0].Year, all[1].Year)
	}

	y2027, err := repo.ListYear(ctx, 2027)
	if err != nil || len(y2027) != 1 || y2027[0].Date != "2027-01-01" {
		t.Errorf("ListYear(2027) = %v, %v", y2027, err)
	}

	total, err := repo.Count(ctx)
	if err != nil || total != 2 {
		t.Errorf("Count = %d, %v, want 2", total, err)
	}
}

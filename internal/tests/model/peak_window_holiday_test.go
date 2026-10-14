package model_test

import (
	"testing"
	"time"

	"xtokenhub/internal/model"
)

// fakeHolidaySource 内存年历，供 PeakWindow 的 !H / W 记号判定。
type fakeHolidaySource map[string]model.HolidayKind

func (f fakeHolidaySource) HolidayKindOf(day string) model.HolidayKind { return f[day] }

// useHoliday 注入内存年历，测试结束自动还原全局数据源。
func useHoliday(t *testing.T, src model.HolidaySource) {
	t.Helper()
	model.SetHolidaySource(src)
	t.Cleanup(func() { model.SetHolidaySource(nil) })
}

// TestPeakWindowHolidayTokens 验证节假日记号的解析：合法组合接受，未知记号报错。
func TestPeakWindowHolidayTokens(t *testing.T) {
	for _, s := range []string{
		"1-5,!H;09:00-12:00,14:00-18:00", // DeepSeek 口径：节假日全天谷
		"1-5,W;09:00-12:00,14:00-18:00",  // 补班日按工作日
		"1-5,!H,W;09:00-12:00",           // 两个记号组合
		"!H;00:00-23:59",                 // 只有记号、无星期
		"1-5,!h;09:00-12:00",             // 小写容忍
		"1-5,w;09:00-12:00",
	} {
		if _, err := model.NewPeakWindow(s); err != nil {
			t.Errorf("NewPeakWindow(%q) 报错: %v", s, err)
		}
	}
	for _, s := range []string{
		"1-5,!X;09:00-12:00", // 未知排除记号
		"1-5,H;09:00-12:00",  // 少了 ! 也报错（提示用 !H）
	} {
		if _, err := model.NewPeakWindow(s); err == nil {
			t.Errorf("NewPeakWindow(%q) 应报错", s)
		}
	}
}

// TestPeakWindowSkipHoliday 放假日（含调休连休）全天不计高峰；未配 !H 的规则不受影响。
// 2026-10-01 是周四（国庆节，年历标注放假）。
func TestPeakWindowSkipHoliday(t *testing.T) {
	useHoliday(t, fakeHolidaySource{"2026-10-01": model.HolidayKindHoliday})

	peak := bj(2026, time.October, 1, 10, 0) // 周四 10:00，落在高峰窗口内

	withToken := model.MustPeakWindow("1-5,!H;09:00-12:00,14:00-18:00")
	if withToken.IsPeak(peak) {
		t.Error("放假日白天应为空闲（!H）")
	}
	if p := withToken.Period(peak); p != model.PricePeriodOffPeak {
		t.Errorf("Period = %q, want off_peak", p)
	}
	// 放假日其它时段同样空闲
	if withToken.IsPeak(bj(2026, time.October, 1, 20, 0)) {
		t.Error("放假日夜间应为空闲")
	}

	// 同一天、未配 !H：维持原口径（按星期判定为高峰）
	noToken := model.MustPeakWindow("1-5;09:00-12:00,14:00-18:00")
	if !noToken.IsPeak(peak) {
		t.Error("未配 !H 的规则不应受年历影响")
	}

	// 普通工作日（2026-10-02 是周五，但也在放假；10-13 周二为普通日）不受影响
	if !withToken.IsPeak(bj(2026, time.October, 13, 10, 0)) {
		t.Error("普通工作日白天仍应是高峰")
	}
}

// TestPeakWindowMakeupWorkday 调休补班日（周末上班）按工作日对待。
// 2026-10-10 是周六（国庆节后补班，年历标注 workday）。
func TestPeakWindowMakeupWorkday(t *testing.T) {
	useHoliday(t, fakeHolidaySource{"2026-10-10": model.HolidayKindWorkday})

	sat := bj(2026, time.October, 10, 10, 0) // 周六 10:00

	withToken := model.MustPeakWindow("1-5,!H,W;09:00-12:00,14:00-18:00")
	if !withToken.IsPeak(sat) {
		t.Error("补班日白天应按工作日计高峰（W）")
	}
	// 补班日的空闲时段照旧不计峰
	if withToken.IsPeak(bj(2026, time.October, 10, 13, 0)) {
		t.Error("补班日 13:00 不在高峰窗口")
	}

	// 未配 W：周末一律空闲
	noToken := model.MustPeakWindow("1-5,!H;09:00-12:00,14:00-18:00")
	if noToken.IsPeak(sat) {
		t.Error("未配 W 的规则周末仍应为空闲")
	}

	// 普通周六（2026-10-17）即使配了 W 也照常空闲
	if withToken.IsPeak(bj(2026, time.October, 17, 10, 0)) {
		t.Error("普通周六不应因 W 记号变成高峰")
	}
}

// TestPeakWindowNoHolidaySource 未注册年历（或年历未同步）时记号退化为按星期判定，不误判。
func TestPeakWindowNoHolidaySource(t *testing.T) {
	model.SetHolidaySource(nil) // 显式清空
	w := model.MustPeakWindow("1-5,!H,W;09:00-12:00,14:00-18:00")
	if !w.IsPeak(bj(2026, time.October, 1, 10, 0)) {
		t.Error("无年历数据时放假日应退化为按星期判定（视为普通周四）")
	}
	if w.IsPeak(bj(2026, time.October, 10, 10, 0)) {
		t.Error("无年历数据时补班周六应退化为按星期判定（周末空闲）")
	}
}

// TestPeakWindowHolidayRoundTrip 带记号的规则走 TEXT 列与 JSON 的往返不变形。
func TestPeakWindowHolidayRoundTrip(t *testing.T) {
	const raw = "1-5,!H;09:00-12:00,14:00-18:00"
	w := model.MustPeakWindow(raw)
	v, err := w.Value()
	if err != nil || v != raw {
		t.Fatalf("Value = %v, %v", v, err)
	}
	var back model.PeakWindow
	if err := back.Scan(raw); err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if back.String() != raw {
		t.Errorf("Scan 后 = %q, want %q", back.String(), raw)
	}
}

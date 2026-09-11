package model_test

import (
	"encoding/json"
	"testing"
	"time"

	"xtokenhub/internal/model"
)

// bj 构造北京时间，便于表达时段断言。
func bj(year int, month time.Month, day, hour, min int) time.Time {
	return time.Date(year, month, day, hour, min, 0, 0, model.BeijingZone)
}

// DeepSeek 官方错峰规则：工作日 9:00-12:00、14:00-18:00 为高峰。
const deepseekWindow = "1-5;09:00-12:00,14:00-18:00"

func TestPeakWindowParseValid(t *testing.T) {
	for _, s := range []string{
		deepseekWindow,
		"1;09:00-12:00",
		"1-7;00:00-23:59",
		"1,3,5;09:00-12:00,14:00-18:00", // 列表
		"6-7;10:00-11:00",               // 周末
		" 1-5 ; 09:00 - 12:00 ",         // 空白容忍
		"1;9:00-12:00",                  // 单位数小时
	} {
		w, err := model.NewPeakWindow(s)
		if err != nil {
			t.Errorf("NewPeakWindow(%q) 报错: %v", s, err)
			continue
		}
		if !w.Valid() {
			t.Errorf("NewPeakWindow(%q).Valid() = false", s)
		}
	}
}

func TestPeakWindowParseInvalid(t *testing.T) {
	for _, s := range []string{
		"1-5",                         // 缺时段段
		"1-5;",                        // 空时段段
		";09:00-12:00",                // 空星期段
		"0;09:00-12:00",               // 星期越界（1..7）
		"8;09:00-12:00",               // 星期越界
		"5-1;09:00-12:00",             // 区间起点大于终点
		"1;12:00-09:00",               // 结束早于开始（不支持跨零点）
		"1;09:00-09:00",               // 零长区间
		"1;24:00-25:00",               // 小时越界
		"1;09:60-12:00",               // 分钟越界
		"1;0900-1200",                 // 缺冒号
		"1;09:00-12:00,",              // 尾随逗号产生空项
		"1-5;09:00-12:00;14:00-18:00", // 段数过多（分号应为两段）
	} {
		if _, err := model.NewPeakWindow(s); err == nil {
			t.Errorf("NewPeakWindow(%q) 应报错但通过了", s)
		}
	}
}

// 空串表示不启用时段价：恒为高峰（不打折）。
func TestPeakWindowEmptyAlwaysPeak(t *testing.T) {
	var w model.PeakWindow
	if w.Valid() {
		t.Error("零值 PeakWindow 不应 Valid")
	}
	// 任意时刻都是高峰
	for _, at := range []time.Time{bj(2026, 1, 3, 3, 0), bj(2026, 1, 5, 10, 0)} {
		if !w.IsPeak(at) {
			t.Errorf("未配置时段规则时 %v 应为高峰", at)
		}
		if p := w.Period(at); p != "" {
			t.Errorf("未配置时段规则时 Period = %q, want 空", p)
		}
	}
}

func TestPeakWindowIsPeak(t *testing.T) {
	w := model.MustPeakWindow(deepseekWindow)
	cases := []struct {
		name string
		at   time.Time
		want bool
	}{
		// 2026-01-05 是周一，01-03 是周六，01-04 是周日。
		{"工作日 上午高峰起点 09:00", bj(2026, 1, 5, 9, 0), true},
		{"工作日 上午高峰中段 10:30", bj(2026, 1, 5, 10, 30), true},
		{"工作日 上午高峰终点前 11:59", bj(2026, 1, 5, 11, 59), true},
		{"工作日 午休（12:00 已是空闲）", bj(2026, 1, 5, 12, 0), false},
		{"工作日 午休中段 13:00", bj(2026, 1, 5, 13, 0), false},
		{"工作日 下午高峰起点 14:00", bj(2026, 1, 5, 14, 0), true},
		{"工作日 下午高峰中段 16:00", bj(2026, 1, 5, 16, 0), true},
		{"工作日 下午高峰终点 18:00（不含）", bj(2026, 1, 5, 18, 0), false},
		{"工作日 晚间 20:00", bj(2026, 1, 5, 20, 0), false},
		{"工作日 凌晨 03:00", bj(2026, 1, 5, 3, 0), false},
		{"周五 上午高峰", bj(2026, 1, 9, 10, 0), true},
		{"周六 上午（周末非高峰）", bj(2026, 1, 3, 10, 0), false},
		{"周日 下午（周末非高峰）", bj(2026, 1, 4, 15, 0), false},
		{"周一 23:59", bj(2026, 1, 5, 23, 59), false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := w.IsPeak(c.at); got != c.want {
				t.Errorf("IsPeak(%v) = %v, want %v", c.at.In(model.BeijingZone), got, c.want)
			}
			// Period 必须与 IsPeak 一致
			wantPeriod := model.PricePeriodOffPeak
			if c.want {
				wantPeriod = model.PricePeriodPeak
			}
			if got := w.Period(c.at); got != wantPeriod {
				t.Errorf("Period(%v) = %q, want %q", c.at, got, wantPeriod)
			}
		})
	}
}

// 判定必须按北京时间，与入参时刻自身携带的时区无关。
func TestPeakWindowTimezoneIndependent(t *testing.T) {
	w := model.MustPeakWindow(deepseekWindow)
	// 同一时刻的三种表示：北京 2026-01-05 10:00 == UTC 02:00。
	bjTime := bj(2026, 1, 5, 10, 0)
	utcTime := bjTime.UTC()
	if !w.IsPeak(bjTime) || !w.IsPeak(utcTime) {
		t.Errorf("同一时刻不同时区表示应得到相同判定: bj=%v utc=%v",
			w.IsPeak(bjTime), w.IsPeak(utcTime))
	}
	// UTC 02:00 的“当天 10:00”在 UTC 时区看是周二凌晨吗？确认换算方向正确：
	// 北京时间周一 10:00 对应 UTC 周一 02:00，仍是工作日高峰。
	if utcTime.Weekday() != time.Monday || utcTime.Hour() != 2 {
		t.Fatalf("测试前提有误: UTC 表示 = %v", utcTime)
	}
}

// 跨日/多时段：夜间窗口（如 22:00-23:59）不应在白天命中。
func TestPeakWindowMultipleSpans(t *testing.T) {
	w := model.MustPeakWindow("1-7;00:00-06:00,22:00-23:59")
	if !w.IsPeak(bj(2026, 1, 5, 3, 0)) {
		t.Error("凌晨 03:00 应在高峰窗口内")
	}
	if w.IsPeak(bj(2026, 1, 5, 12, 0)) {
		t.Error("正午 12:00 不应在夜间高峰窗口内")
	}
	if !w.IsPeak(bj(2026, 1, 5, 23, 0)) {
		t.Error("23:00 应在高峰窗口内")
	}
	if w.IsPeak(bj(2026, 1, 5, 23, 59)) {
		t.Error("23:59 不含在 22:00-23:59 窗口内")
	}
}

// 值/扫描往返：数据库中存的是原始字符串，读回后判定行为一致。
func TestPeakWindowValueScanRoundTrip(t *testing.T) {
	w := model.MustPeakWindow(deepseekWindow)

	v, err := w.Value()
	if err != nil {
		t.Fatalf("Value: %v", err)
	}
	if v != deepseekWindow {
		t.Errorf("Value = %v, want %q", v, deepseekWindow)
	}

	var back model.PeakWindow
	if err := back.Scan(v); err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if back.String() != deepseekWindow {
		t.Errorf("往返后 = %q, want %q", back.String(), deepseekWindow)
	}
	if back.IsPeak(bj(2026, 1, 5, 10, 0)) != w.IsPeak(bj(2026, 1, 5, 10, 0)) {
		t.Error("往返后判定行为不一致")
	}

	// NULL 应扫描为零值（不启用时段价）
	var nul model.PeakWindow
	if err := nul.Scan(nil); err != nil {
		t.Fatalf("Scan(nil): %v", err)
	}
	if nul.Valid() {
		t.Error("Scan(nil) 后不应 Valid")
	}
	// []byte 也应支持（部分驱动返回字节切片）
	var fromBytes model.PeakWindow
	if err := fromBytes.Scan([]byte(deepseekWindow)); err != nil {
		t.Fatalf("Scan([]byte): %v", err)
	}
	if !fromBytes.Valid() {
		t.Error("Scan([]byte) 后应 Valid")
	}
	// 非法内容必须报错，而不是静默降级为“不启用”
	var bad model.PeakWindow
	if err := bad.Scan("1-5"); err == nil {
		t.Error("Scan 非法规则应报错")
	}
}

// 零值的 Value 写空串；JSON 往返保持字符串形态。
func TestPeakWindowZeroValueAndJSON(t *testing.T) {
	var w model.PeakWindow
	v, err := w.Value()
	if err != nil || v != "" {
		t.Errorf("零值 Value = (%v, %v), want (\"\", nil)", v, err)
	}

	// 结构体 JSON 序列化：peak_window 应为字符串
	type holder struct {
		W model.PeakWindow `json:"peak_window"`
	}
	raw, err := json.Marshal(holder{W: model.MustPeakWindow(deepseekWindow)})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if want := `{"peak_window":"` + deepseekWindow + `"}`; string(raw) != want {
		t.Errorf("Marshal = %s, want %s", raw, want)
	}

	var back holder
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if back.W.String() != deepseekWindow {
		t.Errorf("Unmarshal = %q", back.W.String())
	}
	// 非法规则 JSON 反序列化应报错
	if err := json.Unmarshal([]byte(`{"peak_window":"bogus"}`), &back); err == nil {
		t.Error("非法规则反序列化应报错")
	}
	// 空串反序列化为未启用
	var empty holder
	if err := json.Unmarshal([]byte(`{"peak_window":""}`), &empty); err != nil {
		t.Fatalf("空串反序列化: %v", err)
	}
	if empty.W.Valid() {
		t.Error("空串应为未启用")
	}
}

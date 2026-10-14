package model

import (
	"database/sql/driver"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// BeijingZone 高峰时段的判定时区（北京时间，UTC+8）。
//
// 用 FixedZone 而不加载 Asia/Shanghai：中国无夏令时，固定偏移永远正确；
// 且不依赖设备 tzdata（Alpine/postmarketOS 未必安装 zoneinfo）。
var BeijingZone = time.FixedZone("CST", 8*60*60)

// PricePeriod 计费时段。
type PricePeriod string

const (
	// PricePeriodPeak 高峰时段。
	PricePeriodPeak PricePeriod = "peak"
	// PricePeriodOffPeak 空闲（错峰优惠）时段。
	PricePeriodOffPeak PricePeriod = "off_peak"
)

// peakWindowLayout 原始字符串形态：`<星期>;<时段>`。
//
//	星期  1=周一 .. 7=周日，支持 `1-5` 区间与 `1,6,7` 列表；
//	      另有两个节假日记号（判定依赖节假日年历，见 holiday.go）：
//	      `!H` 放假日（含调休连休）全天不计高峰；`W` 调休补班日按工作日对待。
//	时段  `09:00-12:00`，多个用逗号分隔，如 `09:00-12:00,14:00-18:00`
//
// 例（DeepSeek 工作日高峰，节假日全天谷价）：`1-5,!H;09:00-12:00,14:00-18:00`
type peakWindowParsed struct {
	days          [8]bool  // 下标 1..7 有效
	spans         [][2]int // 每段 [起始分钟, 结束分钟)，同一天内
	skipHoliday   bool     // `!H`：放假日全天不计高峰
	makeupWorkday bool     // `W`：调休补班日视为命中星期集合
}

// PeakWindow 高峰时段规则。空值表示不启用时段价（金额恒按高峰价计）。
//
// 存为 TEXT 列，需要手写 Valuer/Scanner（GORM 不会自动为结构体做文本编解码）。
type PeakWindow struct {
	raw    string
	parsed *peakWindowParsed
}

// NewPeakWindow 解析并校验时段规则字符串；空串表示不启用（返回零值，无错误）。
func NewPeakWindow(s string) (PeakWindow, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return PeakWindow{}, nil
	}
	p, err := parsePeakWindow(s)
	if err != nil {
		return PeakWindow{}, err
	}
	return PeakWindow{raw: s, parsed: p}, nil
}

// MustPeakWindow 供已知合法的常量使用（测试、模板）。
func MustPeakWindow(s string) PeakWindow {
	w, err := NewPeakWindow(s)
	if err != nil {
		panic(err)
	}
	return w
}

// String 返回原始字符串；未启用时为空串。
func (w PeakWindow) String() string { return w.raw }

// Valid 是否已配置时段规则。
func (w PeakWindow) Valid() bool { return w.parsed != nil }

// IsPeak 判定 at 时刻是否处于高峰时段；未配置时段规则时恒为 true（不打折）。
//
// 按北京时间判定：先把 at 转到 UTC+8，取其星期与当日分钟数；
// 规则带 `!H` / `W` 记号时再按节假日年历（HolidayKindOf）修正。
func (w PeakWindow) IsPeak(at time.Time) bool {
	if w.parsed == nil {
		return true
	}
	local := at.In(BeijingZone)
	kind := HolidayKindOf(beijingDate(local))
	// `!H`：放假日（含调休连休）全天空闲，优先于星期与时段判定。
	if w.parsed.skipHoliday && kind == HolidayKindHoliday {
		return false
	}
	// Go 的 Weekday 是 0=周日；规则用 1=周一 .. 7=周日。
	day := int(local.Weekday())
	if day == 0 {
		day = 7
	}
	matched := w.parsed.days[day]
	// `W`：调休补班日（周末但按工作日上班）视为命中星期集合。
	if !matched && w.parsed.makeupWorkday && kind == HolidayKindWorkday {
		matched = true
	}
	if !matched {
		return false
	}
	minute := local.Hour()*60 + local.Minute()
	for _, sp := range w.parsed.spans {
		if minute >= sp[0] && minute < sp[1] {
			return true
		}
	}
	return false
}

// beijingDate 取北京日期 YYYY-MM-DD（年历主键口径）。
func beijingDate(t time.Time) string {
	y, m, d := t.Date()
	return fmt.Sprintf("%04d-%02d-%02d", y, int(m), d)
}

// Period 返回 at 时刻的计费时段；未配置时段规则时为空串（不落库、不展示）。
func (w PeakWindow) Period(at time.Time) PricePeriod {
	if w.parsed == nil {
		return ""
	}
	if w.IsPeak(at) {
		return PricePeriodPeak
	}
	return PricePeriodOffPeak
}

// Value 实现 driver.Valuer，写库为空串或原始规则。
func (w PeakWindow) Value() (driver.Value, error) {
	if w.raw == "" {
		return "", nil
	}
	// 重新校验：防止绕过 NewPeakWindow 直接构造出的非法值进库。
	if _, err := parsePeakWindow(w.raw); err != nil {
		return nil, err
	}
	return w.raw, nil
}

// Scan 实现 sql.Scanner，从 TEXT 列读回。
func (w *PeakWindow) Scan(src any) error {
	switch v := src.(type) {
	case nil:
		*w = PeakWindow{}
		return nil
	case string:
		parsed, err := NewPeakWindow(v)
		if err != nil {
			return err
		}
		*w = parsed
		return nil
	case []byte:
		return w.Scan(string(v))
	default:
		return fmt.Errorf("peak_window 列类型不支持: %T", src)
	}
}

// MarshalJSON 序列化为字符串（前端直接拿到原始规则）。
func (w PeakWindow) MarshalJSON() ([]byte, error) {
	return []byte(strconv.Quote(w.raw)), nil
}

// UnmarshalJSON 从字符串解析（非法规则返回错误）。
func (w *PeakWindow) UnmarshalJSON(b []byte) error {
	s, err := strconv.Unquote(string(b))
	if err != nil {
		return fmt.Errorf("peak_window 需为字符串: %w", err)
	}
	parsed, err := NewPeakWindow(s)
	if err != nil {
		return err
	}
	*w = parsed
	return nil
}

// parsePeakWindow 解析 `<星期>;<时段列表>`。
func parsePeakWindow(s string) (*peakWindowParsed, error) {
	parts := strings.Split(s, ";")
	if len(parts) != 2 {
		return nil, fmt.Errorf("时段规则需为 `<星期>;<时段>` 两段，如 1-5;09:00-12:00: %q", s)
	}
	days, skipHoliday, makeup, err := parseWeekdays(strings.TrimSpace(parts[0]))
	if err != nil {
		return nil, err
	}
	spans, err := parseSpans(strings.TrimSpace(parts[1]))
	if err != nil {
		return nil, err
	}
	if len(spans) == 0 {
		return nil, fmt.Errorf("时段规则至少需要一个时段: %q", s)
	}
	return &peakWindowParsed{
		days: days, spans: spans,
		skipHoliday: skipHoliday, makeupWorkday: makeup,
	}, nil
}

// parseWeekdays 解析星期表达式：支持 `1-5` 区间与 `1,6,7` 列表，值域 1..7；
// 另识别节假日记号 `!H`（放假日不计峰）与 `W`（补班日按工作日）。
func parseWeekdays(s string) (days [8]bool, skipHoliday, makeup bool, err error) {
	if s == "" {
		return days, false, false, fmt.Errorf("时段规则缺少星期段")
	}
	for _, item := range strings.Split(s, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			return days, false, false, fmt.Errorf("星期段含空项: %q", s)
		}
		// 节假日记号（大小写不敏感）：`!H` 放假日不计峰、`W` 补班日按工作日。
		switch strings.ToUpper(item) {
		case "!H":
			skipHoliday = true
			continue
		case "W":
			makeup = true
			continue
		}
		if strings.HasPrefix(item, "!") {
			return days, false, false, fmt.Errorf("未知节假日记号 %q（支持 !H = 放假日不计峰）", item)
		}
		lo, hi := item, item
		if i := strings.Index(item, "-"); i >= 0 {
			lo, hi = strings.TrimSpace(item[:i]), strings.TrimSpace(item[i+1:])
		}
		l, err := parseWeekday(lo, s)
		if err != nil {
			return days, false, false, err
		}
		h, err := parseWeekday(hi, s)
		if err != nil {
			return days, false, false, err
		}
		if l > h {
			return days, false, false, fmt.Errorf("星期区间起点大于终点: %q", item)
		}
		for d := l; d <= h; d++ {
			days[d] = true
		}
	}
	return days, skipHoliday, makeup, nil
}

func parseWeekday(s, whole string) (int, error) {
	n, err := strconv.Atoi(s)
	if err != nil || n < 1 || n > 7 {
		return 0, fmt.Errorf("星期需为 1..7（1=周一）: %q（完整规则 %q）", s, whole)
	}
	return n, nil
}

// parseSpans 解析 HH:MM-HH:MM 列表，返回按出现顺序的 [起, 止) 分钟对。
// 结束时间必须晚于开始时间：空闲时段以「列出高峰窗口」表达，无需跨零点环绕。
func parseSpans(s string) ([][2]int, error) {
	if s == "" {
		return nil, fmt.Errorf("时段规则缺少时段段")
	}
	var spans [][2]int
	for _, item := range strings.Split(s, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			return nil, fmt.Errorf("时段段含空项: %q", s)
		}
		dash := strings.Index(item, "-")
		if dash < 0 {
			return nil, fmt.Errorf("时段需为 HH:MM-HH:MM: %q", item)
		}
		start, err := parseClock(item[:dash])
		if err != nil {
			return nil, err
		}
		end, err := parseClock(item[dash+1:])
		if err != nil {
			return nil, err
		}
		if end <= start {
			return nil, fmt.Errorf("时段结束须晚于开始（不支持跨零点）: %q", item)
		}
		spans = append(spans, [2]int{start, end})
	}
	return spans, nil
}

// parseClock 解析 HH:MM（或 H:MM）为当日分钟数，值域 00:00..23:59。
func parseClock(s string) (int, error) {
	s = strings.TrimSpace(s)
	parts := strings.Split(s, ":")
	if len(parts) != 2 {
		return 0, fmt.Errorf("时间需为 HH:MM: %q", s)
	}
	h, err := strconv.Atoi(strings.TrimSpace(parts[0]))
	if err != nil || h < 0 || h > 23 {
		return 0, fmt.Errorf("小时需为 0..23: %q", s)
	}
	m, err := strconv.Atoi(strings.TrimSpace(parts[1]))
	if err != nil || m < 0 || m > 59 {
		return 0, fmt.Errorf("分钟需为 0..59: %q", s)
	}
	return h*60 + m, nil
}

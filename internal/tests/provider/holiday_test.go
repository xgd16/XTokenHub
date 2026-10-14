package provider_test

import (
	"testing"

	"xtokenhub/internal/provider"
)

// 年历响应样例（holiday-cn，默认源）：isOffDay=true 放假、false 调休补班。
const holidayCNBody = `{
  "year": 2026,
  "days": [
    {"name": "国庆节", "date": "2026-10-01", "isOffDay": true},
    {"name": "国庆节", "date": "2026-10-07", "isOffDay": true},
    {"name": "国庆节", "date": "2026-10-10", "isOffDay": false}
  ]
}`

// 年历响应样例（timor.tech 口径，兼容解析）：holiday=true 放假、false 调休补班。
const holidayTimorBody = `{
  "code": 0,
  "holiday": {
    "10-01": {"holiday": true, "name": "国庆节", "wage": 3, "date": "2026-10-01"},
    "10-07": {"holiday": true, "name": "国庆节", "wage": 2, "date": "2026-10-07"},
    "10-10": {"holiday": false, "name": "国庆节后补班", "wage": 1, "target": "国庆节", "after": true, "date": "2026-10-10"},
    "12-31": {"holiday": true, "name": "元旦", "wage": 2}
  }
}`

// TestParseHolidayYearCN 默认源格式：放假/补班标注。
func TestParseHolidayYearCN(t *testing.T) {
	entries, err := provider.ParseHolidayYear([]byte(holidayCNBody), 2026)
	if err != nil {
		t.Fatalf("ParseHolidayYear: %v", err)
	}
	byDate := map[string]provider.HolidayEntry{}
	for _, e := range entries {
		byDate[e.Date] = e
	}
	if len(entries) != 3 {
		t.Fatalf("条目数 = %d, want 3", len(entries))
	}
	if e := byDate["2026-10-01"]; !e.Holiday || e.Name != "国庆节" {
		t.Errorf("10-01 = %+v, want 放假 国庆节", e)
	}
	if e := byDate["2026-10-10"]; e.Holiday {
		t.Errorf("10-10 = %+v, want 补班", e)
	}
}

// TestParseHolidayYearTimor 兼容 timor.tech 格式；无 date 字段时用 MM-DD 键补全年份。
func TestParseHolidayYearTimor(t *testing.T) {
	entries, err := provider.ParseHolidayYear([]byte(holidayTimorBody), 2026)
	if err != nil {
		t.Fatalf("ParseHolidayYear: %v", err)
	}
	if len(entries) != 4 {
		t.Fatalf("条目数 = %d, want 4", len(entries))
	}
	byDate := map[string]provider.HolidayEntry{}
	for _, e := range entries {
		byDate[e.Date] = e
	}
	if e := byDate["2026-10-01"]; !e.Holiday || e.Name != "国庆节" {
		t.Errorf("10-01 = %+v, want 放假 国庆节", e)
	}
	if e := byDate["2026-10-10"]; e.Holiday || e.Name != "国庆节后补班" {
		t.Errorf("10-10 = %+v, want 补班 国庆节后补班", e)
	}
	// 无 date 字段：键 12-31 + year 2026
	if e := byDate["2026-12-31"]; !e.Holiday {
		t.Errorf("12-31 = %+v, want 放假（键补全年份）", e)
	}
}

// TestParseHolidayYearEmpty 某年安排未公布时返回空切片、不报错（调用方保留旧数据）。两种格式同口径。
func TestParseHolidayYearEmpty(t *testing.T) {
	entries, err := provider.ParseHolidayYear([]byte(`{"code":0,"holiday":{}}`), 2027)
	if err != nil {
		t.Fatalf("ParseHolidayYear: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("条目数 = %d, want 0", len(entries))
	}
}

// TestParseHolidayYearBadCode 接口报错与非法 JSON 都应返回错误。
func TestParseHolidayYearBadCode(t *testing.T) {
	if _, err := provider.ParseHolidayYear([]byte(`{"code":1,"holiday":{}}`), 2026); err == nil {
		t.Error("code!=0 应报错")
	}
	if _, err := provider.ParseHolidayYear([]byte(`not-json`), 2026); err == nil {
		t.Error("非法 JSON 应报错")
	}
}

// TestHolidayURL 年份占位符替换与末尾追加两种拼法。
func TestHolidayURL(t *testing.T) {
	for _, tc := range []struct {
		tpl  string
		year int
		want string
	}{
		{"", 2026, "https://raw.githubusercontent.com/NateScarlet/holiday-cn/master/2026.json"}, // 默认（holiday-cn）
		{"https://x.example/cal/{year}.json", 2027, "https://x.example/cal/2027.json"},          // 占位符
		{"https://x.example/cal/", 2027, "https://x.example/cal/2027"},                          // 末尾追加
		{"https://x.example/cal", 2027, "https://x.example/cal/2027"},                           // 末尾追加（无斜杠）
	} {
		got := provider.HolidayURL(tc.tpl, tc.year)
		if got != tc.want {
			t.Errorf("HolidayURL(%q,%d) = %q, want %q", tc.tpl, tc.year, got, tc.want)
		}
	}
}

package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// DefaultHolidayURLTemplate 内置节假日年历源（holiday-cn，按国务院放假安排标注
// 放假/调休补班，数据随公告更新）。`{year}` 为年份占位符。
// 备选 timor.tech（https://timor.tech/api/holiday/year/{year}）解析同样支持，
// 但该站对非浏览器客户端有 Cloudflare 拦截，需自备可达链路。
const DefaultHolidayURLTemplate = "https://raw.githubusercontent.com/NateScarlet/holiday-cn/master/{year}.json"

// holidayUA 年历请求的 User-Agent：部分站点（含 CDN 防护）会拦截空/Go 默认 UA。
const holidayUA = "XTokenHub/1.0 (holiday-calendar sync)"

// maxHolidayBytes 年历响应体上限（一年几十条标注，2MB 已是宽松上限）。
const maxHolidayBytes = 2 << 20

// HolidayEntry 年历中的一条标注。
type HolidayEntry struct {
	// Date 北京日期 YYYY-MM-DD。
	Date string
	// Holiday true=放假（含调休连休）；false=调休补班上班日。
	Holiday bool
	// Name 节日名（如 国庆节、国庆节后补班），仅展示。
	Name string
}

// HolidayClient 拉取节假日年历。
type HolidayClient struct {
	http *http.Client
}

// NewHolidayClient 构造；timeout <= 0 时取 20 秒。
func NewHolidayClient(timeout time.Duration) *HolidayClient {
	if timeout <= 0 {
		timeout = 20 * time.Second
	}
	return &HolidayClient{http: &http.Client{Timeout: timeout}}
}

// FetchYear 拉取并解析某年的节假日年历。urlTpl 为空时使用 DefaultHolidayURLTemplate；
// 含 `{year}` 占位符则替换，不含时在末尾追加年份路径段。
func (c *HolidayClient) FetchYear(ctx context.Context, urlTpl string, year int) ([]HolidayEntry, error) {
	url := HolidayURL(urlTpl, year)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("构造年历请求: %w", err)
	}
	req.Header.Set("User-Agent", holidayUA)
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("拉取年历: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("年历返回 HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxHolidayBytes))
	if err != nil {
		return nil, fmt.Errorf("读取年历: %w", err)
	}
	return ParseHolidayYear(body, year)
}

// HolidayURL 拼出年份对应的年历地址：{year} 占位符替换，不含占位符时在末尾追加年份路径段。
func HolidayURL(urlTpl string, year int) string {
	tpl := strings.TrimSpace(urlTpl)
	if tpl == "" {
		tpl = DefaultHolidayURLTemplate
	}
	ys := strconv.Itoa(year)
	if strings.Contains(tpl, "{year}") {
		return strings.ReplaceAll(tpl, "{year}", ys)
	}
	return strings.TrimRight(tpl, "/") + "/" + ys
}

// holidayCNRec holiday-cn 年历的单条标注。
type holidayCNRec struct {
	Name     string `json:"name"`
	Date     string `json:"date"`
	IsOffDay bool   `json:"isOffDay"`
}

// holidayTimorRec timor.tech 年历的单条标注。
type holidayTimorRec struct {
	Holiday bool   `json:"holiday"`
	Name    string `json:"name"`
	Date    string `json:"date"`
}

// ParseHolidayYear 解析年历 JSON，自动识别两种常见格式：
//
//	holiday-cn（默认源）：{"year":2026,"days":[{"date":"2026-10-01","isOffDay":true,"name":"国庆节"}, ...]}
//	timor.tech：          {"code":0,"holiday":{"MM-DD":{"holiday":true,"name":"国庆节","date":"2026-10-01"}, ...}}
//
// isOffDay / holiday 为 true 表示放假（含调休连休），false 表示调休补班上班日。
// 某年安排未公布时两种格式都表现为「无条目」，返回空切片（调用方据此保留旧数据）；
// timor 格式 code != 0 视为接口报错。
func ParseHolidayYear(body []byte, year int) ([]HolidayEntry, error) {
	var raw struct {
		Code    int                        `json:"code"`
		Year    int                        `json:"year"`
		Days    []holidayCNRec             `json:"days"`
		Holiday map[string]json.RawMessage `json:"holiday"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("解析年历 JSON: %w", err)
	}
	if len(raw.Days) > 0 {
		entries := make([]HolidayEntry, 0, len(raw.Days))
		for _, d := range raw.Days {
			entries = append(entries, HolidayEntry{
				Date: strings.TrimSpace(d.Date), Holiday: d.IsOffDay, Name: d.Name,
			})
		}
		return entries, nil
	}
	if raw.Holiday != nil {
		if raw.Code != 0 {
			return nil, fmt.Errorf("年历接口返回 code=%d", raw.Code)
		}
		entries := make([]HolidayEntry, 0, len(raw.Holiday))
		for key, rec := range raw.Holiday {
			var h holidayTimorRec
			if err := json.Unmarshal(rec, &h); err != nil {
				return nil, fmt.Errorf("解析年历条目 %q: %w", key, err)
			}
			date := strings.TrimSpace(h.Date)
			if date == "" {
				// 键为 MM-DD 时补全年份。
				date = fmt.Sprintf("%04d-%s", year, key)
			}
			entries = append(entries, HolidayEntry{Date: date, Holiday: h.Holiday, Name: h.Name})
		}
		return entries, nil
	}
	return []HolidayEntry{}, nil
}

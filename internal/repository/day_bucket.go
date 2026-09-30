package repository

import "time"

// 本地日桶号口径与 dayBucketSQL 一致：桶号 = (Unix 秒 + 本地偏移) / 86400。
// 非夏令时时区（如 Asia/Shanghai）换算精确一致；夏令时时区在切换日可能有 ±1 小时
// 误差，与 dayBucketSQL 的既有口径相同。

// DayBucketOf 时间点所属的本地日桶号。
func DayBucketOf(t time.Time) int64 {
	_, off := t.Zone()
	return (t.Unix() + int64(off)) / 86400
}

// DayRange 本地日桶号对应的明细时间范围 [start, end)，可直接用于 created_at 索引查询。
// 起止按 86400 秒整段推进（与 dayBucketSQL 的整数除法同口径，不走日历加法避免夏令时干扰）。
func DayRange(day int64) (start, end time.Time) {
	_, off := time.Now().Zone()
	start = time.Unix(day*86400-int64(off), 0)
	return start, start.Add(86400 * time.Second)
}

// DayBucketDate 本地日桶号 → 'YYYY-MM-DD'（与 bucketToDate 同口径）。
func DayBucketDate(day int64) string {
	return time.Unix(day*86400, 0).UTC().Format("2006-01-02")
}

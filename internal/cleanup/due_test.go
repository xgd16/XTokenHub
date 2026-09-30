package cleanup

import (
	"testing"
	"time"

	"xtokenhub/internal/repository"
)

// TestDueDaily daily 模式：每天至多一次；到点执行，错过时刻顺延补跑。
func TestDueDaily(t *testing.T) {
	now := time.Date(2026, 9, 30, 10, 0, 0, 0, time.Local)
	today := repository.DayBucketOf(now)

	st := Settings{Enabled: true, Mode: ModeDaily, RunHour: 4}
	if !due(st, now) {
		t.Error("从未清理过且已过执行时刻，应到期")
	}
	st.LastCleanupDay = today
	if due(st, now) {
		t.Error("今天已清理过，不应再执行")
	}
	st.LastCleanupDay = today - 1
	early := time.Date(2026, 9, 30, 3, 0, 0, 0, time.Local)
	if due(st, early) {
		t.Error("未到执行时刻，不应执行")
	}
	if !due(st, now) {
		t.Error("已过执行时刻且今天未清理，应执行")
	}
	off := st
	off.Enabled = false
	if due(off, now) {
		t.Error("禁用后不应执行")
	}
}

// TestDueInterval interval 模式：按上次清理时间判到期；从未清理立即到期。
func TestDueInterval(t *testing.T) {
	now := time.Now()
	st := Settings{Enabled: true, Mode: ModeInterval, IntervalHours: 6}
	if !due(st, now) {
		t.Error("从未清理过，应立即到期（保持旧「启动即清」行为）")
	}
	st.LastCleanupAt = now.Add(-5 * time.Hour)
	if due(st, now) {
		t.Error("未满间隔，不应执行")
	}
	st.LastCleanupAt = now.Add(-7 * time.Hour)
	if !due(st, now) {
		t.Error("已满间隔，应执行")
	}
	off := st
	off.Enabled = false
	if due(off, now) {
		t.Error("禁用后不应执行")
	}
}

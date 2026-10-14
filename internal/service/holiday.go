package service

import (
	"context"
	"strings"
	"sync"
	"time"

	"xtokenhub/internal/model"
	"xtokenhub/internal/pkg/errs"
	"xtokenhub/internal/pkg/logger"
	"xtokenhub/internal/provider"
	"xtokenhub/internal/repository"
)

const (
	// holidayLogModule 日志模块名。
	holidayLogModule = "holiday"
)

// HolidayOptions 节假日年历同步运行参数。
type HolidayOptions struct {
	// Enabled 是否启用节假日年历（规则的 !H / W 记号判定依赖它）。
	Enabled bool
	// AutoSync 是否按 SyncInterval 定时同步。
	AutoSync bool
	// SyncInterval 同步周期（节假日安排一年一变，默认一周足够）。
	SyncInterval time.Duration
	// SourceURL 年历地址，空则用 provider.DefaultHolidayURLTemplate。
	SourceURL string
}

// HolidayFetcher 年历拉取来源，由 provider.HolidayClient 实现；测试可注入假源。
type HolidayFetcher interface {
	FetchYear(ctx context.Context, url string, year int) ([]provider.HolidayEntry, error)
}

// HolidayService 节假日年历：外部同步 + 内存查询，实现 model.HolidaySource。
//
// 年历变化极少、查询却在计费热路径上，因此启动时整体载入内存缓存，
// 同步成功后重建；数据库是唯一持久层。
type HolidayService struct {
	opts    HolidayOptions
	repo    repository.HolidayDateRepository
	fetcher HolidayFetcher

	syncMu sync.Mutex

	statusMu      sync.RWMutex
	lastSyncAt    time.Time
	lastSyncErr   string
	lastSyncNum   int
	lastSyncYears []int

	cacheMu sync.RWMutex
	cache   map[string]model.HolidayKind
}

// NewHolidayService 构造节假日服务。
func NewHolidayService(repo repository.HolidayDateRepository, fetcher HolidayFetcher, opts HolidayOptions) *HolidayService {
	return &HolidayService{opts: opts, repo: repo, fetcher: fetcher, cache: map[string]model.HolidayKind{}}
}

// LoadCache 启动时把年历载入内存缓存（应先于接受请求调用）。
func (s *HolidayService) LoadCache(ctx context.Context) error {
	return s.reloadCache(ctx)
}

// reloadCache 全量重建内存缓存。
func (s *HolidayService) reloadCache(ctx context.Context) error {
	rows, err := s.repo.List(ctx)
	if err != nil {
		return errs.Wrap(errs.CodeInternal, "读取节假日年历失败", err)
	}
	cache := make(map[string]model.HolidayKind, len(rows))
	for _, r := range rows {
		cache[r.Date] = r.Kind
	}
	s.cacheMu.Lock()
	s.cache = cache
	s.cacheMu.Unlock()
	return nil
}

// HolidayKindOf 实现 model.HolidaySource：按北京日期 YYYY-MM-DD 查标注。
func (s *HolidayService) HolidayKindOf(day string) model.HolidayKind {
	s.cacheMu.RLock()
	defer s.cacheMu.RUnlock()
	return s.cache[day]
}

// HolidaySyncResult 同步结果。
type HolidaySyncResult struct {
	Years    []int     `json:"years"`   // 本次拉取的年份
	Written  int       `json:"written"` // 写入行数（各年合计）
	Skipped  []int     `json:"skipped"` // 返回空数据而保留旧标注的年份（安排未公布）
	SyncedAt time.Time `json:"synced_at"`
	Source   string    `json:"source"`
}

// Sync 拉取今年与明年的节假日年历并整体替换本地标注。
// 某年返回空数据（安排未公布）时保留旧标注；拉取失败立即返回，已写入的年份保留。
func (s *HolidayService) Sync(ctx context.Context) (*HolidaySyncResult, error) {
	if !s.opts.Enabled {
		return nil, errs.New(errs.CodeInvalidParams, "节假日年历未启用（configs/config.yaml 的 holiday.enabled）")
	}
	if !s.syncMu.TryLock() {
		return nil, errs.New(errs.CodeInvalidParams, "节假日同步正在进行中，请稍候")
	}
	defer s.syncMu.Unlock()

	tpl := s.opts.SourceURL
	source := tpl
	if strings.TrimSpace(source) == "" {
		source = provider.DefaultHolidayURLTemplate
	}

	now := time.Now()
	years := []int{now.In(model.BeijingZone).Year(), now.In(model.BeijingZone).Year() + 1}
	res := &HolidaySyncResult{Years: years, SyncedAt: now, Source: source}

	var firstErr error
	for _, y := range years {
		entries, err := s.fetcher.FetchYear(ctx, tpl, y)
		if err != nil {
			s.recordSyncError(err)
			firstErr = err
			continue
		}
		if len(entries) == 0 {
			// 该年安排未公布：保留旧标注，等下轮同步。
			res.Skipped = append(res.Skipped, y)
			continue
		}
		rows := make([]model.HolidayDate, 0, len(entries))
		for _, e := range entries {
			kind := model.HolidayKindHoliday
			if !e.Holiday {
				kind = model.HolidayKindWorkday
			}
			rows = append(rows, model.HolidayDate{
				Date: e.Date, Kind: kind, Name: e.Name, Year: y, SyncedAt: now,
			})
		}
		n, err := s.repo.ReplaceYear(ctx, y, rows)
		if err != nil {
			s.recordSyncError(err)
			firstErr = err
			continue
		}
		res.Written += n
	}

	// 有写入或部分成功都要重建缓存，保证查询与库一致。
	if err := s.reloadCache(ctx); err != nil && firstErr == nil {
		firstErr = err
		s.recordSyncError(err)
	}
	if firstErr != nil {
		return nil, errs.Wrap(errs.CodeInternal, "同步节假日年历失败", firstErr)
	}

	s.recordSyncOK(res.Written, now, years)
	logger.L(holidayLogModule).Info("节假日年历同步完成",
		"years", years, "written", res.Written, "skipped", res.Skipped, "source", source)
	return res, nil
}

func (s *HolidayService) recordSyncError(err error) {
	s.statusMu.Lock()
	s.lastSyncErr = err.Error()
	s.statusMu.Unlock()
	logger.L(holidayLogModule).Warn("节假日年历同步失败", logger.Err(err))
}

func (s *HolidayService) recordSyncOK(written int, at time.Time, years []int) {
	s.statusMu.Lock()
	s.lastSyncAt = at
	s.lastSyncErr = ""
	s.lastSyncNum = written
	s.lastSyncYears = years
	s.statusMu.Unlock()
}

// RunAutoSync 后台定时同步，直到 ctx 取消。未启用或间隔非法时立即返回。
func (s *HolidayService) RunAutoSync(ctx context.Context) {
	if !s.opts.Enabled || !s.opts.AutoSync || s.opts.SyncInterval <= 0 {
		return
	}
	ticker := time.NewTicker(s.opts.SyncInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_, _ = s.Sync(ctx)
		}
	}
}

// HolidayStatus 同步状态与年历规模，供设置页展示。
type HolidayStatus struct {
	Enabled       bool       `json:"enabled"`
	AutoSync      bool       `json:"auto_sync"`
	SourceURL     string     `json:"source_url"`
	IntervalHours float64    `json:"interval_hours"`
	LastSyncAt    *time.Time `json:"last_sync_at,omitempty"`
	LastError     string     `json:"last_error,omitempty"`
	LastSyncCount int        `json:"last_sync_count"`
	LastSyncYears []int      `json:"last_sync_years,omitempty"`
	HolidayCount  int        `json:"holiday_count"`
	WorkdayCount  int        `json:"workday_count"`
}

// Status 返回同步状态与年历规模。
func (s *HolidayService) Status(ctx context.Context) *HolidayStatus {
	source := s.opts.SourceURL
	if strings.TrimSpace(source) == "" {
		source = provider.DefaultHolidayURLTemplate
	}
	st := &HolidayStatus{
		Enabled:       s.opts.Enabled,
		AutoSync:      s.opts.AutoSync,
		SourceURL:     source,
		IntervalHours: s.opts.SyncInterval.Hours(),
	}
	s.statusMu.RLock()
	if !s.lastSyncAt.IsZero() {
		at := s.lastSyncAt
		st.LastSyncAt = &at
	}
	st.LastError = s.lastSyncErr
	st.LastSyncCount = s.lastSyncNum
	st.LastSyncYears = append([]int(nil), s.lastSyncYears...)
	s.statusMu.RUnlock()

	if rows, err := s.repo.List(ctx); err == nil {
		for _, r := range rows {
			switch r.Kind {
			case model.HolidayKindHoliday:
				st.HolidayCount++
			case model.HolidayKindWorkday:
				st.WorkdayCount++
			}
		}
	}
	return st
}

// ListYear 某年标注，按日期升序（设置页与前端峰谷卡片用）。
func (s *HolidayService) ListYear(ctx context.Context, year int) ([]model.HolidayDate, error) {
	return s.repo.ListYear(ctx, year)
}

// Package database 负责 GORM + SQLite(纯 Go) 的连接初始化与自动迁移。
package database

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	"xtokenhub/internal/config"
	"xtokenhub/internal/model"
	"xtokenhub/internal/pkg/logger"
)

const (
	// defaultMaxOpenConns 未配置（或配置非法）时的连接池上限。
	defaultMaxOpenConns = 4
	// pageCacheKiB 每连接 page cache（负值单位为 KiB）。生产库约 5~11MB，
	// 16MB 足以让整库常驻内存，聚合查询不再反复读 eMMC。
	pageCacheKiB = -16000
	// mmapBytes 内存映射窗口，省去读路径的页拷贝。
	mmapBytes = 134217728 // 128MB
)

// Open 建立数据库连接（path 为 ":memory:" 时用于测试）。
func Open(cfg *config.DatabaseConfig) (*gorm.DB, error) {
	dsn := cfg.Path
	if dsn != ":memory:" {
		if dir := filepath.Dir(dsn); dir != "." {
			// 数据目录由调用方保证存在；这里再兜底创建。
			_ = ensureDir(dir)
		}
		dsn += fmt.Sprintf(
			"?_pragma=journal_mode(WAL)"+
				"&_pragma=busy_timeout(5000)"+
				"&_pragma=synchronous(NORMAL)"+
				"&_pragma=cache_size(%d)"+
				"&_pragma=mmap_size(%d)",
			pageCacheKiB, mmapBytes,
		)
	}

	gormCfg := &gorm.Config{
		Logger: gormlogger.Default.LogMode(logMode(cfg.EnableLog)),
	}
	db, err := gorm.Open(sqlite.Open(dsn), gormCfg)
	if err != nil {
		return nil, fmt.Errorf("打开 sqlite(%s): %w", cfg.Path, err)
	}

	if dsn != ":memory:" {
		sqlDB, err := db.DB()
		if err != nil {
			return nil, fmt.Errorf("获取底层 sql.DB: %w", err)
		}
		maxConns := cfg.MaxOpenConns
		if maxConns < 1 {
			maxConns = defaultMaxOpenConns
		}
		// WAL 模式下读可并发：连接数 > 1 才能让仪表盘的并发查询真正并行执行。
		// 写仍由 SQLite 串行化，busy_timeout(5000) 负责写锁竞争的重试兜底。
		sqlDB.SetMaxOpenConns(maxConns)
		sqlDB.SetMaxIdleConns(maxConns)
		sqlDB.SetConnMaxLifetime(time.Hour)
		logEffectiveSettings(db, maxConns)
	}
	return db, nil
}

// logEffectiveSettings 启动时回读并记录实际生效的 PRAGMA 与连接池大小。
// WAL 是多连接并发读的前提：若它没生效（例如旧库或 pragma 被忽略），
// 连接数 > 1 会争抢数据库锁，必须启动即暴露而不是靠推断。
func logEffectiveSettings(db *gorm.DB, maxConns int) {
	log := logger.L("database")
	var journalMode string
	var cacheSize int
	if err := db.Raw("PRAGMA journal_mode").Row().Scan(&journalMode); err != nil {
		log.Warn("读取 journal_mode 失败", logger.Err(err))
		journalMode = "unknown"
	}
	if err := db.Raw("PRAGMA cache_size").Row().Scan(&cacheSize); err != nil {
		log.Warn("读取 cache_size 失败", logger.Err(err))
	}
	log.Info("数据库连接池就绪",
		"max_open_conns", maxConns,
		"journal_mode", journalMode,
		"cache_size_kib", cacheSize,
	)
	if !strings.EqualFold(journalMode, "wal") {
		log.Warn("journal_mode 非 WAL：多连接并发读可能争锁，建议检查数据目录权限与磁盘",
			"journal_mode", journalMode)
	}
}

// Migrate 自动迁移全部业务表，并补建 AutoMigrate 建不了的 FTS5 虚表。
func Migrate(db *gorm.DB) error {
	if err := db.AutoMigrate(
		&model.Channel{},
		&model.APIKey{},
		&model.RequestLog{},
		&model.CustomModel{},
		&model.SessionHeaderConfig{},
		&model.ModelPrice{},
		&model.HolidayDate{},
		&model.BillingSettings{},
		&model.DailyUsageStat{},
		&model.DailyModelStat{},
		&model.DataSettings{},
		&model.ReasoningRewrite{},
		&model.MimoSettings{},
		&model.Memory{},
		&model.MemoryNamespace{},
		&model.MemoryLink{},
		&model.MemorySettings{},
	); err != nil {
		return fmt.Errorf("AutoMigrate: %w", err)
	}
	if err := ensureMemoryFTS(db); err != nil {
		return err
	}
	return nil
}

// ensureMemoryFTS 建立记忆库全文索引虚表（AutoMigrate 无法创建虚拟表，用幂等 DDL 补）。
// unicode61 作用于 textseg 预切分后的 *_seg 列：ASCII 词与中文 bigram 已是空格分隔的 token，
// 因此中文子串检索不再受「整段中文=一个 token」之困；短查询由 LIKE 兜底。
func ensureMemoryFTS(db *gorm.DB) error {
	err := db.Exec(`CREATE VIRTUAL TABLE IF NOT EXISTS memories_fts USING fts5(
		title_seg, content_seg, tags_seg, tokenize='unicode61 remove_diacritics 2'
	)`).Error
	if err != nil {
		return fmt.Errorf("创建 memories_fts: %w", err)
	}
	return nil
}

func ensureDir(dir string) error {
	return os.MkdirAll(dir, 0o755)
}

func logMode(enable bool) gormlogger.LogLevel {
	if enable {
		return gormlogger.Info
	}
	return gormlogger.Warn
}

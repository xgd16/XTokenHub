// Package database 负责 GORM + SQLite(纯 Go) 的连接初始化与自动迁移。
package database

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"xtokenhub/internal/config"
	"xtokenhub/internal/model"
)

// Open 建立数据库连接（path 为 ":memory:" 时用于测试）。
func Open(cfg *config.DatabaseConfig) (*gorm.DB, error) {
	dsn := cfg.Path
	if dsn != ":memory:" {
		if dir := filepath.Dir(dsn); dir != "." {
			// 数据目录由调用方保证存在；这里再兜底创建。
			_ = ensureDir(dir)
		}
		dsn += "?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=synchronous(NORMAL)"
	}

	gormCfg := &gorm.Config{
		Logger: logger.Default.LogMode(logMode(cfg.EnableLog)),
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
		sqlDB.SetMaxOpenConns(1) // SQLite 单写连接，避免 SQLITE_BUSY
		sqlDB.SetMaxIdleConns(1)
		sqlDB.SetConnMaxLifetime(time.Hour)
	}
	return db, nil
}

// Migrate 自动迁移全部业务表。
func Migrate(db *gorm.DB) error {
	if err := db.AutoMigrate(&model.Channel{}, &model.APIKey{}, &model.RequestLog{}, &model.CustomModel{}); err != nil {
		return fmt.Errorf("AutoMigrate: %w", err)
	}
	return nil
}

func ensureDir(dir string) error {
	return os.MkdirAll(dir, 0o755)
}

func logMode(enable bool) logger.LogLevel {
	if enable {
		return logger.Info
	}
	return logger.Warn
}

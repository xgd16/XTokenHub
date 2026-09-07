// Package testutil 测试共享工具（非测试文件，可被多个测试包导入）。
package testutil

import (
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"xtokenhub/internal/model"
)

// NewMemoryDB 每个用例独享 :memory: SQLite（单连接共享同一库）并完成迁移。
func NewMemoryDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("打开内存库: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("获取底层连接: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)
	if err := db.AutoMigrate(&model.Channel{}, &model.APIKey{}, &model.RequestLog{}, &model.CustomModel{}); err != nil {
		t.Fatalf("迁移: %v", err)
	}
	return db
}

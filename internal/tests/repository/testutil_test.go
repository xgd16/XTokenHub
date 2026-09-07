// Package repository 的测试基建：每个用例独享一个 :memory: SQLite 库。
package repository_test

import (
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"xtokenhub/internal/model"
)

// newTestDB 建立独立的内存库并完成迁移。
func NewTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("打开内存库: %v", err)
	}
	// :memory: 模式下每个连接都是独立库，限定单连接保证所有查询共享同一库
	// （与生产 SQLite 单写连接策略一致）。
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("获取底层连接: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)
	if err := db.AutoMigrate(&model.Channel{}, &model.APIKey{}, &model.RequestLog{}); err != nil {
		t.Fatalf("迁移: %v", err)
	}
	return db
}

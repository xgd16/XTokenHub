package database_test

import (
	"xtokenhub/internal/database"

	"path/filepath"
	"testing"

	"xtokenhub/internal/config"
)

func TestOpenMemoryAndMigrate(t *testing.T) {
	cfg := &config.DatabaseConfig{Path: ":memory:"}
	db, err := database.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Migrate(db); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	// 表存在性
	for _, table := range []string{"channels", "request_logs"} {
		if !db.Migrator().HasTable(table) {
			t.Errorf("表 %s 未创建", table)
		}
	}
	// 迁移幂等
	if err := database.Migrate(db); err != nil {
		t.Errorf("重复迁移失败: %v", err)
	}
}

func TestOpenFileDB(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "t.db")
	cfg := &config.DatabaseConfig{Path: path}
	db, err := database.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Migrate(db); err != nil {
		t.Fatal(err)
	}
	// 数据落盘验证：写一条再新开连接读
	if err := db.Exec("INSERT INTO channels (name, provider, base_url, api_key) VALUES ('x','openai_compatible','u','k')").Error; err != nil {
		t.Fatal(err)
	}
	db2, err := database.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	var count int64
	if err := db2.Raw("SELECT COUNT(*) FROM channels").Scan(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Errorf("持久化失败: count=%d", count)
	}
}

func TestOpenBadPath(t *testing.T) {
	// 非法路径应返回错误而非 panic
	cfg := &config.DatabaseConfig{Path: "/nonexistent-root-dir/a/b/c.db"}
	if _, err := database.Open(cfg); err == nil {
		t.Error("非法路径应报错")
	}
}

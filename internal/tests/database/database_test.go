package database_test

import (
	"xtokenhub/internal/database"

	"fmt"
	"path/filepath"
	"strings"
	"sync"
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

// 多连接并发读的前提是 WAL：非 WAL 下连接数 > 1 只会争抢数据库锁。
func TestOpenFileDBEnablesWALAndCache(t *testing.T) {
	cfg := &config.DatabaseConfig{Path: filepath.Join(t.TempDir(), "wal.db")}
	db, err := database.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}

	var journalMode string
	if err := db.Raw("PRAGMA journal_mode").Row().Scan(&journalMode); err != nil {
		t.Fatal(err)
	}
	if !strings.EqualFold(journalMode, "wal") {
		t.Errorf("journal_mode = %q, 期望 wal", journalMode)
	}

	// cache_size 以负数（KiB）配置时，回读应得到同样的负值
	var cacheSize int
	if err := db.Raw("PRAGMA cache_size").Row().Scan(&cacheSize); err != nil {
		t.Fatal(err)
	}
	if cacheSize >= 0 {
		t.Errorf("cache_size = %d, 期望负数（KiB 口径）", cacheSize)
	}
}

func TestOpenUsesConfiguredPoolSize(t *testing.T) {
	cfg := &config.DatabaseConfig{Path: filepath.Join(t.TempDir(), "pool.db"), MaxOpenConns: 4}
	db, err := database.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	if got := sqlDB.Stats().MaxOpenConnections; got != 4 {
		t.Errorf("MaxOpenConnections = %d, 期望 4", got)
	}
	// 未配置时回退默认值，保证连接数不会退化成 1（=串行执行）
	def, err := database.Open(&config.DatabaseConfig{Path: filepath.Join(t.TempDir(), "def.db")})
	if err != nil {
		t.Fatal(err)
	}
	defSQL, err := def.DB()
	if err != nil {
		t.Fatal(err)
	}
	if got := defSQL.Stats().MaxOpenConnections; got != 4 {
		t.Errorf("默认 MaxOpenConnections = %d, 期望 4", got)
	}
}

// 并发读必须全部成功：这正是仪表盘多接口同时拉取的场景。
func TestConcurrentReads(t *testing.T) {
	cfg := &config.DatabaseConfig{Path: filepath.Join(t.TempDir(), "conc.db"), MaxOpenConns: 4}
	db, err := database.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Migrate(db); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 50; i++ {
		if err := db.Exec(
			"INSERT INTO request_logs (created_at, protocol, forward_mode, model, prompt_tokens, completion_tokens) VALUES (datetime('now'), 'messages', 'native_passthrough', 'm', 10, 5)",
		).Error; err != nil {
			t.Fatal(err)
		}
	}

	const workers = 8
	errCh := make(chan error, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				var count int64
				if err := db.Raw(
					"SELECT COUNT(*) FROM request_logs WHERE created_at >= datetime('now','-1 day')",
				).Scan(&count).Error; err != nil {
					errCh <- err
					return
				}
				if count != 50 {
					errCh <- fmt.Errorf("count = %d, 期望 50", count)
					return
				}
			}
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Errorf("并发读失败: %v", err)
	}
}

// 并发写读混合：写仍是串行的，busy_timeout 负责重试，不应出现 SQLITE_BUSY。
func TestConcurrentWriteWithReads(t *testing.T) {
	cfg := &config.DatabaseConfig{Path: filepath.Join(t.TempDir(), "mix.db"), MaxOpenConns: 4}
	db, err := database.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Migrate(db); err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	errCh := make(chan error, 16)
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				if err := db.Exec(
					"INSERT INTO request_logs (created_at, protocol, forward_mode, model) VALUES (datetime('now'), 'messages', 'native_passthrough', 'm')",
				).Error; err != nil {
					errCh <- err
					return
				}
			}
		}()
	}
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				var count int64
				if err := db.Raw("SELECT COUNT(*) FROM request_logs").Scan(&count).Error; err != nil {
					errCh <- err
					return
				}
			}
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Errorf("并发读写失败: %v", err)
	}

	var total int64
	if err := db.Raw("SELECT COUNT(*) FROM request_logs").Scan(&total).Error; err != nil {
		t.Fatal(err)
	}
	if total != 40 {
		t.Errorf("写入总数 = %d, 期望 40", total)
	}
}

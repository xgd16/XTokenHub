package repository_test

import (
	"context"
	"testing"

	"xtokenhub/internal/model"
	"xtokenhub/internal/repository"
	"xtokenhub/internal/tests/testutil"
)

// TestListPagedPeakOnly 验证 peak_only 只返回配置了峰谷时段价的模型；
// 仪表盘峰谷模块依赖该过滤避免大表分页漏行。
func TestListPagedPeakOnly(t *testing.T) {
	db := testutil.NewMemoryDB(t)
	repo := repository.NewModelPriceRepository(db)
	ctx := context.Background()

	win, err := model.NewPeakWindow("1-5;09:00-12:00,14:00-18:00")
	if err != nil {
		t.Fatal(err)
	}
	prices := []model.ModelPrice{
		{Model: "deepseek-chat", Currency: model.CurrencyCNY, InputCostPerToken: 1, OutputCostPerToken: 2, PeakWindow: win},
		{Model: "deepseek-reasoner", InputCostPerToken: 1, OutputCostPerToken: 2}, // 未配置时段价
		{Model: "gpt-4o", InputCostPerToken: 1, OutputCostPerToken: 2},            // 未配置时段价
	}
	for i := range prices {
		if err := repo.Create(ctx, &prices[i]); err != nil {
			t.Fatalf("写入价格: %v", err)
		}
	}

	items, total, err := repo.ListPaged(ctx, "", false, true, 0, 50)
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || len(items) != 1 || items[0].Model != "deepseek-chat" {
		t.Fatalf("peak_only 应只返回 deepseek-chat, got total=%d items=%v", total, items)
	}
	if !items[0].PeakWindow.Valid() {
		t.Errorf("返回行应带有效时段规则")
	}

	// 不过滤时全量返回
	_, all, err := repo.ListPaged(ctx, "", false, false, 0, 50)
	if err != nil {
		t.Fatal(err)
	}
	if all != 3 {
		t.Errorf("不过滤应返回 3 行, got %d", all)
	}
}

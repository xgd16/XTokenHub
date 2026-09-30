// Package service_test MiMo 网页凭证业务测试：播种幂等、Cookie 校验、
// 更新后回调触发（余额缓存失效）。
package service_test

import (
	"context"
	"errors"
	"testing"

	"gorm.io/gorm"

	"xtokenhub/internal/pkg/errs"
	"xtokenhub/internal/repository"
	"xtokenhub/internal/service"
	"xtokenhub/internal/tests/testutil"
)

func newMimoSettingsSvc(t *testing.T) (*service.MimoSettingsService, *gorm.DB) {
	t.Helper()
	db := testutil.NewMemoryDB(t)
	return service.NewMimoSettingsService(repository.NewMimoSettingsRepository(db)), db
}

// TestMimoSettingsEnsureDefaultsSeedsOnce 首次播种空凭证行；再次调用不覆盖已保存的 Cookie。
func TestMimoSettingsEnsureDefaultsSeedsOnce(t *testing.T) {
	svc, _ := newMimoSettingsSvc(t)
	ctx := context.Background()

	if err := svc.EnsureDefaults(ctx); err != nil {
		t.Fatal(err)
	}
	row, err := svc.Get(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if row.ID != 1 || row.Cookie != "" {
		t.Fatalf("播种应为空凭证行: %+v", row)
	}

	// 保存凭证后重复 EnsureDefaults 不得清掉
	const cookie = `userId=266075060; api-platform_serviceToken="abc="`
	if _, err := svc.UpdateCookie(ctx, cookie); err != nil {
		t.Fatal(err)
	}
	if err := svc.EnsureDefaults(ctx); err != nil {
		t.Fatal(err)
	}
	row, _ = svc.Get(ctx)
	if row.Cookie != cookie {
		t.Fatalf("重复播种覆盖了用户凭证: %q", row.Cookie)
	}
}

// TestMimoSettingsUpdateCookieValidation 缺 serviceToken 的串拒绝；合法值落库并可读回。
func TestMimoSettingsUpdateCookieValidation(t *testing.T) {
	svc, _ := newMimoSettingsSvc(t)
	ctx := context.Background()
	if err := svc.EnsureDefaults(ctx); err != nil {
		t.Fatal(err)
	}

	if _, err := svc.UpdateCookie(ctx, `cookie-preferences=%7B%7D; userId=266075060`); err == nil {
		t.Fatal("缺 serviceToken 应报参数错误")
	} else if appErr := errs.From(err); appErr == nil || appErr.Code != errs.CodeInvalidParams {
		t.Fatalf("应返回参数错误, got %v", err)
	}

	const cookie = `userId=266075060; api-platform_serviceToken="tok="`
	row, err := svc.UpdateCookie(ctx, cookie)
	if err != nil {
		t.Fatal(err)
	}
	if row.Cookie != cookie || row.UpdatedAt.IsZero() {
		t.Fatalf("保存结果异常: %+v", row)
	}
	// Cookie() 供余额查询读取
	got, err := svc.Cookie(ctx)
	if err != nil || got != cookie {
		t.Fatalf("Cookie() = %q, %v", got, err)
	}

	// 首尾空白应被修剪
	if _, err := svc.UpdateCookie(ctx, "  "+cookie+"  "); err != nil {
		t.Fatal(err)
	}
	if got, _ := svc.Cookie(ctx); got != cookie {
		t.Fatalf("应修剪首尾空白: %q", got)
	}
}

// TestMimoSettingsCookieChangedCallback 更新凭证后触发回调（清余额缓存）；
// 空串表示清除凭证，同样触发回调。
func TestMimoSettingsCookieChangedCallback(t *testing.T) {
	svc, _ := newMimoSettingsSvc(t)
	ctx := context.Background()
	if err := svc.EnsureDefaults(ctx); err != nil {
		t.Fatal(err)
	}

	calls := 0
	svc.SetOnCookieChanged(func() { calls++ })
	if _, err := svc.UpdateCookie(ctx, `userId=1; api-platform_serviceToken="t"`); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.UpdateCookie(ctx, ""); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("回调应触发 2 次, got %d", calls)
	}
	if got, _ := svc.Cookie(ctx); got != "" {
		t.Fatalf("空串应清除凭证, got %q", got)
	}
}

// TestMimoSettingsCookieUnseeded 行不存在时 Cookie() 返回空串而非报错（余额侧按未配置处理）。
func TestMimoSettingsCookieUnseeded(t *testing.T) {
	svc, _ := newMimoSettingsSvc(t)
	got, err := svc.Cookie(context.Background())
	if err != nil {
		t.Fatalf("未播种不应报错: %v", err)
	}
	if got != "" {
		t.Fatalf("未播种应返回空串, got %q", got)
	}
	if _, err := svc.Get(context.Background()); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("Get 应返回 NotFound, got %v", err)
	}
}

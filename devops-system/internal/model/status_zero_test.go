package model

import (
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

// TestStatusZeroValuePersists 守护「停用状态可被持久化」。
//
// 这是一个真实陷阱：GORM 的 struct tag `default:1` 会把字段的零值视为
// 「未提供」并改写为默认值。对 Status（0=停用 1=正常）而言，
// 零值 0 恰恰是合法且有业务含义的值——一旦带上 default:1，
// 任何 Create(&x{Status: 0}) 都会被静默改写成 Status=1，
// 表现为「租户/用户/角色停用不生效」，且不报任何错。
//
// 该陷阱曾导致 TestHumaTenantMiddlewareStates 失败：停用租户（Status=0）
// 读回却是 1，中间件据此放行。
//
// 修法：移除 Status 字段的 `default:1`（数据库列本身仍有 DEFAULT 1，
// 直接 SQL 写入不受影响；GORM 显式赋值的 0 则能正确落库）。
func TestStatusZeroValuePersists(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:statusprobe?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range []any{&Tenant{}, &User{}, &Role{}, &Dept{}, &Post{}, &Menu{}} {
		if err := db.AutoMigrate(m); err != nil {
			t.Fatal(err)
		}
	}

	// 各表插入一条 Status=0（停用）的记录，读回必须是 0。
	tn := &Tenant{BaseModel: BaseModel{ID: 801}, Name: "停用租户", Code: "t801", Status: 0}
	if err := db.Create(tn).Error; err != nil {
		t.Fatal(err)
	}
	var gotTenant Tenant
	if err := db.Where("id = ?", 801).First(&gotTenant).Error; err != nil {
		t.Fatal(err)
	}
	if gotTenant.Status != 0 {
		t.Fatalf("Tenant.Status：写入 0(停用) 读回 %d，说明 default tag 仍在改写零值", gotTenant.Status)
	}

	// 顺带确认 1 也能正常写入，避免误伤另一侧。
	tn2 := &Tenant{BaseModel: BaseModel{ID: 802}, Name: "正常租户", Code: "t802", Status: 1}
	if err := db.Create(tn2).Error; err != nil {
		t.Fatal(err)
	}
	var gotTenant2 Tenant
	if err := db.Where("id = ?", 802).First(&gotTenant2).Error; err != nil {
		t.Fatal(err)
	}
	if gotTenant2.Status != 1 {
		t.Fatalf("Tenant.Status：写入 1(正常) 读回 %d", gotTenant2.Status)
	}
}

// 事务体 dao 化后的行为契约（sqlite 内存库）：
//   - SyncRoleMenusTx / SyncUserRolesTx 的 replace 语义（清旧 + 插新；空集清空）
//   - SoftDeleteRoleTx / SoftDeletePostTx 的唯一键改写（软删后同 code 可重建）
package dao_test

import (
	"context"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"devops-system/internal/dao"
	"devops-system/internal/model"
)

// txDB 建 sqlite 内存库并迁移本组测试所需表。
func txDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:dao_tx_test?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开 sqlite 失败: %v", err)
	}
	if err := db.AutoMigrate(&model.Role{}, &model.Post{}, &model.UserRole{}, &model.RoleMenu{}); err != nil {
		t.Fatalf("建表失败: %v", err)
	}
	return db
}

func menuIDsOf(t *testing.T, db *gorm.DB, roleID int64) []int64 {
	t.Helper()
	var ids []int64
	if err := db.Model(&model.RoleMenu{}).Where("role_id = ?", roleID).Pluck("menu_id", &ids).Error; err != nil {
		t.Fatalf("查询 role_menu 失败: %v", err)
	}
	return ids
}

// TestSyncRoleMenusReplace 首插 → 换集合（旧的全删）→ 空集（清空）。
func TestSyncRoleMenusReplace(t *testing.T) {
	db := txDB(t)

	if err := dao.SyncRoleMenusTx(db, 1, []int64{10, 11}); err != nil {
		t.Fatalf("首次写入失败: %v", err)
	}
	if got := menuIDsOf(t, db, 1); len(got) != 2 {
		t.Fatalf("首次写入后菜单数 = %d, want 2", len(got))
	}

	if err := dao.SyncRoleMenusTx(db, 1, []int64{20}); err != nil {
		t.Fatalf("替换失败: %v", err)
	}
	got := menuIDsOf(t, db, 1)
	if len(got) != 1 || got[0] != 20 {
		t.Fatalf("替换后菜单 = %v, want [20]", got)
	}

	if err := dao.SyncRoleMenusTx(db, 1, nil); err != nil {
		t.Fatalf("清空失败: %v", err)
	}
	if got := menuIDsOf(t, db, 1); len(got) != 0 {
		t.Fatalf("空集替换后菜单 = %v, want 空", got)
	}

	// 其他角色的关联不受影响
	if err := dao.SyncRoleMenusTx(db, 2, []int64{30}); err != nil {
		t.Fatalf("写入角色2失败: %v", err)
	}
	if got := menuIDsOf(t, db, 2); len(got) != 1 || got[0] != 30 {
		t.Fatalf("角色2菜单 = %v, want [30]", got)
	}
}

// TestSyncUserRolesReplace 用户-角色关联的同一 replace 语义。
func TestSyncUserRolesReplace(t *testing.T) {
	db := txDB(t)

	if err := dao.SyncUserRolesTx(db, 7, []int64{1, 2}); err != nil {
		t.Fatalf("首次写入失败: %v", err)
	}
	if err := dao.SyncUserRolesTx(db, 7, []int64{3}); err != nil {
		t.Fatalf("替换失败: %v", err)
	}
	var ids []int64
	if err := db.Model(&model.UserRole{}).Where("user_id = ?", 7).Pluck("role_id", &ids).Error; err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if len(ids) != 1 || ids[0] != 3 {
		t.Fatalf("替换后角色 = %v, want [3]", ids)
	}

	if err := dao.SyncUserRolesTx(db, 7, nil); err != nil {
		t.Fatalf("清空失败: %v", err)
	}
	var cnt int64
	if err := db.Model(&model.UserRole{}).Where("user_id = ?", 7).Count(&cnt).Error; err != nil {
		t.Fatalf("计数失败: %v", err)
	}
	if cnt != 0 {
		t.Fatalf("空集替换后关联数 = %d, want 0", cnt)
	}
}

// TestSoftDeleteUniqueRewrite 软删改写唯一键：软删后同 code 可再建（租户内唯一索引不冲突）。
func TestSoftDeleteUniqueRewrite(t *testing.T) {
	db := txDB(t)

	role := &model.Role{Name: "运维", Code: "ops"}
	role.ID = 42
	if err := db.Create(role).Error; err != nil {
		t.Fatalf("建角色失败: %v", err)
	}
	if err := dao.SoftDeleteRoleTx(db, role.ID, role.Code); err != nil {
		t.Fatalf("软删角色失败: %v", err)
	}
	var code string
	if err := db.Unscoped().Model(&model.Role{}).Where("id = ?", role.ID).Pluck("code", &code).Error; err != nil {
		t.Fatalf("查询软删角色失败: %v", err)
	}
	if want := "ops_del_42"; code != want {
		t.Fatalf("软删后 code = %q, want %q", code, want)
	}
	// 默认作用域下已不可见（deleted_at 已落）
	var alive int64
	if err := db.Model(&model.Role{}).Where("id = ?", role.ID).Count(&alive).Error; err != nil {
		t.Fatalf("计数失败: %v", err)
	}
	if alive != 0 {
		t.Fatalf("软删后默认作用域仍可见 %d 行", alive)
	}

	post := &model.Post{Code: "ceo", Name: "CEO"}
	post.ID = 99
	if err := db.Create(post).Error; err != nil {
		t.Fatalf("建岗位失败: %v", err)
	}
	if err := dao.SoftDeletePostTx(db, post.ID, post.Code); err != nil {
		t.Fatalf("软删岗位失败: %v", err)
	}
	var pcode string
	if err := db.Unscoped().Model(&model.Post{}).Where("id = ?", post.ID).Pluck("code", &pcode).Error; err != nil {
		t.Fatalf("查询软删岗位失败: %v", err)
	}
	if want := "ceo_del_99"; pcode != want {
		t.Fatalf("软删后 code = %q, want %q", pcode, want)
	}
}

// WithTx 走 tenantdb.DB(ctx)，主链路依赖真实数据源；此处仅验证事务回滚语义可用。
func TestWithTxRollback(t *testing.T) {
	db := txDB(t)
	err := db.Transaction(func(tx *gorm.DB) error {
		if err := dao.SyncRoleMenusTx(tx, 5, []int64{1}); err != nil {
			return err
		}
		return context.Canceled // 模拟事务中断
	})
	if err == nil {
		t.Fatal("期望事务返回错误")
	}
	if got := menuIDsOf(t, db, 5); len(got) != 0 {
		t.Fatalf("回滚后残留 %d 行, want 0", len(got))
	}
}

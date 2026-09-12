// TenantPlugin（模式A 核心，方案 5.3）：GORM 回调自动注入/过滤 tenant_id。
//
// fail-closed 语义：
//   - stmt.Schema 不含 tenant_id 字段（全局表）→ 放行
//   - 原生 JOIN 查询（stmt.Joins 非空）→ 放行：dao 已显式过滤（裸列在多表下有歧义风险）
//   - ctx 无租户且未显式 Skip → 直接报错，查询失败（防串数据）
//   - 查询/改/删：注入 WHERE tenant_id = ?；新建：填充 TenantID 字段
//
// 仅在 server 装配期注册（migrate/种子直接走 db.Get()，无插件、不误伤）。
package tenantdb

import (
	"context"
	"reflect"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/schema"

	"devops-infra/errs"
)

// RegisterPlugin 在共享连接上注册租户过滤回调（server 命令装配期调用一次）。
func RegisterPlugin(gdb *gorm.DB) error {
	cbs := gdb.Callback()
	if err := cbs.Create().Before("gorm:create").
		Register("tenant:fill", onCreate); err != nil {
		return err
	}
	if err := cbs.Query().Before("gorm:query").
		Register("tenant:scope", onScope); err != nil {
		return err
	}
	if err := cbs.Update().Before("gorm:update").
		Register("tenant:scope", onScope); err != nil {
		return err
	}
	if err := cbs.Delete().Before("gorm:delete").
		Register("tenant:scope", onScope); err != nil {
		return err
	}
	return nil
}

// tenantFieldOf 取租户判别列字段；非租户表返回 nil。
func tenantFieldOf(stmt *gorm.Statement) *schema.Field {
	if stmt == nil || stmt.Schema == nil {
		return nil
	}
	return stmt.Schema.FieldsByDBName["tenant_id"]
}

// onScope Query/Update/Delete 前置回调：注入 tenant_id 条件。
func onScope(tx *gorm.DB) {
	field := tenantFieldOf(tx.Statement)
	if field == nil {
		return // 全局表 / 原生 SQL（Schema 为 nil，调用方负责）
	}
	if len(tx.Statement.Joins) > 0 {
		return // 原生 JOIN：dao 显式过滤（多表下注入裸列会歧义）
	}

	tc, ok := From(tx.Statement.Context)
	if !ok {
		if IsSkip(tx.Statement.Context) {
			return
		}
		tx.AddError(errs.New(errs.CodeInternal, "缺少租户上下文（fail-closed）"))
		return
	}
	tx.Statement.AddClause(clause.Where{
		Exprs: []clause.Expression{clause.Eq{
			Column: clause.Column{Name: field.DBName},
			Value:  tc.ID,
		}},
	})
}

// onCreate Create 前置回调：填充 TenantID 字段（零值才填，显式指定不受影响）。
func onCreate(tx *gorm.DB) {
	field := tenantFieldOf(tx.Statement)
	if field == nil {
		return
	}

	tc, ok := From(tx.Statement.Context)
	if !ok {
		if IsSkip(tx.Statement.Context) {
			return
		}
		tx.AddError(errs.New(errs.CodeInternal, "缺少租户上下文（fail-closed）"))
		return
	}
	fillTenantID(tx.Statement, field, tc.ID)
}

// fillTenantID 向目标值（struct 或 slice）填充 TenantID。
func fillTenantID(stmt *gorm.Statement, field *schema.Field, tid int64) {
	rv := stmt.ReflectValue
	switch rv.Kind() {
	case reflect.Slice, reflect.Array:
		for i := 0; i < rv.Len(); i++ {
			setTenantField(field, rv.Index(i), tid)
		}
	case reflect.Struct:
		setTenantField(field, rv, tid)
	}
}

// setTenantField 字段为零值时写入租户 ID。
func setTenantField(field *schema.Field, v reflect.Value, tid int64) {
	if !v.CanAddr() {
		return
	}
	if _, isZero := field.ValueOf(context.Background(), v); isZero {
		_ = field.Set(context.Background(), v, tid)
	}
}

package tenant

import (
	"context"
	"reflect"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/schema"
)

// RegisterPlugin 在 GORM 上注册租户字段自动填充和行级过滤回调。
func RegisterPlugin(gdb *gorm.DB) error {
	if gdb == nil {
		return ErrContextMissing
	}
	cbs := gdb.Callback()
	if err := cbs.Create().Before("gorm:create").Register("tenant:fill", onCreate); err != nil {
		return err
	}
	if err := cbs.Query().Before("gorm:query").Register("tenant:scope", onScope); err != nil {
		return err
	}
	if err := cbs.Update().Before("gorm:update").Register("tenant:scope", onScope); err != nil {
		return err
	}
	return cbs.Delete().Before("gorm:delete").Register("tenant:scope", onScope)
}

func tenantFieldOf(stmt *gorm.Statement) *schema.Field {
	if stmt == nil || stmt.Schema == nil {
		return nil
	}
	return stmt.Schema.FieldsByDBName["tenant_id"]
}

func onScope(tx *gorm.DB) {
	field := tenantFieldOf(tx.Statement)
	if field == nil || len(tx.Statement.Joins) > 0 || IsSkip(tx.Statement.Context) {
		return
	}
	tenantID, err := RequiredTenantID(tx.Statement.Context)
	if err != nil {
		tx.AddError(ErrContextMissing)
		return
	}
	tx.Statement.AddClause(clause.Where{Exprs: []clause.Expression{
		clause.Eq{Column: clause.Column{Name: field.DBName}, Value: tenantID},
	}})
}

func onCreate(tx *gorm.DB) {
	field := tenantFieldOf(tx.Statement)
	if field == nil || IsSkip(tx.Statement.Context) {
		return
	}
	tenantID, err := RequiredTenantID(tx.Statement.Context)
	if err != nil {
		tx.AddError(ErrContextMissing)
		return
	}
	fillTenantID(tx.Statement, field, tenantID)
}

func fillTenantID(stmt *gorm.Statement, field *schema.Field, tenantID int64) {
	value := stmt.ReflectValue
	switch value.Kind() {
	case reflect.Slice, reflect.Array:
		for i := 0; i < value.Len(); i++ {
			setTenantField(field, value.Index(i), tenantID)
		}
	case reflect.Struct:
		setTenantField(field, value, tenantID)
	}
}

func setTenantField(field *schema.Field, value reflect.Value, tenantID int64) {
	if !value.CanAddr() {
		return
	}
	if _, zero := field.ValueOf(context.Background(), value); zero {
		_ = field.Set(context.Background(), value, tenantID)
	}
}

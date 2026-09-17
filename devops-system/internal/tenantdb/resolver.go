// 模式B（schema）：租户独立 Schema 的数据源解析与库管理。
//
// 如实说明 GORM 约束：schema.Namer 无 context 且表名按 model 缓存，
// 按请求动态拼表名不可行；DSN 级切库是 GORM 生态内唯一干净的物理隔离路径。
//
// 路由规则（tenantdb.DB(ctx)）：
//   - ctx 含租户（ID+Code）→ 租户库连接池 tenant_{code}
//   - ctx 无租户 → 主库（t_tenant / casbin_rule / 菜单母本）
//
// 连接池 LRU 上限淘汰（tenant.max_pools）；池上同样注册 TenantPlugin
// （租户库表结构含 tenant_id 列，双重隔离：物理切库 + 判别列过滤）。
package tenantdb

import (
	"container/list"
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	dbConfig "devops-infra/config"
	client "devops-infra/database/mysql"
	"devops-system/internal/config"
	"devops-system/internal/db"
	"devops-system/internal/model"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

var currentResolver *resolver

// resolver 模式B数据源解析器。
type resolver struct {
	mysqlCfg  dbConfig.MysqlConfig
	tenantCfg config.TenantConfig

	mu    sync.Mutex
	pools map[string]*list.Element // tenant code → LRU 节点
	lru   list.List                // 前端最近使用
}

type poolEntry struct {
	code string
	gdb  *gorm.DB
}

// InitResolver 装配模式B解析器（tenant.mode=schema 时由 server/migrate 调用）。
func InitResolver(mysqlCfg dbConfig.MysqlConfig, tenantCfg config.TenantConfig) error {
	if tenantCfg.SchemaTemplate == "" {
		tenantCfg.SchemaTemplate = "tenant_{code}"
	}
	currentResolver = &resolver{
		mysqlCfg:  mysqlCfg,
		tenantCfg: tenantCfg,
		pools:     make(map[string]*list.Element),
	}
	slog.Info("模式B（schema）已启用：租户数据将路由至独立库", "template", tenantCfg.SchemaTemplate)
	return nil
}

// CloseResolver 关闭全部租户连接池（优雅退出链调用）。
func CloseResolver() {
	if currentResolver == nil {
		return
	}
	currentResolver.mu.Lock()
	defer currentResolver.mu.Unlock()
	for el := currentResolver.lru.Front(); el != nil; el = el.Next() {
		closePool(el.Value.(*poolEntry).gdb)
	}
	currentResolver.pools = nil
	currentResolver.lru.Init()
	currentResolver = nil
}

// SchemaEnabled 当前是否模式B。
func SchemaEnabled() bool { return currentResolver != nil }

// resolve 模式B路由：租户 ctx → 租户池；无租户 → 主库。
// 租户库打开失败时返回主库并记日志：租户表在主库不存在，查询自然失败（fail-closed）。
func (r *resolver) resolve(ctx context.Context) *gorm.DB {
	tc, ok := From(ctx)
	if !ok || tc.Code == "" {
		return client.Get().WithContext(ctx)
	}
	gdb, err := r.pool(tc.Code)
	if err != nil {
		slog.Error("租户库连接失败（回退主库，租户表查询将失败）", "code", tc.Code, "err", err)
		return client.Get().WithContext(ctx)
	}
	return gdb.WithContext(ctx)
}

// pool 取（或惰性建）租户连接池；超 LRU 上限淘汰最久未用并关闭。
func (r *resolver) pool(code string) (*gorm.DB, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if el, ok := r.pools[code]; ok {
		r.lru.MoveToFront(el)
		return el.Value.(*poolEntry).gdb, nil
	}

	gdb, err := r.openPool(code)
	if err != nil {
		return nil, err
	}
	el := r.lru.PushFront(&poolEntry{code: code, gdb: gdb})
	r.pools[code] = el

	max := r.tenantCfg.MaxPools
	if max <= 0 {
		max = 64
	}
	for r.lru.Len() > max {
		last := r.lru.Back()
		if last == nil {
			break
		}
		entry := last.Value.(*poolEntry)
		r.lru.Remove(last)
		delete(r.pools, entry.code)
		slog.Warn("租户连接池超上限，淘汰最久未用", "code", entry.code, "max", max)
		closePool(entry.gdb)
	}
	return gdb, nil
}

// openPool 打开租户库连接（DSN 切库）并注册租户过滤插件。
func (r *resolver) openPool(code string) (*gorm.DB, error) {
	dsn := fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?charset=%s&parseTime=True&loc=Local",
		r.mysqlCfg.Username, r.mysqlCfg.Password, r.mysqlCfg.Host, r.mysqlCfg.Port,
		r.dbName(code), r.mysqlCfg.Charset,
	)
	gdb, err := gorm.Open(mysql.Open(dsn), &gorm.Config{
		Logger: gormlogger.Default.LogMode(gormlogger.Warn),
	})
	if err != nil {
		return nil, fmt.Errorf("打开租户库 %s 失败: %w", r.dbName(code), err)
	}
	sqlDB, err := gdb.DB()
	if err != nil {
		return nil, err
	}
	sqlDB.SetMaxIdleConns(r.mysqlCfg.MaxIdleConns)
	sqlDB.SetMaxOpenConns(r.mysqlCfg.MaxOpenConns)
	sqlDB.SetConnMaxLifetime(r.mysqlCfg.ConnMaxLifetime)
	if err := RegisterPlugin(gdb); err != nil {
		return nil, fmt.Errorf("租户库 %s 注册过滤插件失败: %w", r.dbName(code), err)
	}
	return gdb, nil
}

// dbName 库名模板展开：tenant_{code} → tenant_custa。
func (r *resolver) dbName(code string) string {
	if strings.Contains(r.tenantCfg.SchemaTemplate, "{code}") {
		return strings.Replace(r.tenantCfg.SchemaTemplate, "{code}", code, 1)
	}
	return r.tenantCfg.SchemaTemplate + code
}

// closePool 关闭连接池（尽力而为）。
func closePool(gdb *gorm.DB) {
	if sqlDB, err := gdb.DB(); err == nil {
		_ = sqlDB.Close()
	}
}

// tenantTables 租户库全量表（含 menu 拷贝，方案 5.4：role_menu 与 menu 同库自洽）。
func tenantTables() []any {
	return []any{
		&model.User{}, &model.Role{}, &model.Menu{}, &model.Dept{}, &model.Post{},
		&model.UserRole{}, &model.RoleMenu{},
	}
}

// ProvisionSchema 模式B建库（创建租户时调用；模式A 直接返回）：
// CREATE DATABASE → 建池 → AutoMigrate → 复合唯一索引 → 菜单母本 fan-out。
// 幂等：重复调用对已存在的库等同迁移+菜单同步。
func ProvisionSchema(ctx context.Context, code string) error {
	if currentResolver == nil {
		return nil // 模式A：无需建库
	}
	dbName := currentResolver.dbName(code)
	if err := client.Get().Exec(
		fmt.Sprintf("CREATE DATABASE IF NOT EXISTS `%s` DEFAULT CHARACTER SET utf8mb4", dbName),
	).Error; err != nil {
		return fmt.Errorf("创建租户库 %s 失败: %w", dbName, err)
	}

	gdb, err := currentResolver.pool(code)
	if err != nil {
		return err
	}
	if err := gdb.AutoMigrate(tenantTables()...); err != nil {
		return fmt.Errorf("租户库 %s 建表失败: %w", dbName, err)
	}
	if err := db.EnsureCompositeUniques(gdb); err != nil {
		return fmt.Errorf("租户库 %s 创建唯一索引失败: %w", dbName, err)
	}
	if err := syncMenus(client.Get(), gdb); err != nil {
		return fmt.Errorf("租户库 %s 菜单同步失败: %w", dbName, err)
	}
	slog.Info("租户库就绪", "database", dbName, "tables", len(tenantTables()))
	return nil
}

// syncMenus 菜单母本（主库）→ 租户库全量同步（migrate fan-out 复用；ID 原样保留）。
func syncMenus(master, tenant *gorm.DB) error {
	var menus []model.Menu
	if err := master.Find(&menus).Error; err != nil {
		return fmt.Errorf("读取菜单母本失败: %w", err)
	}
	return tenant.Transaction(func(tx *gorm.DB) error {
		if err := tx.Unscoped().Where("1 = 1").Delete(&model.Menu{}).Error; err != nil {
			return fmt.Errorf("清空租户菜单失败: %w", err)
		}
		if len(menus) == 0 {
			return nil
		}
		return tx.Create(&menus).Error
	})
}

// MigrateAllTenants 模式B migrate fan-out：遍历 t_tenant 逐库迁移+菜单同步（幂等）。
func MigrateAllTenants() error {
	if currentResolver == nil {
		return nil
	}
	var tenants []model.Tenant
	if err := client.Get().Find(&tenants).Error; err != nil {
		return fmt.Errorf("遍历租户失败: %w", err)
	}
	for i := range tenants {
		if err := ProvisionSchema(context.Background(), tenants[i].Code); err != nil {
			return fmt.Errorf("租户 %s(%d) 迁移失败: %w", tenants[i].Code, tenants[i].ID, err)
		}
	}
	slog.Info("模式B migrate fan-out 完成", "tenants", len(tenants))
	return nil
}

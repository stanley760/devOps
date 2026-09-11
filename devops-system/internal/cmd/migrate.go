// migrate 子命令：业务表 AutoMigrate + 复合唯一索引 + 号段注册 + 种子。
package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	infracfg "devops-infra/config"

	"devops-system/internal/config"
	"devops-system/internal/db"
	"devops-system/internal/model"
)

var migrateCmd = &cobra.Command{
	Use:   "migrate",
	Short: "建表（t_tenant + t_system_*）并初始化种子数据",
	RunE: func(cmd *cobra.Command, args []string) error {
		var cfg config.Config
		if err := infracfg.LoadInto(&cfg); err != nil {
			return err
		}
		if err := db.Init(cfg.Mysql); err != nil {
			return err
		}
		gdb := db.Get()

		if err := autoMigrate(gdb); err != nil {
			return fmt.Errorf("建表失败: %w", err)
		}
		if err := ensureUniqueIndexes(gdb); err != nil {
			return fmt.Errorf("创建唯一索引失败: %w", err)
		}
		if err := registerBizTags(gdb, cfg.Tenant); err != nil {
			return err
		}
		if err := seed(gdb); err != nil {
			return fmt.Errorf("种子数据初始化失败: %w", err)
		}
		fmt.Println("migrate 完成：t_tenant + 7 张 t_system_* 表已就绪")
		return nil
	},
}

// autoMigrate 全部业务表（幂等）。
func autoMigrate(gdb *gorm.DB) error {
	return gdb.AutoMigrate(
		&model.Tenant{},
		&model.User{},
		&model.Role{},
		&model.Menu{},
		&model.Dept{},
		&model.Post{},
		&model.UserRole{},
		&model.RoleMenu{},
	)
}

// compositeUniques 跨嵌入字段的复合唯一索引（租户内唯一，方案 5 节）。
// GORM tag 无法在共享的 BaseTenantModel.TenantID 上按表组合，故以 DDL 声明。
var compositeUniques = []struct {
	Table  string
	Name   string
	Column string
}{
	{"t_system_users", "uk_tenant_username", "tenant_id, username"},
	{"t_system_users", "uk_tenant_mobile", "tenant_id, mobile"},
	{"t_system_role", "uk_tenant_code", "tenant_id, code"},
	{"t_system_post", "uk_tenant_code", "tenant_id, code"},
}

// ensureUniqueIndexes 逐个检查并创建（information_schema 判存，MySQL/MariaDB 通用）。
func ensureUniqueIndexes(gdb *gorm.DB) error {
	for _, u := range compositeUniques {
		var cnt int64
		if err := gdb.Raw(
			"SELECT COUNT(*) FROM information_schema.statistics "+
				"WHERE table_schema = DATABASE() AND table_name = ? AND index_name = ?",
			u.Table, u.Name,
		).Scan(&cnt).Error; err != nil {
			return err
		}
		if cnt > 0 {
			continue
		}
		if err := gdb.Exec(
			fmt.Sprintf("CREATE UNIQUE INDEX %s ON %s (%s)", u.Name, u.Table, u.Column),
		).Error; err != nil {
			return fmt.Errorf("表 %s 创建 %s 失败: %w", u.Table, u.Name, err)
		}
	}
	return nil
}

// registerBizTags 同库部署时向 t_leaf_alloc 注册号段行（INSERT IGNORE 幂等；
// 模式B 库拆分后改走 id 服务管理 API，Roadmap）。
func registerBizTags(gdb *gorm.DB, cfg config.TenantConfig) error {
	tags := []string{"user", "role", "menu", "dept", "post", "tenant"}
	for _, tag := range tags {
		if err := gdb.Exec(
			"INSERT IGNORE INTO t_leaf_alloc (biz_tag, max_id, step, description) VALUES (?, 1, 1000, ?)",
			tag, "devops-system 消费方",
		).Error; err != nil {
			return fmt.Errorf("注册 biz_tag %s 失败: %w", tag, err)
		}
	}
	fmt.Printf("号段已注册：t_leaf_alloc biz_tag: %s\n", strings.Join(tags, ", "))
	return nil
}

// seed 种子数据（幂等：t_tenant 为空才执行）。
// 显式 ID（admin=1、部门 100 起、菜单 1-7 等）——不消耗号段。
func seed(gdb *gorm.DB) error {
	var cnt int64
	if err := gdb.Model(&model.Tenant{}).Count(&cnt).Error; err != nil {
		return err
	}
	if cnt > 0 {
		fmt.Println("种子已初始化，跳过（t_tenant 非空）")
		return nil
	}

	pwd, err := bcrypt.GenerateFromPassword([]byte("Admin@123"), 10)
	if err != nil {
		return fmt.Errorf("生成密码哈希失败: %w", err)
	}

	const tid = int64(1) // 默认租户 1
	return gdb.Transaction(func(tx *gorm.DB) error {
		// 租户：1=devOps / code=default（expire NULL 永不过期）
		if err := tx.Create(&model.Tenant{
			BaseModel: model.BaseModel{ID: 1, Creator: "seed"},
			Name:      "devOps",
			Code:      "default",
			Status:    0,
		}).Error; err != nil {
			return err
		}

		// 部门：100=devOps(root) → 101 技术部、102 运维部
		depts := []model.Dept{
			{BaseTenantModel: model.BaseTenantModel{BaseModel: model.BaseModel{ID: 100, Creator: "seed"}, TenantID: tid},
				Name: "devOps", Sort: 0, Status: 0},
			{BaseTenantModel: model.BaseTenantModel{BaseModel: model.BaseModel{ID: 101, Creator: "seed"}, TenantID: tid},
				Name: "技术部", ParentID: 100, Sort: 1, Status: 0},
			{BaseTenantModel: model.BaseTenantModel{BaseModel: model.BaseModel{ID: 102, Creator: "seed"}, TenantID: tid},
				Name: "运维部", ParentID: 100, Sort: 2, Status: 0},
		}
		if err := tx.Create(&depts).Error; err != nil {
			return err
		}

		// 岗位：1=ceo、2=se、3=ops、4=user
		posts := []model.Post{
			{BaseTenantModel: model.BaseTenantModel{BaseModel: model.BaseModel{ID: 1, Creator: "seed"}, TenantID: tid},
				Code: "ceo", Name: "董事长", Sort: 1, Status: 0},
			{BaseTenantModel: model.BaseTenantModel{BaseModel: model.BaseModel{ID: 2, Creator: "seed"}, TenantID: tid},
				Code: "se", Name: "架构师", Sort: 2, Status: 0},
			{BaseTenantModel: model.BaseTenantModel{BaseModel: model.BaseModel{ID: 3, Creator: "seed"}, TenantID: tid},
				Code: "ops", Name: "运维工程师", Sort: 3, Status: 0},
			{BaseTenantModel: model.BaseTenantModel{BaseModel: model.BaseModel{ID: 4, Creator: "seed"}, TenantID: tid},
				Code: "user", Name: "普通用户", Sort: 4, Status: 0},
		}
		if err := tx.Create(&posts).Error; err != nil {
			return err
		}

		// 角色：1=超级管理员（内置）、2=普通角色
		roles := []model.Role{
			{BaseTenantModel: model.BaseTenantModel{BaseModel: model.BaseModel{ID: 1, Creator: "seed"}, TenantID: tid},
				Name: "超级管理员", Code: "super_admin", Sort: 1, Type: 1, DataScope: 1, Status: 0},
			{BaseTenantModel: model.BaseTenantModel{BaseModel: model.BaseModel{ID: 2, Creator: "seed"}, TenantID: tid},
				Name: "普通角色", Code: "common", Sort: 2, Type: 2, DataScope: 2, Status: 0},
		}
		if err := tx.Create(&roles).Error; err != nil {
			return err
		}

		// 菜单：1 系统管理（目录）→ 2-6 五管理 + 7 租户管理（type 2）+ 各自按钮（type 3）
		menus := seedMenus()
		if err := tx.Create(&menus).Error; err != nil {
			return err
		}

		// 用户：1=admin / Admin@123（dept 101，post_ids [4]）
		if err := tx.Create(&model.User{
			BaseTenantModel: model.BaseTenantModel{BaseModel: model.BaseModel{ID: 1, Creator: "seed"}, TenantID: tid},
			Username:        "admin",
			Password:        string(pwd),
			Nickname:        "admin",
			DeptID:          101,
			PostIDs:         "[4]",
			Email:           "admin@devops.local",
			Sex:             0,
			Status:          0,
		}).Error; err != nil {
			return err
		}

		// 授权：admin → super_admin；super_admin → 全量菜单（含菜单 7）
		if err := tx.Create(&model.UserRole{TenantID: tid, UserID: 1, RoleID: 1}).Error; err != nil {
			return err
		}
		roleMenus := make([]model.RoleMenu, 0, len(menus))
		for _, m := range menus {
			roleMenus = append(roleMenus, model.RoleMenu{TenantID: tid, RoleID: 1, MenuID: m.ID})
		}
		if err := tx.Create(&roleMenus).Error; err != nil {
			return err
		}

		fmt.Println("种子完成：租户 default(1)、admin、部门 100-102、岗位 1-4、角色 super_admin/common、菜单树")
		return nil
	})
}

// seedMenus 菜单树（对齐 yudao 精简版；租户管理 permission 留空——不派生 p 行）。
func seedMenus() []model.Menu {
	base := func(id int64, name string, typ int8, parent int64, sort int, path, icon, component, permission string) model.Menu {
		return model.Menu{
			BaseModel:  model.BaseModel{ID: id, Creator: "seed"},
			Name:       name,
			Permission: permission,
			Type:       typ,
			Sort:       sort,
			ParentID:   parent,
			Path:       path,
			Icon:       icon,
			Component:  component,
			Status:     0,
			Visible:    true,
			AlwaysShow: true,
		}
	}
	btn := func(id int64, parent int64, sort int, name, permission string) model.Menu {
		m := base(id, name, 3, parent, sort, "", "", "", permission)
		m.AlwaysShow = false
		return m
	}
	return []model.Menu{
		// 1 系统管理（目录）
		base(1, "系统管理", 1, 0, 1, "/system", "ep:setting", "", ""),
		// 2-6 管理菜单（type 2）
		base(2, "用户管理", 2, 1, 1, "user", "ep:user", "system/user/index", "system:user:list"),
		base(3, "角色管理", 2, 1, 2, "role", "ep:key", "system/role/index", "system:role:list"),
		base(4, "菜单管理", 2, 1, 3, "menu", "ep:menu", "system/menu/index", "system:menu:list"),
		base(5, "部门管理", 2, 1, 4, "dept", "ep:office-building", "system/dept/index", "system:dept:list"),
		base(6, "岗位管理", 2, 1, 5, "post", "ep:postcard", "system/post/index", "system:post:list"),
		// 7 租户管理（平台路由仅导航，permission 留空）
		base(7, "租户管理", 2, 1, 6, "tenant", "ep:coin", "platform/tenant/index", ""),

		// 用户按钮 21-25
		btn(21, 2, 1, "用户查询", "system:user:query"),
		btn(22, 2, 2, "用户新增", "system:user:create"),
		btn(23, 2, 3, "用户修改", "system:user:update"),
		btn(24, 2, 4, "用户删除", "system:user:delete"),
		btn(25, 2, 5, "重置密码", "system:user:update-password"),
		// 角色按钮 31-34
		btn(31, 3, 1, "角色查询", "system:role:query"),
		btn(32, 3, 2, "角色新增", "system:role:create"),
		btn(33, 3, 3, "角色修改", "system:role:update"),
		btn(34, 3, 4, "角色删除", "system:role:delete"),
		// 菜单按钮 41-44
		btn(41, 4, 1, "菜单查询", "system:menu:query"),
		btn(42, 4, 2, "菜单新增", "system:menu:create"),
		btn(43, 4, 3, "菜单修改", "system:menu:update"),
		btn(44, 4, 4, "菜单删除", "system:menu:delete"),
		// 部门按钮 51-54
		btn(51, 5, 1, "部门查询", "system:dept:query"),
		btn(52, 5, 2, "部门新增", "system:dept:create"),
		btn(53, 5, 3, "部门修改", "system:dept:update"),
		btn(54, 5, 4, "部门删除", "system:dept:delete"),
		// 岗位按钮 61-64
		btn(61, 6, 1, "岗位查询", "system:post:query"),
		btn(62, 6, 2, "岗位新增", "system:post:create"),
		btn(63, 6, 3, "岗位修改", "system:post:update"),
		btn(64, 6, 4, "岗位删除", "system:post:delete"),
	}
}

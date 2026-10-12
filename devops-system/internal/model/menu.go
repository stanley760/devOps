// t_system_menu 菜单表（全局表，无 tenant_id，对齐 yudao）。
// 不挂 api_path/api_method：API 鉴权走「权限标识」模式（Casbin 按 permission 判定）。
package model

// Menu 菜单。type：1目录 2菜单 3按钮。
type Menu struct {
	BaseModel
	Name          string `gorm:"size:50;not null"`
	Permission    string `gorm:"size:100"` // 权限标识，如 system:user:create；目录/租户管理留空
	Type          int8   `gorm:"not null"`
	Sort          int
	ParentID      int64  `gorm:"index;default:0"`
	Path          string `gorm:"size:200"`
	Icon          string `gorm:"size:100"`
	Component     string `gorm:"size:255"`
	ComponentName string `gorm:"size:255;column:component_name"`
	Status        int8   `gorm:"not null;default:1"`     // 0停用 1正常
	Visible       bool   `gorm:"not null;default:true"`  // 是否显示
	KeepAlive     bool   `gorm:"not null;default:false"` // 是否缓存
	AlwaysShow    bool   `gorm:"not null;default:true"`  // 目录始终显示
}

// TableName 表名。
func (Menu) TableName() string { return "t_system_menu" }

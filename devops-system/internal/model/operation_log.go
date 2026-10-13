package model

type OperationLog struct {
	BaseTenantModel
	Username string `gorm:"size:30;not null;index"` // 尝试登录的用户名（失败也记录）
	UserID   int64  `gorm:"not null;default:0;index"`
}

func (OperationLog) TableName() string { return "t_system_operation_log" }

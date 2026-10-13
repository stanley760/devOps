package model

type OperationLog struct {
	BaseTenantModel
	TraceId       string `gorm:"size:100;not null;index"`            // 链路追踪编号
	UserID        int64  `gorm:"not null;default:0;index"`            // 用户编号
	UserType      int64  `gorm:"column:user_type;not null;default:0"` // 用户类型
	Module        string `gorm:"size:50;not null;default:''"`
	SubModule     string `gorm:"size:50;not null;default:''"`
	BizID         int64  `gorm:"column:biz_id;not null;default:0"`
	Contents      string `gorm:"type:text"`
	Extra         string `gorm:"type:text"`
	RequestMethod string `gorm:"size:16"`
	RequestURL    string `gorm:"size:255"`
	UserIP        string `gorm:"size:50;index"`
	UserAgent     string `gorm:"size:512"`
}

func (OperationLog) TableName() string { return "t_system_operation_log" }

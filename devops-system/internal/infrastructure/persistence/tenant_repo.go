package persistence

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"

	"github.com/stanley760/devops/devops-common/domain"
	"github.com/stanley760/devops/devops-common/domain/gormx"
	"github.com/stanley760/devops/devops-infra/errs"

	"github.com/stanley760/devops/devops-system/internal/domain/auditlog"
	"github.com/stanley760/devops/devops-system/internal/domain/iam"
	"github.com/stanley760/devops/devops-system/internal/model"
)

// tenantRepo 是 iam.TenantRepository 的 GORM 实现。
//
// t_tenant 是全局表（租户注册表本身，无 tenant_id），故租户插件不注入；
// 其余租户内表仍靠插件过滤。
type tenantRepo struct{}

// NewTenantRepository 构造租户仓储。
func NewTenantRepository() iam.TenantRepository { return tenantRepo{} }

func (tenantRepo) Save(ctx context.Context, t *iam.Tenant) error {
	if t.ID == 0 {
		if err := db(ctx).Create(toTenantPO(t)).Error; err != nil {
			return errs.Wrap(err, errs.CodeInternal, "创建租户失败")
		}
		return nil
	}
	res := db(ctx).Model(&model.Tenant{}).Where("id = ?", t.ID).Updates(map[string]any{
		"name":           t.Name,
		"contact_name":   t.ContactName,
		"contact_mobile": t.ContactMobile,
		"status":         t.Status,
		"expire_time":    t.ExpireTime,
		"remark":         t.Remark,
	})
	if res.Error != nil {
		return errs.Wrap(res.Error, errs.CodeInternal, "更新租户失败")
	}
	if res.RowsAffected == 0 {
		return errs.New(errs.CodeNotFound, "租户不存在")
	}
	return appendEvents(ctx, t.PullEvents()...)
}

func (tenantRepo) Get(ctx context.Context, id int64) (*iam.Tenant, error) {
	var po model.Tenant
	if err := db(ctx).Where("id = ?", id).First(&po).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errs.New(errs.CodeNotFound, "租户不存在")
		}
		return nil, errs.Wrap(err, errs.CodeInternal, "查询租户失败")
	}
	return toTenantDomain(&po), nil
}

// GetByCode 按编码取租户（登录入参 tenant_code 的解析路径）。
//
// 登录场景 ctx 尚未解析租户，租户插件对全局表本就不注入，故此处安全。
func (tenantRepo) GetByCode(ctx context.Context, code string) (*iam.Tenant, error) {
	var po model.Tenant
	if err := db(ctx).Where("code = ?", code).First(&po).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errs.New(errs.CodeNotFound, "租户不存在")
		}
		return nil, errs.Wrap(err, errs.CodeInternal, "查询租户失败")
	}
	return toTenantDomain(&po), nil
}

func (tenantRepo) List(ctx context.Context, f iam.TenantFilter) (domain.PageResult[*iam.Tenant], error) {
	conn := db(ctx).Model(&model.Tenant{})
	if f.Name != "" {
		conn = conn.Where("name LIKE ?", "%"+f.Name+"%")
	}
	if f.Code != "" {
		conn = conn.Where("code = ?", f.Code)
	}
	if f.Status != nil {
		conn = conn.Where("status = ?", *f.Status)
	}
	page, err := gormx.FindPage[model.Tenant](conn.Order("id DESC"), f.PageParam)
	if err != nil {
		return domain.PageResult[*iam.Tenant]{}, errs.Wrap(err, errs.CodeInternal, "查询租户列表失败")
	}
	return domain.MapPage(page, func(po model.Tenant) *iam.Tenant { return toTenantDomain(&po) }), nil
}

// Delete 软删租户并改写 code 绕开全局唯一索引 uk_code。
func (tenantRepo) Delete(ctx context.Context, t *iam.Tenant, operator string) error {
	res := db(ctx).Model(&model.Tenant{}).Where("id = ?", t.ID).Updates(map[string]any{
		"code":       iam.SoftDeletedCode(t.Code, t.ID),
		"deleted_at": time.Now(),
		"updater":    operator,
	})
	if res.Error != nil {
		return errs.Wrap(res.Error, errs.CodeInternal, "删除租户失败")
	}
	return appendEvents(ctx, t.PullEvents()...)
}

func (tenantRepo) CodeExists(ctx context.Context, code string, excludeID int64) (bool, error) {
	return existsBy(ctx, &model.Tenant{}, "code", code, excludeID, "检查租户编码唯一性失败")
}

func (tenantRepo) IsAnyUserExists(ctx context.Context, tenantID int64) (bool, error) {
	// 显式带 tenant_id：跨租户表查询必须自己表达范围（租户插件按 ctx 注入，
	// 而此处 ctx 是租户管理侧的全局 ctx，不是目标租户的 ctx）。
	var cnt int64
	if err := db(ctx).Model(&model.User{}).
		Where("tenant_id = ?", tenantID).Count(&cnt).Error; err != nil {
		return false, errs.Wrap(err, errs.CodeInternal, "查询租户用户失败")
	}
	return cnt > 0, nil
}

func toTenantPO(t *iam.Tenant) *model.Tenant {
	return &model.Tenant{
		BaseModel:     model.BaseModel{Updater: t.Code},
		Name:          t.Name,
		Code:          t.Code,
		ContactName:   t.ContactName,
		ContactMobile: t.ContactMobile,
		Status:        t.Status,
		ExpireTime:    t.ExpireTime,
		Remark:        t.Remark,
	}
}

func toTenantDomain(po *model.Tenant) *iam.Tenant {
	return &iam.Tenant{
		ID:            po.ID,
		Name:          po.Name,
		Code:          po.Code,
		ContactName:   po.ContactName,
		ContactMobile: po.ContactMobile,
		Status:        po.Status,
		ExpireTime:    po.ExpireTime,
		Remark:        po.Remark,
		CreateTime:    po.CreateTime,
		UpdateTime:    po.UpdateTime,
	}
}

// 审计行列宽（与 model gorm tag 对齐；超长截断保行不丢）。
// 改造前分散在 dao/login_log.go:18-23，此处保持同一口径。
const (
	loginLogMaxUsername  = 30
	loginLogMaxIP        = 50
	loginLogMaxUserAgent = 512
	loginLogMaxGeo       = 64
)

// loginLogRepo 是 auditlog.LoginLogWriter + LoginLogReader 的 GORM 实现。
type loginLogRepo struct{}

// NewLoginLogRepository 构造登录日志仓储。
func NewLoginLogRepository() auditlog.LoginLogWriter { return loginLogRepo{} }

// Append 落一行登录审计。
//
// 尽力而为语义保留：审计写入失败不影响登录结果，仅记告警。
// 落库前按列宽截断——网关侧对 UA/Geo 无长度约束，MySQL 严格模式下
// 超长会整行被拒（丢审计行比截断更不可接受）。
func (loginLogRepo) Append(ctx context.Context, l *auditlog.LoginLog) error {
	if l == nil {
		return nil
	}
	po := &model.LoginLog{
		Username:    auditlog.ClipRunes(l.Username, loginLogMaxUsername),
		IP:          auditlog.ClipRunes(l.IP, loginLogMaxIP),
		UserAgent:   auditlog.ClipRunes(l.UserAgent, loginLogMaxUserAgent),
		GeoCountry:  auditlog.ClipRunes(l.GeoCountry, loginLogMaxGeo),
		GeoProvince: auditlog.ClipRunes(l.GeoProvince, loginLogMaxGeo),
		GeoCity:     auditlog.ClipRunes(l.GeoCity, loginLogMaxGeo),
		GeoIsp:      auditlog.ClipRunes(l.GeoISP, loginLogMaxGeo),
		UserID:      l.UserID,
		Result:      l.Result,
		Code:        l.Code,
	}
	if err := db(ctx).Create(po).Error; err != nil {
		// 审计丢行需要可见性，故用 Warn 而非静默。
		return errs.Wrap(err, errs.CodeInternal, "登录审计落库失败")
	}
	return nil
}

// NewLoginLogReader 构造登录日志读端口。
func NewLoginLogReader() auditlog.LoginLogReader { return loginLogRepo{} }

func (loginLogRepo) List(ctx context.Context, f auditlog.LoginLogFilter) (domain.PageResult[*auditlog.LoginLog], error) {
	conn := db(ctx).Model(&model.LoginLog{})
	if f.Username != "" {
		conn = conn.Where("username LIKE ?", "%"+f.Username+"%")
	}
	if f.IP != "" {
		conn = conn.Where("ip LIKE ?", "%"+f.IP+"%")
	}
	if f.Result != nil {
		conn = conn.Where("result = ?", *f.Result)
	}
	if f.BeginTime != nil {
		conn = conn.Where("create_time >= ?", *f.BeginTime)
	}
	if f.EndTime != nil {
		conn = conn.Where("create_time <= ?", *f.EndTime)
	}
	page, err := gormx.FindPage[model.LoginLog](conn.Order("id DESC"), f.PageParam)
	if err != nil {
		return domain.PageResult[*auditlog.LoginLog]{}, errs.Wrap(err, errs.CodeInternal, "查询登录审计列表失败")
	}
	out := make([]*auditlog.LoginLog, 0, len(page.List))
	for i := range page.List {
		out = append(out, toLoginLogDomain(&page.List[i]))
	}
	return domain.PageResult[*auditlog.LoginLog]{
		List: out, Total: page.Total, PageNum: page.PageNum,
		PageSize: page.PageSize, Pages: page.Pages,
	}, nil
}

// DeleteByIDs 批量物理删除审计行。
func (loginLogRepo) DeleteByIDs(ctx context.Context, ids []int64) error {
	if len(ids) == 0 {
		return nil
	}
	if err := db(ctx).Unscoped().Where("id IN ?", ids).
		Delete(&model.LoginLog{}).Error; err != nil {
		return errs.Wrap(err, errs.CodeInternal, "删除登录审计失败")
	}
	return nil
}

// Clean 清空本租户全部审计行。
//
// Unscoped 之后租户插件不再注入条件，故必须显式带 tenant_id——
// 否则会跨租户清空（既有实现 dao/login_log.go:97 同样显式处理）。
func (loginLogRepo) Clean(ctx context.Context) error {
	tid, err := tenantOf(ctx)
	if err != nil {
		return err
	}
	if err := db(ctx).Unscoped().Where("tenant_id = ?", tid).
		Delete(&model.LoginLog{}).Error; err != nil {
		return errs.Wrap(err, errs.CodeInternal, "清空登录审计失败")
	}
	return nil
}

func toLoginLogDomain(po *model.LoginLog) *auditlog.LoginLog {
	return &auditlog.LoginLog{
		ID:          po.ID,
		TenantID:    po.TenantID,
		Username:    po.Username,
		UserID:      po.UserID,
		IP:          po.IP,
		UserAgent:   po.UserAgent,
		GeoCountry:  po.GeoCountry,
		GeoProvince: po.GeoProvince,
		GeoCity:     po.GeoCity,
		GeoISP:      po.GeoIsp,
		Result:      po.Result,
		Code:        po.Code,
		CreateTime:  po.CreateTime,
	}
}

// operationLogRepo 是 auditlog.OperationLogReader 的 GORM 实现。
//
// 只读 + 删除/清空：写入侧仍由 humakit 审计采集中间件经
// devops-infra/audit.Sink 落库（保持既有双轨制，不改变审计采集范围白名单）。
type operationLogRepo struct{}

// NewOperationLogReader 构造操作日志读端口。
func NewOperationLogReader() auditlog.OperationLogReader { return operationLogRepo{} }

func (operationLogRepo) List(ctx context.Context, f auditlog.OperationLogFilter) (domain.PageResult[*auditlog.OperationLog], error) {
	conn := db(ctx).Model(&model.OperationLog{})
	if f.Module != "" {
		conn = conn.Where("module = ?", f.Module)
	}
	if f.SubModule != "" {
		conn = conn.Where("sub_module = ?", f.SubModule)
	}
	if f.UserName != "" {
		conn = conn.Where("user_name LIKE ?", "%"+f.UserName+"%")
	}
	if f.UserIP != "" {
		conn = conn.Where("user_ip LIKE ?", "%"+f.UserIP+"%")
	}
	if f.Result != nil {
		conn = conn.Where("result = ?", *f.Result)
	}
	if f.BeginTime != nil {
		conn = conn.Where("create_time >= ?", *f.BeginTime)
	}
	if f.EndTime != nil {
		conn = conn.Where("create_time <= ?", *f.EndTime)
	}
	page, err := gormx.FindPage[model.OperationLog](conn.Order("id DESC"), f.PageParam)
	if err != nil {
		return domain.PageResult[*auditlog.OperationLog]{}, errs.Wrap(err, errs.CodeInternal, "查询操作审计列表失败")
	}
	return domain.MapPage(page, func(po model.OperationLog) *auditlog.OperationLog {
		return toOperationLogDomain(&po)
	}), nil
}

func (operationLogRepo) DeleteByIDs(ctx context.Context, ids []int64) error {
	if len(ids) == 0 {
		return nil
	}
	if err := db(ctx).Unscoped().Where("id IN ?", ids).
		Delete(&model.OperationLog{}).Error; err != nil {
		return errs.Wrap(err, errs.CodeInternal, "删除操作审计失败")
	}
	return nil
}

func (operationLogRepo) Clean(ctx context.Context) error {
	tid, err := tenantOf(ctx)
	if err != nil {
		return err
	}
	if err := db(ctx).Unscoped().Where("tenant_id = ?", tid).
		Delete(&model.OperationLog{}).Error; err != nil {
		return errs.Wrap(err, errs.CodeInternal, "清空操作审计失败")
	}
	return nil
}

func toOperationLogDomain(po *model.OperationLog) *auditlog.OperationLog {
	return &auditlog.OperationLog{
		ID:            po.ID,
		TenantID:      po.TenantID,
		TraceID:       po.TraceID,
		UserID:        po.UserID,
		UserName:      po.UserName,
		UserType:      int64(po.UserType),
		Module:        po.Module,
		SubModule:     po.SubModule,
		BizID:         po.BizID,
		Contents:      po.Contents,
		Extra:         po.Extra,
		RequestMethod: po.RequestMethod,
		RequestURL:    po.RequestURL,
		UserIP:        po.UserIP,
		UserAgent:     po.UserAgent,
		GeoCountry:    po.GeoCountry,
		GeoProvince:   po.GeoProvince,
		GeoCity:       po.GeoCity,
		GeoISP:        po.GeoISP,
		Result:        po.Result,
		Code:          po.Code,
		CreateTime:    po.CreateTime,
	}
}

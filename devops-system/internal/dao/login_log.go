// 登录审计：写入（service.Auth.login defer 调用）+ 管理端查询/删除/清空。
package dao

import (
	"context"
	"log/slog"

	"github.com/stanley760/devOps/devops-common/domain"
	"github.com/stanley760/devOps/devops-common/domain/gormx"
	"github.com/stanley760/devOps/devops-infra/errs"

	"github.com/stanley760/devOps/devops-system/internal/model"
	"github.com/stanley760/devOps/devops-system/internal/tenantdb"
	"github.com/stanley760/devOps/devops-system/internal/types"
)

// 审计行列宽（与 model.LoginLog gorm tag 对齐；超长截断保行不丢）。
const (
	loginLogMaxUsername  = 30
	loginLogMaxIP        = 50
	loginLogMaxUserAgent = 512
	loginLogMaxGeo       = 64
)

// CreateLoginLog 落一行登录审计。尽力而为：审计写入失败不影响登录，
// 仅 slog 告警（与 TouchLogin 同语义，但审计丢行需要可见性）。
// 调用方须已注入租户 ctx（TenantPlugin fail-closed，无租户 ctx 会报错并被此处吞掉）。
// 入参来自请求头（UA/Geo）与请求体（用户名），网关侧无长度约束：落库前按列宽
// 截断，杜绝 MySQL 严格模式下整行被拒（审计行丢失比截断更不可接受）。
func CreateLoginLog(ctx context.Context, log *model.LoginLog) {
	if log == nil {
		return
	}
	log.Username = clipRunes(log.Username, loginLogMaxUsername)
	log.IP = clipRunes(log.IP, loginLogMaxIP)
	log.UserAgent = clipRunes(log.UserAgent, loginLogMaxUserAgent)
	log.GeoCountry = clipRunes(log.GeoCountry, loginLogMaxGeo)
	log.GeoProvince = clipRunes(log.GeoProvince, loginLogMaxGeo)
	log.GeoCity = clipRunes(log.GeoCity, loginLogMaxGeo)
	log.GeoIsp = clipRunes(log.GeoIsp, loginLogMaxGeo)

	if err := tenantdb.DB(ctx).Create(log).Error; err != nil {
		slog.WarnContext(ctx, "登录审计落库失败", "username", log.Username, "err", err)
	}
}

// ListLoginLogs 分页查询登录审计（筛选 username/ip/result/时间范围，新行在前）。
func ListLoginLogs(ctx context.Context, q types.LoginLogPageQuery) (domain.PageResult[model.LoginLog], error) {
	db := tenantdb.DB(ctx).Model(&model.LoginLog{})
	if q.Username != "" {
		db = db.Where("username LIKE ?", "%"+q.Username+"%")
	}
	if q.IP != "" {
		db = db.Where("ip LIKE ?", "%"+q.IP+"%")
	}
	if q.Result != nil {
		db = db.Where("result = ?", *q.Result)
	}
	if q.BeginTime != nil {
		db = db.Where("create_time >= ?", *q.BeginTime)
	}
	if q.EndTime != nil {
		db = db.Where("create_time <= ?", *q.EndTime)
	}

	page, err := gormx.FindPage[model.LoginLog](
		db.Order("id DESC"),
		domain.PageParam{PageNum: q.PageNum, PageSize: q.PageSize},
	)
	if err != nil {
		return domain.PageResult[model.LoginLog]{}, errs.Wrap(err, errs.CodeInternal, "查询登录审计列表失败")
	}
	return page, nil
}

// DeleteLoginLogs 批量物理删除审计行（tenant_id 由 TenantPlugin 注入，跨租户 ID 不可达）。
// 返回实际删除行数。
func DeleteLoginLogs(ctx context.Context, ids []int64) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	res := tenantdb.DB(ctx).Unscoped().
		Where("id IN ?", ids).Delete(&model.LoginLog{})
	if res.Error != nil {
		return 0, errs.Wrap(res.Error, errs.CodeInternal, "删除登录审计失败")
	}
	return res.RowsAffected, nil
}

// CleanLoginLogs 清空本租户全部审计行（运维留存量管控入口），返回删除行数。
// 物理删除：审计行无软删语义，留下 deleted_at 行只会让表继续膨胀。
func CleanLoginLogs(ctx context.Context) (int64, error) {
	tid, err := tenantOf(ctx)
	if err != nil {
		return 0, err
	}
	res := tenantdb.DB(ctx).Unscoped().
		Where("tenant_id = ?", tid).Delete(&model.LoginLog{})
	if res.Error != nil {
		return 0, errs.Wrap(res.Error, errs.CodeInternal, "清空登录审计失败")
	}
	return res.RowsAffected, nil
}

// clipRunes 按字符数截断（MySQL varchar 按字符计长；按字节截会截出非法 UTF-8，
// 中文归属地如「黑龙江省」正是高风险场景）。len(s) 字节数不超 n 时必不超 n 字符，快路径直接返回。
func clipRunes(s string, n int) string {
	if len(s) <= n {
		return s
	}
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

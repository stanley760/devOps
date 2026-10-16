// Package dao 数据访问层（C 端）。
//
// 与 devops-system/internal/dao 的差异：无租户过滤——C 端单租户，
// 故不需要 tenantdb.DB(ctx) 包装，也不做「ctx 缺租户即 fail-fast」检查。
//
// 所有查询都经 gorm.ErrRecordNotFound 判定「不存在」并转成 errs.BizError，
// 避免上层拿到 GORM 原始错误（会泄漏 SQL 细节给前端）。
package dao

import (
	"context"
	"errors"

	"gorm.io/gorm"

	"github.com/stanley760/devops/devops-infra/errs"

	client "github.com/stanley760/devops/devops-infra/database/mysql"

	"github.com/stanley760/devops/devops-user/internal/model"
)

// db 返回全局 GORM 客户端（未 Init 时为 nil，由调用方的装配保证非空）。
func db(_ context.Context) *gorm.DB { return client.Get() }

// GetUserByUsername 按用户名取用户。
//
// ⚠️ 文案「用户名或密码错误」：登录失败与用户不存在**必须**返回同一错误，
// 否则攻击者可据此枚举出哪些用户名存在。
func GetUserByUsername(ctx context.Context, username string) (*model.User, error) {
	var u model.User
	err := db(ctx).Where("username = ?", username).First(&u).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			// 与 service.Login 的密码错误分支**必须完全一致**（码 + 文案），
			// 否则攻击者可通过响应差异枚举出哪些用户名存在。
			return nil, errs.New(errs.CodeUnauthorized, "账号或密码错误")
		}
		return nil, errs.Wrap(err, errs.CodeInternal, "查询用户失败")
	}
	return &u, nil
}

// GetUserByID 按 ID 取用户。
func GetUserByID(ctx context.Context, userID int64) (*model.User, error) {
	var u model.User
	err := db(ctx).Where("id = ?", userID).First(&u).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errs.New(errs.CodeNotFound, "用户不存在")
		}
		return nil, errs.Wrap(err, errs.CodeInternal, "查询用户失败")
	}
	return &u, nil
}

// IsUsernameTaken 用户名是否已被占用（注册前的预检，给出友好文案）。
//
// 真正的唯一性由数据库唯一索引 uk_username 保证：本函数只是提前拦截，
// 竞态下仍可能两个请求同时通过，最终以数据库约束为准（见 CreateUser）。
func IsUsernameTaken(ctx context.Context, username string) (bool, error) {
	var cnt int64
	if err := db(ctx).Model(&model.User{}).Where("username = ?", username).Count(&cnt).Error; err != nil {
		return false, errs.Wrap(err, errs.CodeInternal, "查询用户名失败")
	}
	return cnt > 0, nil
}

// CreateUser 新增用户。
//
// ⚠️ 唯一冲突（uk_username / uk_email / uk_mobile）必须按**具体约束**区分文案：
// 笼统映射成「用户名已被注册」会让用户填了邮箱却看到这句，明显误导
// （实测踩过：邮箱重复时提示「用户名已被注册」）。
func CreateUser(ctx context.Context, u *model.User) error {
	u.Normalize()
	if err := db(ctx).Create(u).Error; err != nil {
		if dup, index := duplicateField(err); dup {
			// index 是**索引名**（uk_username / uk_email / uk_mobile），非字段名。
			switch index {
			case "uk_username":
				return errs.New(errs.CodeParam, "用户名已被注册")
			case "uk_email":
				return errs.New(errs.CodeParam, "邮箱已被其他账号绑定")
			case "uk_mobile":
				return errs.New(errs.CodeParam, "手机号已被其他账号绑定")
			default:
				return errs.New(errs.CodeParam, "该信息已被其他账号绑定")
			}
		}
		return errs.Wrap(err, errs.CodeInternal, "创建用户失败")
	}
	return nil
}

// UpdateProfile 按字段更新资料。
//
// 用 map 而非 struct 更新，避免「未传的字段被零值覆盖」——
// 部分更新接口里这是最常见的 bug 来源。空 map 直接返回，避免发出无意义的 UPDATE。
func UpdateProfile(ctx context.Context, userID int64, fields map[string]any) error {
	if len(fields) == 0 {
		return nil
	}
	res := db(ctx).Model(&model.User{}).Where("id = ?", userID).Updates(fields)
	if res.Error != nil {
		if dup, index := duplicateField(res.Error); dup {
			// index 是索引名（uk_email / uk_mobile），非字段名。
			switch index {
			case "uk_email":
				return errs.New(errs.CodeParam, "邮箱已被其他账号绑定")
			case "uk_mobile":
				return errs.New(errs.CodeParam, "手机号已被其他账号绑定")
			default:
				return errs.New(errs.CodeParam, "该信息已被其他账号绑定")
			}
		}
		return errs.Wrap(res.Error, errs.CodeInternal, "更新资料失败")
	}
	if res.RowsAffected == 0 {
		return errs.New(errs.CodeNotFound, "用户不存在")
	}
	return nil
}

// SetDisabled 封禁/解封（管理侧接口用；当前骨架未暴露 HTTP 端点）。
func SetDisabled(ctx context.Context, userID int64, disabled bool) error {
	res := db(ctx).Model(&model.User{}).Where("id = ?", userID).Update("disabled", disabled)
	if res.Error != nil {
		return errs.Wrap(res.Error, errs.CodeInternal, "更新封禁状态失败")
	}
	if res.RowsAffected == 0 {
		return errs.New(errs.CodeNotFound, "用户不存在")
	}
	return nil
}

// UpdatePassword 更新口令哈希。
//
// 不校验旧口令——那是 service 层的职责（先校验再调本方法）。
// 用 Model+Where 而非 Save，避免 Save 会写零值覆盖其他列。
func UpdatePassword(ctx context.Context, userID int64, hashed string) error {
	res := db(ctx).Model(&model.User{}).Where("id = ?", userID).Update("password", hashed)
	if res.Error != nil {
		return errs.Wrap(res.Error, errs.CodeInternal, "更新口令失败")
	}
	if res.RowsAffected == 0 {
		return errs.New(errs.CodeNotFound, "用户不存在")
	}
	return nil
}

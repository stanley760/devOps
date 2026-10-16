package dao

import (
	"context"
	"errors"

	"gorm.io/gorm"

	"github.com/stanley760/devops/devops-infra/errs"

	"github.com/stanley760/devops/devops-user/internal/model"
)

// GetOAuthIdentity 按 (provider, provider_user_id) 查三方绑定。
// 返回 nil, nil 表示尚未绑定（不是错误）。
func GetOAuthIdentity(ctx context.Context, provider model.OAuthProvider, providerUserID string) (*model.OAuthIdentity, error) {
	var m model.OAuthIdentity
	err := db(ctx).Where("provider = ? AND provider_user_id = ?", provider, providerUserID).First(&m).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, errs.Wrap(err, errs.CodeInternal, "查询三方绑定失败")
	}
	return &m, nil
}

// ListOAuthIdentities 列某用户的全部三方绑定（设置页展示用）。
func ListOAuthIdentities(ctx context.Context, userID int64) ([]model.OAuthIdentity, error) {
	var list []model.OAuthIdentity
	if err := db(ctx).Where("user_id = ?", userID).Find(&list).Error; err != nil {
		return nil, errs.Wrap(err, errs.CodeInternal, "查询三方绑定列表失败")
	}
	return list, nil
}

// CreateOAuthIdentity 新增绑定。
//
// 唯一冲突转为可预期的业务错误：uk_provider_provider_id 冲突意味着
// 该三方账号已绑到别的本地用户（账号劫持信号，**必须拒绝**）。
func CreateOAuthIdentity(ctx context.Context, m *model.OAuthIdentity) error {
	if err := db(ctx).Create(m).Error; err != nil {
		if dup, index := duplicateField(err); dup {
			switch index {
			case "uk_provider_provider_id":
				return errs.New(errs.CodeParam, "该三方账号已绑定其他用户")
			case "uk_user_provider":
				return errs.New(errs.CodeParam, "该三方提供方已绑定过")
			default:
				return errs.New(errs.CodeParam, "绑定信息冲突")
			}
		}
		return errs.Wrap(err, errs.CodeInternal, "创建三方绑定失败")
	}
	return nil
}

// UpdateOAuthToken 刷新三方 token 与展示信息（每次登录都更新快照）。
func UpdateOAuthToken(ctx context.Context, id int64, accessToken, refreshToken string, expiresAt int64, nickname, avatarURL string) error {
	updates := map[string]any{
		"access_token": accessToken,
		"expires_at":   expiresAt,
	}
	if refreshToken != "" {
		updates["refresh_token"] = refreshToken
	}
	if nickname != "" {
		updates["nickname"] = nickname
	}
	if avatarURL != "" {
		updates["avatar_url"] = avatarURL
	}
	if err := db(ctx).Model(&model.OAuthIdentity{}).Where("id = ?", id).Updates(updates).Error; err != nil {
		return errs.Wrap(err, errs.CodeInternal, "更新三方 token 失败")
	}
	return nil
}

// DeleteOAuthIdentity 解绑（用户主动或管理端操作）。
func DeleteOAuthIdentity(ctx context.Context, userID int64, provider model.OAuthProvider) error {
	res := db(ctx).Where("user_id = ? AND provider = ?", userID, provider).Delete(&model.OAuthIdentity{})
	if res.Error != nil {
		return errs.Wrap(res.Error, errs.CodeInternal, "解绑失败")
	}
	if res.RowsAffected == 0 {
		return errs.New(errs.CodeNotFound, "未绑定该三方账号")
	}
	return nil
}

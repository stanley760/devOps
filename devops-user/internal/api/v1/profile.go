package v1

import (
	"context"
	"net/http"

	commonResp "github.com/stanley760/devops/devops-common/response"
	"github.com/stanley760/devops/devops-infra/errs"

	"github.com/stanley760/devops/devops-user/internal/auth"
	"github.com/stanley760/devops/devops-user/internal/model"
	"github.com/stanley760/devops/devops-user/internal/service"
)

// ProfileHandler 用户资料接口。
type ProfileHandler struct {
	svc *service.Profile
}

// NewProfileHandler 构造资料 handler。
func NewProfileHandler(svc *service.Profile) *ProfileHandler {
	return &ProfileHandler{svc: svc}
}

// profileOutput 资料响应体。
type profileOutput struct {
	Body commonResp.BaseResponse[model.PublicProfile]
}

// Get GET /user/api/v1/profile —— 读取当前用户资料。
func (h *ProfileHandler) Get(ctx context.Context, _ *struct{}) (*profileOutput, error) {
	profile, err := h.svc.Get(ctx, auth.ClaimsFrom(ctx))
	if err != nil {
		return nil, errs.ToHumaError(err)
	}
	return &profileOutput{Body: commonResp.BaseResponse[model.PublicProfile]{
		Code: http.StatusOK,
		Msg:  "ok",
		Data: profile,
	}}, nil
}

// updateBody 资料修改入参。
//
// ⚠️ 无 id / user_id 字段：C 端行级归属校验靠「身份只来自 token」实现，
// 若允许客户端传 ID 再比对，等于把越权判断交给客户端参数。
//
// 指针 + `required:"false"`：nil 表示不修改该字段。huma 默认所有字段必填，
// 指针字段同样需显式标可选，否则缺字段即报校验失败（无法做部分更新）。
// `required` 必须写成独立 struct tag，写进 json tag 里无效（详见 auth.go 注释）。
type updateBody struct {
	Nickname *string `json:"nickname" required:"false" doc:"昵称，不传则不修改" maxLength:"32"`
	Email    *string `json:"email" required:"false" doc:"邮箱，不传则不修改"`
	Mobile   *string `json:"mobile" required:"false" doc:"手机号，不传则不修改"`
	Avatar   *string `json:"avatar" required:"false" doc:"头像地址，不传则不修改"`
}

// Update PUT /user/api/v1/profile —— 修改当前用户资料。
func (h *ProfileHandler) Update(ctx context.Context, in *struct {
	Body updateBody
}) (*profileOutput, error) {
	profile, err := h.svc.Update(ctx, auth.ClaimsFrom(ctx), service.UpdateInput{
		Nickname: in.Body.Nickname,
		Email:    in.Body.Email,
		Mobile:   in.Body.Mobile,
		Avatar:   in.Body.Avatar,
	})
	if err != nil {
		return nil, errs.ToHumaError(err)
	}
	return &profileOutput{Body: commonResp.BaseResponse[model.PublicProfile]{
		Code: http.StatusOK,
		Msg:  "ok",
		Data: profile,
	}}, nil
}

// changePasswordBody 改密入参。
type changePasswordBody struct {
	OldPassword string `json:"old_password" required:"true" doc:"原密码" minLength:"1" maxLength:"72"`
	NewPassword string `json:"new_password" required:"true" doc:"新密码，至少 8 位" minLength:"8" maxLength:"72"`
	// CurrentRefreshToken 选填：带上则保留当前设备（其余设备下线）。
	CurrentRefreshToken string `json:"current_refresh_token" required:"false" doc:"当前设备的刷新令牌；带上则保留本设备" maxLength:"128"`
	// KeepCurrentDevice 等价于「传了 current_refresh_token」的显式写法，
	// 便于客户端表达意图：改密后踢掉其他设备但保留本机。
	KeepCurrentDevice bool `json:"keep_current_device" required:"false" doc:"改密后保留本设备，其余设备强制重新登录"`
}

// okOutput 无业务数据的成功响应。
type okOutput struct {
	Body commonResp.BaseResponse[any]
}

// ChangePassword PUT /user/api/v1/profile/password —— 修改自己的口令。
//
// 旧口令必须正确：否则拿到 token 的人可直接改密永久占据账号。
// 响应提示「请重新登录」：口令变更后旧 access 仍在有效期内，
// 客户端应主动丢弃本地凭据重新登录。
func (h *ProfileHandler) ChangePassword(ctx context.Context, in *struct {
	Body changePasswordBody
}) (*okOutput, error) {
	claims := auth.ClaimsFrom(ctx)
	// 保留本设备时需要它的 refresh；两个字段任一为真即视为保留。
	keep := in.Body.CurrentRefreshToken
	if in.Body.KeepCurrentDevice && keep == "" {
		// 只说「保留本设备」但没给 refresh → 无法定位，视为不保留（全踢）。
		// 保守取向：宁可多踢一次，也不要留下未受控的例外。
		keep = ""
	}
	if err := h.svc.ChangePassword(ctx, claims, in.Body.OldPassword, in.Body.NewPassword, keep); err != nil {
		return nil, errs.ToHumaError(err)
	}
	msg := "密码已修改，所有设备已退出，请重新登录"
	if keep != "" {
		msg = "密码已修改，其他设备已退出"
	}
	return &okOutput{Body: commonResp.BaseResponse[any]{
		Code: http.StatusOK,
		Msg:  msg,
	}}, nil
}

package service

import (
	"errors"

	"golang.org/x/crypto/bcrypt"
)

// errPasswordTooLong 口令超过 bcrypt 的 72 字节上限。
var errPasswordTooLong = errors.New("password too long for bcrypt")

// bcryptCost 口令哈希成本。
//
// ⚠️ 取值权衡（bcrypt 的 cost 每 +1 计算量翻倍）：
//   - <10：现代 GPU/ASIC 上抗爆破能力不足，C 端开放注册尤其危险；
//   - >12：登录接口 P99 显著上升，且登录是高频路径。
//
// 10 是当前权衡值。若 C 端有明确的登录失败限流与账号级锁定策略，可上调到 12。
const bcryptCost = bcrypt.DefaultCost

// hashPassword 口令哈希（bcrypt，盐由 bcrypt 内部生成并编入结果）。
func hashPassword(plain string) (string, error) {
	// bcrypt 只取前 72 字节，超长口令会被静默截断——先显式拒绝更安全。
	if len(plain) > 72 {
		return "", errPasswordTooLong
	}
	h, err := bcrypt.GenerateFromPassword([]byte(plain), bcryptCost)
	if err != nil {
		return "", err
	}
	return string(h), nil
}

// checkPassword 校验口令。
//
// 用 bcrypt 的 CompareHashAndPassword（内部常数时间比较），不自行比对哈希串。
func checkPassword(plain, hashed string) bool {
	if len(plain) > 72 {
		return false
	}
	return bcrypt.CompareHashAndPassword([]byte(hashed), []byte(plain)) == nil
}

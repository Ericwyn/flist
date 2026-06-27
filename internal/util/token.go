package util

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
)

// GenerateToken 生成一个高熵的不透明令牌（32 字节随机值，Base64URL 编码）。
func GenerateToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// HashToken 对令牌取 SHA-256 并以 hex 返回，用作 sessions 表主键。
// 服务端只存哈希，不存明文令牌。
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// RandomPassword 生成一个随机初始密码（用于未配置管理员密码时）。
// 保证同时包含字母和数字，满足 validatePasswordStrength 校验。
func RandomPassword() (string, error) {
	for i := 0; i < 10; i++ {
		buf := make([]byte, 12)
		if _, err := rand.Read(buf); err != nil {
			return "", err
		}
		s := base64.RawURLEncoding.EncodeToString(buf)
		var hasLetter, hasDigit bool
		for _, r := range s {
			switch {
			case (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z'):
				hasLetter = true
			case r >= '0' && r <= '9':
				hasDigit = true
			}
		}
		if hasLetter && hasDigit {
			return s, nil
		}
	}
	// 极端情况下兜底：拼一个确定满足条件的。
	buf := make([]byte, 10)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	s := base64.RawURLEncoding.EncodeToString(buf)
	return "a1" + s, nil
}

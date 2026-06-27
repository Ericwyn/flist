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
// 返回的是 Base64URL 编码的随机串，长度足够且含字母数字。
func RandomPassword() (string, error) {
	buf := make([]byte, 12)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

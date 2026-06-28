package service

import (
	"encoding/base64"
	"fmt"
	"sync"
	"time"

	"flist/internal/util"

	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"
	"github.com/skip2/go-qrcode"
)

const twoFactorChallengeTTL = 5 * time.Minute

// twoFactorChallenge 是密码验证通过后的临时凭证，仅用于 2FA 验证步骤。
type twoFactorChallenge struct {
	userID    int64
	expiresAt time.Time
}

// challengeTracker 管理内存中的 2FA 挑战令牌。
type challengeTracker struct {
	mu        sync.Mutex
	challenges map[string]*twoFactorChallenge
}

func newChallengeTracker() *challengeTracker {
	return &challengeTracker{challenges: make(map[string]*twoFactorChallenge)}
}

// create 生成一个临时令牌并关联 userID，TTL 为 twoFactorChallengeTTL。
func (c *challengeTracker) create(userID int64) (string, error) {
	token, err := util.GenerateToken()
	if err != nil {
		return "", err
	}
	c.mu.Lock()
	c.challenges[token] = &twoFactorChallenge{
		userID:    userID,
		expiresAt: time.Now().Add(twoFactorChallengeTTL),
	}
	c.mu.Unlock()
	return token, nil
}

// consume 校验并消费临时令牌，返回 userID。过期或不存在返回 false。
func (c *challengeTracker) consume(token string) (int64, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	ch, ok := c.challenges[token]
	if !ok {
		return 0, false
	}
	delete(c.challenges, token)
	if time.Now().After(ch.expiresAt) {
		return 0, false
	}
	return ch.userID, true
}

// generateTOTPSecret 生成一个新的 TOTP 密钥，issuer 为 "flist"。
func generateTOTPSecret(username string) (secret string, key *otp.Key, err error) {
	key, err = totp.Generate(totp.GenerateOpts{
		Issuer:      "flist",
		AccountName: username,
	})
	if err != nil {
		return "", nil, fmt.Errorf("generate totp key: %w", err)
	}
	return key.Secret(), key, nil
}

// generateQRCode 将 otpauth URL 编码为 QR 码 PNG 的 base64 data URI。
func generateQRCode(otpURL string) (string, error) {
	png, err := qrcode.Encode(otpURL, qrcode.Medium, 256)
	if err != nil {
		return "", fmt.Errorf("encode qr: %w", err)
	}
	b64 := base64.StdEncoding.EncodeToString(png)
	return "data:image/png;base64," + b64, nil
}

// validateTOTP 校验 6 位验证码是否匹配当前时间窗口的 TOTP 值。
func validateTOTP(code, secret string) bool {
	return totp.Validate(code, secret)
}

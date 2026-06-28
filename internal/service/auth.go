package service

import (
	"errors"
	"log/slog"
	"sync"
	"time"
	"unicode"

	"flist/internal/model"
	"flist/internal/store"
	"flist/internal/util"

	"golang.org/x/crypto/bcrypt"
)

// 服务层错误，handler 据此映射错误码。
var (
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrAccountLocked      = errors.New("account locked")
	ErrWeakPassword       = errors.New("weak password")
	ErrUnauthorized       = errors.New("unauthorized")
	ErrPasswordTooLong    = errors.New("password too long")
	ErrInvalidUsername    = errors.New("invalid username")
	ErrUsernameTaken      = errors.New("username taken")
	ErrUserNotFound       = errors.New("user not found")
	ErrInvalidTOTP        = errors.New("invalid totp code")
	ErrTOTPAlreadyEnabled = errors.New("two-factor already enabled")
	ErrTOTPNotEnabled     = errors.New("two-factor not enabled")
	ErrTOTPNotSetup       = errors.New("totp secret not set up")
)

const (
	bcryptCost       = 10
	maxPasswordLen   = 72 // bcrypt 截断边界
	lockoutThreshold = 5
	lockoutDuration  = 15 * time.Minute
)

// AuthService 封装认证相关业务逻辑。
type AuthService struct {
	store      *store.Store
	sessionTTL time.Duration
	logger     *slog.Logger
	lockout    *lockoutTracker
	challenge  *challengeTracker
}

// NewAuthService 构造认证服务。
func NewAuthService(st *store.Store, sessionTTL time.Duration, logger *slog.Logger) *AuthService {
	if logger == nil {
		logger = slog.Default()
	}
	return &AuthService{
		store:      st,
		sessionTTL: sessionTTL,
		logger:     logger,
		lockout:    newLockoutTracker(),
		challenge:  newChallengeTracker(),
	}
}

// EnsureAdmin 在库中无用户时创建初始管理员。
// 返回是否新建、以及（当随机生成密码时）生成的明文密码。
func (a *AuthService) EnsureAdmin(username, password string) (created bool, generatedPass string, err error) {
	n, err := a.store.CountUsers()
	if err != nil {
		return false, "", err
	}
	if n > 0 {
		return false, "", nil
	}

	if password == "" {
		password, err = util.RandomPassword()
		if err != nil {
			return false, "", err
		}
		generatedPass = password
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcryptCost)
	if err != nil {
		return false, "", err
	}
	if _, err := a.store.CreateUser(username, string(hash)); err != nil {
		return false, "", err
	}
	return true, generatedPass, nil
}

// LoginResult 登录成功的返回。
type LoginResult struct {
	Token             string
	ExpiresAt         time.Time
	User              *model.User
	RequiresTwoFactor bool   // 为 true 时 Token/ExpiresAt/User 为空
	TempToken         string // 仅 RequiresTwoFactor=true 时有值
}

// Login 校验凭证并签发会话令牌。clientIP 用于登录失败锁定。
// 若用户启用了 2FA，则不直接返回会话令牌，而是返回一个临时令牌供后续 verify-2fa 使用。
func (a *AuthService) Login(username, password, clientIP string) (*LoginResult, error) {
	key := clientIP + "|" + username
	if a.lockout.isLocked(key) {
		return nil, ErrAccountLocked
	}
	if len(password) > maxPasswordLen {
		a.lockout.recordFailure(key)
		return nil, ErrInvalidCredentials
	}

	user, err := a.store.GetUserByUsername(username)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			a.lockout.recordFailure(key)
			return nil, ErrInvalidCredentials
		}
		return nil, err
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(password)); err != nil {
		a.lockout.recordFailure(key)
		return nil, ErrInvalidCredentials
	}

	a.lockout.reset(key)

	// 2FA 已启用：不签发会话令牌，返回临时挑战令牌。
	if user.TwoFactorEnabled && user.TOTPSecret != "" {
		tempToken, err := a.challenge.create(user.ID)
		if err != nil {
			return nil, err
		}
		return &LoginResult{RequiresTwoFactor: true, TempToken: tempToken}, nil
	}

	token, err := util.GenerateToken()
	if err != nil {
		return nil, err
	}
	expiresAt := time.Now().Add(a.sessionTTL)
	if err := a.store.CreateSession(util.HashToken(token), user.ID, expiresAt); err != nil {
		return nil, err
	}

	return &LoginResult{Token: token, ExpiresAt: expiresAt, User: user}, nil
}

// Validate 校验令牌并返回对应用户与会话 ID（哈希）。过期会话会被删除。
func (a *AuthService) Validate(token string) (*model.User, string, error) {
	if token == "" {
		return nil, "", ErrUnauthorized
	}
	id := util.HashToken(token)
	sess, err := a.store.GetSession(id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, "", ErrUnauthorized
		}
		return nil, "", err
	}
	if time.Now().After(sess.ExpiresAt) {
		_ = a.store.DeleteSession(id)
		return nil, "", ErrUnauthorized
	}
	user, err := a.store.GetUserByID(sess.UserID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, "", ErrUnauthorized
		}
		return nil, "", err
	}
	_ = a.store.TouchSession(id)
	return user, id, nil
}

// Logout 删除指定会话。
func (a *AuthService) Logout(sessionID string) error {
	return a.store.DeleteSession(sessionID)
}

// ChangePassword 校验旧密码后更新为新密码，并吊销该用户的其他会话。
// keepSessionID 为当前会话哈希，更新后保留该会话。
func (a *AuthService) ChangePassword(userID int64, keepSessionID, oldPassword, newPassword string) error {
	user, err := a.store.GetUserByID(userID)
	if err != nil {
		return err
	}
	if len(oldPassword) > maxPasswordLen {
		return ErrInvalidCredentials
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(oldPassword)); err != nil {
		return ErrInvalidCredentials
	}
	if err := validatePasswordStrength(newPassword); err != nil {
		return err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcryptCost)
	if err != nil {
		return err
	}
	if err := a.store.UpdatePassword(userID, string(hash)); err != nil {
		return err
	}
	return a.store.DeleteUserSessionsExcept(userID, keepSessionID)
}

// ChangeUsername 修改当前用户的用户名。会话本身已证明身份，故无需再次校验密码。
// 新用户名需通过格式校验且未被其他用户占用。返回更新后的用户对象。
func (a *AuthService) ChangeUsername(userID int64, newUsername string) (*model.User, error) {
	if err := validateUsername(newUsername); err != nil {
		return nil, err
	}
	if err := a.store.UpdateUsername(userID, newUsername); err != nil {
		if errors.Is(err, store.ErrUsernameTaken) {
			return nil, ErrUsernameTaken
		}
		return nil, err
	}
	return a.store.GetUserByID(userID)
}

// CleanupExpiredSessions 清理过期会话，供后台定时任务调用。
func (a *AuthService) CleanupExpiredSessions() (int64, error) {
	return a.store.DeleteExpiredSessions(time.Now())
}

// ResetAdmin 重置管理员（id=1）的用户名和密码，并吊销所有已签发会话。
// username 为新用户名；password 为空时随机生成并返回明文，否则返回空字符串。
func (a *AuthService) ResetAdmin(username, password string) (string, error) {
	user, err := a.store.GetUserByID(1)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return "", ErrUserNotFound
		}
		return "", err
	}

	if err := validateUsername(username); err != nil {
		return "", err
	}

	generatedPass := ""
	if password == "" {
		password, err = util.RandomPassword()
		if err != nil {
			return "", err
		}
		generatedPass = password
	}

	if err := validatePasswordStrength(password); err != nil {
		return "", err
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcryptCost)
	if err != nil {
		return "", err
	}

	if err := a.store.UpdateUsername(user.ID, username); err != nil {
		if errors.Is(err, store.ErrUsernameTaken) {
			return "", ErrUsernameTaken
		}
		return "", err
	}
	if err := a.store.UpdatePassword(user.ID, string(hash)); err != nil {
		return "", err
	}
	// 吊销该用户全部会话（keepID 传空）。
	if err := a.store.DeleteUserSessionsExcept(user.ID, ""); err != nil {
		return "", err
	}

	return generatedPass, nil
}

// VerifyTwoFactor 校验临时令牌和 TOTP 验证码，通过后创建正式会话。
func (a *AuthService) VerifyTwoFactor(tempToken, code string) (*LoginResult, error) {
	userID, ok := a.challenge.consume(tempToken)
	if !ok {
		return nil, ErrInvalidTOTP
	}
	user, err := a.store.GetUserByID(userID)
	if err != nil {
		return nil, ErrInvalidTOTP
	}
	if !user.TwoFactorEnabled || user.TOTPSecret == "" {
		return nil, ErrTOTPNotEnabled
	}
	if !validateTOTP(code, user.TOTPSecret) {
		return nil, ErrInvalidTOTP
	}
	token, err := util.GenerateToken()
	if err != nil {
		return nil, err
	}
	expiresAt := time.Now().Add(a.sessionTTL)
	if err := a.store.CreateSession(util.HashToken(token), user.ID, expiresAt); err != nil {
		return nil, err
	}
	a.logger.Info("2fa login success", slog.Int64("user_id", user.ID))
	return &LoginResult{Token: token, ExpiresAt: expiresAt, User: user}, nil
}

// SetupTwoFactor 生成新的 TOTP 密钥并存入数据库（此时 two_factor_enabled 仍为 0），
// 返回 secret 字符串和 QR 码 base64 data URI 供前端展示。
func (a *AuthService) SetupTwoFactor(userID int64) (secret, qrCode string, err error) {
	user, err := a.store.GetUserByID(userID)
	if err != nil {
		return "", "", err
	}
	if user.TwoFactorEnabled {
		return "", "", ErrTOTPAlreadyEnabled
	}
	secret, key, err := generateTOTPSecret(user.Username)
	if err != nil {
		return "", "", err
	}
	qrCode, err = generateQRCode(key.URL())
	if err != nil {
		return "", "", err
	}
	if err := a.store.UpdateTOTP(userID, secret, false); err != nil {
		return "", "", err
	}
	return secret, qrCode, nil
}

// EnableTwoFactor 验证用户输入的验证码，通过后正式启用 2FA。
func (a *AuthService) EnableTwoFactor(userID int64, code string) error {
	user, err := a.store.GetUserByID(userID)
	if err != nil {
		return err
	}
	if user.TwoFactorEnabled {
		return ErrTOTPAlreadyEnabled
	}
	if user.TOTPSecret == "" {
		return ErrTOTPNotSetup
	}
	if !validateTOTP(code, user.TOTPSecret) {
		return ErrInvalidTOTP
	}
	if err := a.store.UpdateTOTP(userID, user.TOTPSecret, true); err != nil {
		return err
	}
	a.logger.Info("2fa enabled", slog.Int64("user_id", userID))
	return nil
}

// DisableTwoFactor 验证验证码后关闭 2FA，清空 secret。
func (a *AuthService) DisableTwoFactor(userID int64, code string) error {
	user, err := a.store.GetUserByID(userID)
	if err != nil {
		return err
	}
	if !user.TwoFactorEnabled {
		return ErrTOTPNotEnabled
	}
	if !validateTOTP(code, user.TOTPSecret) {
		return ErrInvalidTOTP
	}
	if err := a.store.UpdateTOTP(userID, "", false); err != nil {
		return err
	}
	a.logger.Info("2fa disabled", slog.Int64("user_id", userID))
	return nil
}

// GetTwoFactorStatus 返回用户的 2FA 启用状态。
func (a *AuthService) GetTwoFactorStatus(userID int64) (bool, error) {
	user, err := a.store.GetUserByID(userID)
	if err != nil {
		return false, err
	}
	return user.TwoFactorEnabled, nil
}

// ResetTOTP 清除用户的 TOTP 配置并恢复为纯密码登录，同时吊销所有会话。
// 供 CLI --reset-totp 调用。
func (a *AuthService) ResetTOTP(userID int64) error {
	if _, err := a.store.GetUserByID(userID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return ErrUserNotFound
		}
		return err
	}
	if err := a.store.UpdateTOTP(userID, "", false); err != nil {
		return err
	}
	if err := a.store.DeleteUserSessionsExcept(userID, ""); err != nil {
		return err
	}
	a.logger.Info("totp config reset", slog.Int64("user_id", userID))
	return nil
}

const (
	usernameMinLen = 3
	usernameMaxLen = 32
)

// validateUsername 校验用户名：长度 3-32，仅允许字母、数字、下划线、连字符，且首字符为字母或数字。
func validateUsername(name string) error {
	if len(name) < usernameMinLen || len(name) > usernameMaxLen {
		return ErrInvalidUsername
	}
	for i, r := range name {
		switch {
		case unicode.IsLetter(r) && r < unicode.MaxASCII:
		case unicode.IsDigit(r):
		case (r == '_' || r == '-') && i > 0:
		default:
			return ErrInvalidUsername
		}
	}
	return nil
}

// validatePasswordStrength 校验新密码强度：≥8 位且同时含字母与数字，且不超过 bcrypt 上限。
func validatePasswordStrength(pw string) error {
	if len(pw) > maxPasswordLen {
		return ErrPasswordTooLong
	}
	if len(pw) < 8 {
		return ErrWeakPassword
	}
	var hasLetter, hasDigit bool
	for _, r := range pw {
		switch {
		case unicode.IsLetter(r):
			hasLetter = true
		case unicode.IsDigit(r):
			hasDigit = true
		}
	}
	if !hasLetter || !hasDigit {
		return ErrWeakPassword
	}
	return nil
}

// lockoutTracker 进程内登录失败计数与锁定（重启清零）。
type lockoutTracker struct {
	mu       sync.Mutex
	attempts map[string]*attemptState
}

type attemptState struct {
	failures    int
	lockedUntil time.Time
}

func newLockoutTracker() *lockoutTracker {
	return &lockoutTracker{attempts: make(map[string]*attemptState)}
}

func (l *lockoutTracker) isLocked(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	st, ok := l.attempts[key]
	if !ok {
		return false
	}
	return time.Now().Before(st.lockedUntil)
}

func (l *lockoutTracker) recordFailure(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	st, ok := l.attempts[key]
	if !ok {
		st = &attemptState{}
		l.attempts[key] = st
	}
	st.failures++
	if st.failures >= lockoutThreshold {
		st.lockedUntil = time.Now().Add(lockoutDuration)
	}
}

func (l *lockoutTracker) reset(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.attempts, key)
}

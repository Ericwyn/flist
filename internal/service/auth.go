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
	Token     string
	ExpiresAt time.Time
	User      *model.User
}

// Login 校验凭证并签发会话令牌。clientIP 用于登录失败锁定。
func (a *AuthService) Login(username, password, clientIP string) (*LoginResult, error) {
	key := clientIP + "|" + username
	if a.lockout.isLocked(key) {
		return nil, ErrAccountLocked
	}
	if len(password) > maxPasswordLen {
		// 过长视为非法凭证，不泄露细节，同样计入失败。
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

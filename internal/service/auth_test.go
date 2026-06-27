package service

import (
	"testing"
	"time"

	"flist/internal/store"
)

func newAuthService(t *testing.T) (*AuthService, *store.Store) {
	t.Helper()
	dsn := "file:" + t.Name() + "?mode=memory&cache=shared&_pragma=foreign_keys(ON)"
	st, err := store.OpenWithDSN(dsn)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return NewAuthService(st, time.Hour, nil), st
}

func TestEnsureAdmin_CreatesOnce(t *testing.T) {
	a, _ := newAuthService(t)

	created, _, err := a.EnsureAdmin("admin", "secret12")
	if err != nil || !created {
		t.Fatalf("expected admin created, got created=%v err=%v", created, err)
	}

	// 第二次调用不应重复创建。
	created2, _, err := a.EnsureAdmin("admin", "secret12")
	if err != nil {
		t.Fatal(err)
	}
	if created2 {
		t.Error("admin should not be recreated when users exist")
	}
}

func TestEnsureAdmin_GeneratesPassword(t *testing.T) {
	a, _ := newAuthService(t)
	created, gen, err := a.EnsureAdmin("admin", "")
	if err != nil || !created {
		t.Fatalf("create failed: %v", err)
	}
	if gen == "" {
		t.Error("expected generated password when none provided")
	}
	// 生成的密码应能登录。
	if _, err := a.Login("admin", gen, "1.1.1.1"); err != nil {
		t.Errorf("login with generated password failed: %v", err)
	}
}

func TestLogin_SuccessAndValidate(t *testing.T) {
	a, _ := newAuthService(t)
	a.EnsureAdmin("admin", "secret12")

	res, err := a.Login("admin", "secret12", "1.1.1.1")
	if err != nil {
		t.Fatalf("login failed: %v", err)
	}
	if res.Token == "" {
		t.Fatal("expected non-empty token")
	}

	user, sid, err := a.Validate(res.Token)
	if err != nil {
		t.Fatalf("validate failed: %v", err)
	}
	if user.Username != "admin" || sid == "" {
		t.Errorf("unexpected validate result: user=%+v sid=%q", user, sid)
	}
}

func TestLogin_WrongPassword(t *testing.T) {
	a, _ := newAuthService(t)
	a.EnsureAdmin("admin", "secret12")

	if _, err := a.Login("admin", "wrong", "1.1.1.1"); err != ErrInvalidCredentials {
		t.Errorf("expected ErrInvalidCredentials, got %v", err)
	}
}

func TestLogin_LockoutAfterFailures(t *testing.T) {
	a, _ := newAuthService(t)
	a.EnsureAdmin("admin", "secret12")

	for i := 0; i < lockoutThreshold; i++ {
		a.Login("admin", "wrong", "9.9.9.9")
	}
	// 达到阈值后即使密码正确也应被锁定。
	if _, err := a.Login("admin", "secret12", "9.9.9.9"); err != ErrAccountLocked {
		t.Errorf("expected ErrAccountLocked, got %v", err)
	}
	// 不同 IP 不受影响。
	if _, err := a.Login("admin", "secret12", "8.8.8.8"); err != nil {
		t.Errorf("different IP should not be locked: %v", err)
	}
}

func TestValidate_InvalidToken(t *testing.T) {
	a, _ := newAuthService(t)
	if _, _, err := a.Validate(""); err != ErrUnauthorized {
		t.Errorf("empty token: expected ErrUnauthorized, got %v", err)
	}
	if _, _, err := a.Validate("bogus"); err != ErrUnauthorized {
		t.Errorf("bogus token: expected ErrUnauthorized, got %v", err)
	}
}

func TestValidate_ExpiredSession(t *testing.T) {
	a, st := newAuthService(t)
	a.EnsureAdmin("admin", "secret12")
	// 直接构造一个已过期的会话。
	short := NewAuthService(st, -time.Hour, nil)
	res, err := short.Login("admin", "secret12", "1.1.1.1")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := a.Validate(res.Token); err != ErrUnauthorized {
		t.Errorf("expected ErrUnauthorized for expired session, got %v", err)
	}
}

func TestLogout(t *testing.T) {
	a, _ := newAuthService(t)
	a.EnsureAdmin("admin", "secret12")
	res, _ := a.Login("admin", "secret12", "1.1.1.1")

	_, sid, _ := a.Validate(res.Token)
	if err := a.Logout(sid); err != nil {
		t.Fatal(err)
	}
	if _, _, err := a.Validate(res.Token); err != ErrUnauthorized {
		t.Errorf("session should be invalid after logout, got %v", err)
	}
}

func TestChangePassword(t *testing.T) {
	a, _ := newAuthService(t)
	a.EnsureAdmin("admin", "secret12")
	res, _ := a.Login("admin", "secret12", "1.1.1.1")
	_, keepSID, _ := a.Validate(res.Token)

	// 另开一个会话，改密后应被吊销。
	other, _ := a.Login("admin", "secret12", "2.2.2.2")
	_, _, _ = a.Validate(other.Token)

	if err := a.ChangePassword(res.User.ID, keepSID, "secret12", "newpass99"); err != nil {
		t.Fatalf("change password failed: %v", err)
	}

	// 旧密码失效，新密码可用。
	if _, err := a.Login("admin", "secret12", "3.3.3.3"); err != ErrInvalidCredentials {
		t.Errorf("old password should fail, got %v", err)
	}
	if _, err := a.Login("admin", "newpass99", "3.3.3.3"); err != nil {
		t.Errorf("new password should work: %v", err)
	}

	// 当前会话保留，其他会话被吊销。
	if _, _, err := a.Validate(res.Token); err != nil {
		t.Errorf("current session should remain valid: %v", err)
	}
	if _, _, err := a.Validate(other.Token); err != ErrUnauthorized {
		t.Errorf("other session should be revoked, got %v", err)
	}
}

func TestChangePassword_WrongOld(t *testing.T) {
	a, _ := newAuthService(t)
	a.EnsureAdmin("admin", "secret12")
	res, _ := a.Login("admin", "secret12", "1.1.1.1")
	_, sid, _ := a.Validate(res.Token)

	if err := a.ChangePassword(res.User.ID, sid, "wrongold", "newpass99"); err != ErrInvalidCredentials {
		t.Errorf("expected ErrInvalidCredentials, got %v", err)
	}
}

func TestChangePassword_WeakNew(t *testing.T) {
	a, _ := newAuthService(t)
	a.EnsureAdmin("admin", "secret12")
	res, _ := a.Login("admin", "secret12", "1.1.1.1")
	_, sid, _ := a.Validate(res.Token)

	cases := []string{"short1", "allletters", "12345678"}
	for _, pw := range cases {
		if err := a.ChangePassword(res.User.ID, sid, "secret12", pw); err != ErrWeakPassword {
			t.Errorf("password %q: expected ErrWeakPassword, got %v", pw, err)
		}
	}
}

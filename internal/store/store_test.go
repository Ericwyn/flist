package store

import (
	"testing"
	"time"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	// 每个测试用独立的命名内存库，避免相互干扰。
	dsn := "file:" + t.Name() + "?mode=memory&cache=shared&_pragma=foreign_keys(ON)"
	st, err := OpenWithDSN(dsn)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func TestUserCRUD(t *testing.T) {
	st := newTestStore(t)

	n, err := st.CountUsers()
	if err != nil || n != 0 {
		t.Fatalf("expected 0 users, got %d err=%v", n, err)
	}

	id, err := st.CreateUser("admin", "hash")
	if err != nil {
		t.Fatal(err)
	}
	if id == 0 {
		t.Fatal("expected non-zero id")
	}

	u, err := st.GetUserByUsername("admin")
	if err != nil {
		t.Fatal(err)
	}
	if u.Username != "admin" || u.Password != "hash" {
		t.Errorf("unexpected user: %+v", u)
	}

	u2, err := st.GetUserByID(id)
	if err != nil || u2.ID != id {
		t.Errorf("GetUserByID mismatch: %+v err=%v", u2, err)
	}
}

func TestCreateUser_UniqueUsername(t *testing.T) {
	st := newTestStore(t)
	if _, err := st.CreateUser("dup", "h1"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateUser("dup", "h2"); err == nil {
		t.Fatal("expected unique constraint violation")
	}
}

func TestGetUser_NotFound(t *testing.T) {
	st := newTestStore(t)
	if _, err := st.GetUserByUsername("ghost"); err != ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestSessionLifecycle(t *testing.T) {
	st := newTestStore(t)
	uid, _ := st.CreateUser("u", "h")

	exp := time.Now().Add(time.Hour)
	if err := st.CreateSession("sid1", uid, exp); err != nil {
		t.Fatal(err)
	}
	sess, err := st.GetSession("sid1")
	if err != nil {
		t.Fatal(err)
	}
	if sess.UserID != uid {
		t.Errorf("session user mismatch: %d", sess.UserID)
	}

	if err := st.TouchSession("sid1"); err != nil {
		t.Fatal(err)
	}

	if err := st.DeleteSession("sid1"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.GetSession("sid1"); err != ErrNotFound {
		t.Errorf("expected ErrNotFound after delete, got %v", err)
	}
}

func TestDeleteExpiredSessions(t *testing.T) {
	st := newTestStore(t)
	uid, _ := st.CreateUser("u", "h")

	st.CreateSession("expired", uid, time.Now().Add(-time.Hour))
	st.CreateSession("valid", uid, time.Now().Add(time.Hour))

	n, err := st.DeleteExpiredSessions(time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("expected 1 expired deleted, got %d", n)
	}
	if _, err := st.GetSession("valid"); err != nil {
		t.Errorf("valid session should remain: %v", err)
	}
	if _, err := st.GetSession("expired"); err != ErrNotFound {
		t.Errorf("expired session should be gone: %v", err)
	}
}

func TestDeleteUserSessionsExcept(t *testing.T) {
	st := newTestStore(t)
	uid, _ := st.CreateUser("u", "h")
	exp := time.Now().Add(time.Hour)
	st.CreateSession("keep", uid, exp)
	st.CreateSession("drop1", uid, exp)
	st.CreateSession("drop2", uid, exp)

	if err := st.DeleteUserSessionsExcept(uid, "keep"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.GetSession("keep"); err != nil {
		t.Errorf("keep session should remain: %v", err)
	}
	if _, err := st.GetSession("drop1"); err != ErrNotFound {
		t.Errorf("drop1 should be gone: %v", err)
	}
}

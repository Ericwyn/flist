package config

import (
	"testing"
	"time"
)

func TestLoad_RequiresRoot(t *testing.T) {
	t.Setenv("FLIST_ROOT", "")
	if _, err := Load([]string{}); err == nil {
		t.Fatal("expected error when --root missing")
	}
}

func TestLoad_FlagOverridesEnv(t *testing.T) {
	t.Setenv("FLIST_ROOT", "/env/root")
	t.Setenv("FLIST_ADDR", ":9999")

	cfg, err := Load([]string{"--root", "/flag/root"})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Root != "/flag/root" {
		t.Errorf("flag should override env: got Root=%q", cfg.Root)
	}
	// addr 未通过 flag 指定，应回落到环境变量。
	if cfg.Addr != ":9999" {
		t.Errorf("addr should fall back to env: got %q", cfg.Addr)
	}
}

func TestLoad_Defaults(t *testing.T) {
	t.Setenv("FLIST_ROOT", "")
	t.Setenv("FLIST_ADDR", "")
	t.Setenv("FLIST_DATA", "")
	t.Setenv("FLIST_ADMIN_USER", "")
	t.Setenv("FLIST_SESSION_TTL", "")

	cfg, err := Load([]string{"--root", "/some/root"})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Addr != ":16550" {
		t.Errorf("default addr: got %q", cfg.Addr)
	}
	if cfg.Data != "./data" {
		t.Errorf("default data: got %q", cfg.Data)
	}
	if cfg.AdminUser != "admin" {
		t.Errorf("default admin user: got %q", cfg.AdminUser)
	}
	if cfg.SessionTTL != 24*time.Hour {
		t.Errorf("default ttl: got %v", cfg.SessionTTL)
	}
}

func TestLoad_SessionTTLFromEnv(t *testing.T) {
	t.Setenv("FLIST_ROOT", "/r")
	t.Setenv("FLIST_SESSION_TTL", "48h")
	cfg, err := Load([]string{})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.SessionTTL != 48*time.Hour {
		t.Errorf("ttl from env: got %v", cfg.SessionTTL)
	}
}

func TestLoad_InvalidSessionTTL(t *testing.T) {
	t.Setenv("FLIST_ROOT", "/r")
	t.Setenv("FLIST_SESSION_TTL", "notaduration")
	if _, err := Load([]string{}); err == nil {
		t.Fatal("expected error for invalid TTL")
	}
}

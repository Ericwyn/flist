//go:build linux

package util

import "testing"

func TestUsage(t *testing.T) {
	total, free, err := Usage(t.TempDir())
	if err != nil {
		t.Fatalf("Usage: %v", err)
	}
	if total == 0 {
		t.Error("expected non-zero total")
	}
	if free == 0 {
		t.Error("expected non-zero free")
	}
	if free > total {
		t.Errorf("free (%d) should not exceed total (%d)", free, total)
	}
}

func TestUsage_Nonexistent(t *testing.T) {
	if _, _, err := Usage("/nonexistent/path/xyz"); err == nil {
		t.Error("expected error for nonexistent path")
	}
}

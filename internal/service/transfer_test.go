package service

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"flist/internal/storage"
	"flist/internal/storage/local"
	"flist/internal/util"
)

func TestNumberedName(t *testing.T) {
	cases := []struct {
		name  string
		isDir bool
		want  string
	}{
		{name: "F.I.R", isDir: true, want: "F.I.R (2)"},
		{name: ".config", isDir: true, want: ".config (2)"},
		{name: "F.I.R", want: "F.I (2).R"},
		{name: "archive.tar.gz", want: "archive.tar (2).gz"},
		{name: ".env", want: ".env (2)"},
		{name: "README", want: "README (2)"},
	}
	for _, tc := range cases {
		if got := numberedName(tc.name, tc.isDir, 2); got != tc.want {
			t.Errorf("numberedName(%q, %v, 2) = %q, want %q", tc.name, tc.isDir, got, tc.want)
		}
	}
}

type failTransferBackend struct {
	storage.Backend
	failSrc string
}

func (b *failTransferBackend) Copy(ctx context.Context, src, dst string) error {
	if src == b.failSrc {
		return storage.ErrForbidden
	}
	return b.Backend.Copy(ctx, src, dst)
}

func (b *failTransferBackend) Move(ctx context.Context, src, dst string) error {
	if src == b.failSrc {
		return storage.ErrForbidden
	}
	return b.Backend.Move(ctx, src, dst)
}

func TestMove_MergePartialFailureKeepsRemainingSource(t *testing.T) {
	root := t.TempDir()
	realRoot, err := util.ResolveRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	base := local.New(realRoot, t.TempDir())
	backend := &failTransferBackend{Backend: base, failSrc: "/src/merge/b.txt"}
	svc := NewFileService(backend, util.NewPathLocker(), 5<<20)
	writeFile(t, root, "src/merge/a.txt", "a")
	writeFile(t, root, "src/merge/b.txt", "b")
	os.MkdirAll(filepath.Join(root, "dst", "merge"), 0o755)

	results := svc.MoveWithPolicy(context.Background(), []string{"/src/merge"}, "/dst", ConflictMergeDirs)
	if len(results) != 1 || results[0].OK || results[0].Outcome != "partial" {
		t.Fatalf("expected partial merge failure: %+v", results)
	}
	if results[0].Error != "permission_denied" {
		t.Fatalf("unexpected partial error: %q", results[0].Error)
	}
	if _, err := os.Stat(filepath.Join(root, "dst", "merge", "a.txt")); err != nil {
		t.Fatalf("successful child should remain in destination: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "src", "merge", "b.txt")); err != nil {
		t.Fatalf("failed child should remain in source: %v", err)
	}
}

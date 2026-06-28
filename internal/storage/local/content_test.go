package local

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"flist/internal/model"
	"flist/internal/storage"
	"flist/internal/util"
)

// newEditTestBackend 构造一个本地驱动与其 root 真实路径。
func newEditTestBackend(t *testing.T) (*Local, string) {
	t.Helper()
	real, err := util.ResolveRoot(t.TempDir())
	if err != nil {
		t.Fatalf("ResolveRoot: %v", err)
	}
	return New(real, t.TempDir()), real
}

func TestReadText_UTF8(t *testing.T) {
	b, root := newEditTestBackend(t)
	os.WriteFile(filepath.Join(root, "note.md"), []byte("# Hello\nworld\n"), 0o644)

	res, err := b.ReadText(context.Background(), "/note.md", 0)
	if err != nil {
		t.Fatalf("ReadText: %v", err)
	}
	if res.Content != "# Hello\nworld\n" {
		t.Errorf("unexpected content: %q", res.Content)
	}
	if res.Encoding != model.EncodingUTF8 {
		t.Errorf("expected utf-8, got %q", res.Encoding)
	}
	if res.LineEnding != model.LineEndingLF {
		t.Errorf("expected lf, got %q", res.LineEnding)
	}
	if !strings.HasPrefix(res.Revision.Token, "sha256:") || res.Revision.Weak {
		t.Errorf("unexpected revision: %+v", res.Revision)
	}
	if !res.Editable || res.Readonly {
		t.Errorf("regular file should be editable: %+v", res)
	}
}

func TestReadText_BOMAndCRLF(t *testing.T) {
	b, root := newEditTestBackend(t)
	data := append([]byte{0xEF, 0xBB, 0xBF}, []byte("a\r\nb\r\n")...)
	os.WriteFile(filepath.Join(root, "win.txt"), data, 0o644)

	res, err := b.ReadText(context.Background(), "/win.txt", 0)
	if err != nil {
		t.Fatalf("ReadText: %v", err)
	}
	if res.Encoding != model.EncodingUTF8BOM {
		t.Errorf("expected utf-8-bom, got %q", res.Encoding)
	}
	if res.Content != "a\r\nb\r\n" {
		t.Errorf("BOM should be stripped from content: %q", res.Content)
	}
	if res.LineEnding != model.LineEndingCRLF {
		t.Errorf("expected crlf, got %q", res.LineEnding)
	}
}

func TestReadText_Binary(t *testing.T) {
	b, root := newEditTestBackend(t)
	os.WriteFile(filepath.Join(root, "data.bin"), []byte{0x00, 0x01, 0x02, 0xff}, 0o644)

	if _, err := b.ReadText(context.Background(), "/data.bin", 0); err != storage.ErrUnsupportedMedia {
		t.Errorf("expected ErrUnsupportedMedia, got %v", err)
	}
}

func TestReadText_TooLarge(t *testing.T) {
	b, root := newEditTestBackend(t)
	os.WriteFile(filepath.Join(root, "big.txt"), []byte(strings.Repeat("x", 100)), 0o644)

	if _, err := b.ReadText(context.Background(), "/big.txt", 50); err != storage.ErrFileTooLarge {
		t.Errorf("expected ErrFileTooLarge, got %v", err)
	}
}

func TestReadText_Dir(t *testing.T) {
	b, root := newEditTestBackend(t)
	os.MkdirAll(filepath.Join(root, "adir"), 0o755)

	if _, err := b.ReadText(context.Background(), "/adir", 0); err != storage.ErrNotFile {
		t.Errorf("expected ErrNotFile, got %v", err)
	}
}

func TestReadText_NotFound(t *testing.T) {
	b, _ := newEditTestBackend(t)
	if _, err := b.ReadText(context.Background(), "/ghost.txt", 0); err != storage.ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestWriteText_RevisionMatch(t *testing.T) {
	b, root := newEditTestBackend(t)
	os.WriteFile(filepath.Join(root, "todo.md"), []byte("old"), 0o644)

	cur, err := b.ReadText(context.Background(), "/todo.md", 0)
	if err != nil {
		t.Fatal(err)
	}
	res, err := b.WriteText(context.Background(), "/todo.md", []byte("new content"), cur.Revision, false)
	if err != nil {
		t.Fatalf("WriteText: %v", err)
	}
	if res.Size != int64(len("new content")) {
		t.Errorf("unexpected size: %d", res.Size)
	}
	if res.Revision.Token == cur.Revision.Token {
		t.Error("revision should change after write")
	}
	if got, _ := os.ReadFile(filepath.Join(root, "todo.md")); string(got) != "new content" {
		t.Errorf("file content mismatch: %q", got)
	}
}

func TestWriteText_Conflict(t *testing.T) {
	b, root := newEditTestBackend(t)
	os.WriteFile(filepath.Join(root, "todo.md"), []byte("current"), 0o644)

	stale := model.FileRevision{Token: "sha256:deadbeef"}
	if _, err := b.WriteText(context.Background(), "/todo.md", []byte("x"), stale, false); err != storage.ErrFileModified {
		t.Errorf("expected ErrFileModified, got %v", err)
	}
	// 文件内容不应被改动。
	if got, _ := os.ReadFile(filepath.Join(root, "todo.md")); string(got) != "current" {
		t.Errorf("file should be unchanged on conflict: %q", got)
	}
}

func TestWriteText_Force(t *testing.T) {
	b, root := newEditTestBackend(t)
	os.WriteFile(filepath.Join(root, "todo.md"), []byte("current"), 0o644)

	stale := model.FileRevision{Token: "sha256:deadbeef"}
	if _, err := b.WriteText(context.Background(), "/todo.md", []byte("forced"), stale, true); err != nil {
		t.Fatalf("force write should succeed: %v", err)
	}
	if got, _ := os.ReadFile(filepath.Join(root, "todo.md")); string(got) != "forced" {
		t.Errorf("force write content mismatch: %q", got)
	}
}

func TestWriteText_MissingRevision(t *testing.T) {
	b, root := newEditTestBackend(t)
	os.WriteFile(filepath.Join(root, "todo.md"), []byte("x"), 0o644)

	empty := model.FileRevision{}
	if _, err := b.WriteText(context.Background(), "/todo.md", []byte("y"), empty, false); err != storage.ErrInvalidRev {
		t.Errorf("expected ErrInvalidRev without force, got %v", err)
	}
}

func TestWriteText_PreservesContentExactly(t *testing.T) {
	b, root := newEditTestBackend(t)
	os.WriteFile(filepath.Join(root, "f.txt"), []byte("a\r\nb"), 0o644)

	cur, _ := b.ReadText(context.Background(), "/f.txt", 0)
	// 保存含 CRLF 的内容，应原样写入，不做行尾转换。
	payload := "x\r\ny\r\nz"
	if _, err := b.WriteText(context.Background(), "/f.txt", []byte(payload), cur.Revision, false); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(filepath.Join(root, "f.txt"))
	if string(got) != payload {
		t.Errorf("content should be written verbatim: %q", got)
	}
}

func TestContentEditor_RoundTripRevision(t *testing.T) {
	b, root := newEditTestBackend(t)
	os.WriteFile(filepath.Join(root, "f.txt"), []byte("v1"), 0o644)

	// 读取 → 用返回的 revision 保存 → 再用新 revision 保存，应连续成功。
	r1, _ := b.ReadText(context.Background(), "/f.txt", 0)
	s1, err := b.WriteText(context.Background(), "/f.txt", []byte("v2"), r1.Revision, false)
	if err != nil {
		t.Fatalf("first save: %v", err)
	}
	if _, err := b.WriteText(context.Background(), "/f.txt", []byte("v3"), s1.Revision, false); err != nil {
		t.Fatalf("second save with chained revision: %v", err)
	}
	if got, _ := os.ReadFile(filepath.Join(root, "f.txt")); string(got) != "v3" {
		t.Errorf("final content mismatch: %q", got)
	}
}

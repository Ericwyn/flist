package service

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"flist/internal/model"
	"flist/internal/storage"
	"flist/internal/storage/local"
	"flist/internal/util"

	"github.com/nwaples/rardecode/v2"
)

type testArchiveEntry struct {
	name string
	body string
	dir  bool
	mode os.FileMode
}

func writeZIPFixture(t *testing.T, filename string, entries []testArchiveEntry) {
	t.Helper()
	f, err := os.Create(filename)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	for _, entry := range entries {
		name := entry.name
		if entry.dir && !strings.HasSuffix(name, "/") {
			name += "/"
		}
		h := &zip.FileHeader{Name: name, Method: zip.Deflate}
		if entry.dir {
			h.SetMode(os.ModeDir | 0o755)
		} else if entry.mode != 0 {
			h.SetMode(entry.mode)
		}
		w, err := zw.CreateHeader(h)
		if err != nil {
			t.Fatal(err)
		}
		if !entry.dir {
			if _, err := io.WriteString(w, entry.body); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

func writeTarGZFixture(t *testing.T, filename string, entries []testArchiveEntry) {
	t.Helper()
	f, err := os.Create(filename)
	if err != nil {
		t.Fatal(err)
	}
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	for _, entry := range entries {
		typeflag := byte(tar.TypeReg)
		size := int64(len(entry.body))
		name := entry.name
		if entry.dir {
			typeflag = tar.TypeDir
			size = 0
			if !strings.HasSuffix(name, "/") {
				name += "/"
			}
		}
		if err := tw.WriteHeader(&tar.Header{Name: name, Typeflag: typeflag, Mode: 0o644, Size: size}); err != nil {
			t.Fatal(err)
		}
		if !entry.dir {
			if _, err := io.WriteString(tw, entry.body); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

// writeRARFixture 构造一个 RAR 4.x、store method 的最小归档。
// 只依赖公开格式字段，避免测试依赖系统 rar/unrar 命令。
func writeRARFixture(t *testing.T, filename, entryName, body string) {
	t.Helper()
	var archive bytes.Buffer
	archive.WriteString("Rar!\x1a\x07\x00")
	archive.Write(rar15Header(0x73, 0, nil)) // archive header

	name := []byte(entryName)
	data := []byte(body)
	tail := make([]byte, 4+21+len(name))
	binary.LittleEndian.PutUint32(tail[0:4], uint32(len(data))) // ADD_SIZE / packed size
	binary.LittleEndian.PutUint32(tail[4:8], uint32(len(data))) // unpacked size
	tail[8] = 2                                                 // HostOSWindows - 1
	binary.LittleEndian.PutUint32(tail[9:13], crc32.ChecksumIEEE(data))
	// DOS time tail[13:17] 留 0。
	tail[17] = 20   // unpack version
	tail[18] = 0x30 // store
	binary.LittleEndian.PutUint16(tail[19:21], uint16(len(name)))
	binary.LittleEndian.PutUint32(tail[21:25], 0x20) // normal file attribute
	copy(tail[25:], name)
	archive.Write(rar15Header(0x74, 0x8000, tail)) // file header + data flag
	archive.Write(data)
	archive.Write(rar15Header(0x7b, 0, nil)) // end header

	if err := os.WriteFile(filename, archive.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

func rar15Header(blockType byte, flags uint16, tail []byte) []byte {
	header := make([]byte, 7+len(tail))
	header[2] = blockType
	binary.LittleEndian.PutUint16(header[3:5], flags)
	binary.LittleEndian.PutUint16(header[5:7], uint16(len(header)))
	copy(header[7:], tail)
	binary.LittleEndian.PutUint16(header[0:2], uint16(crc32.ChecksumIEEE(header[2:])))
	return header
}

func runExtractTask(t *testing.T, svc *FileOpService, src string) model.FileOpSnapshot {
	t.Helper()
	res, err := svc.Start(context.Background(), model.FileOpExtract, "u", []string{src}, "", false)
	if err != nil {
		t.Fatalf("Start extract: %v", err)
	}
	if res.Op != model.FileOpExtract || res.TotalItems != 1 {
		t.Fatalf("unexpected start result: %+v", res)
	}
	ch, _, unsub := svc.Subscribe(res.TaskID, "u")
	if ch == nil {
		t.Fatal("Subscribe returned nil")
	}
	defer unsub()
	events := drainEvents(t, ch, 10*time.Second)
	if len(events) == 0 || events[len(events)-1].Type != "finished" {
		t.Fatalf("missing finished event: %+v", events)
	}
	return events[len(events)-1].Snapshot
}

func assertNoExtractTemp(t *testing.T, dir string) {
	t.Helper()
	items, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range items {
		if strings.HasPrefix(item.Name(), ".flist-extract-") {
			t.Errorf("temporary extract directory leaked: %s", item.Name())
		}
	}
}

func TestArchiveNameDetectionAndBase(t *testing.T) {
	cases := []struct {
		name   string
		format archiveFormat
		base   string
		ok     bool
	}{
		{"photos.ZIP", archiveZIP, "photos", true},
		{"backup.Rar", archiveRAR, "backup", true},
		{"logs.TAR.GZ", archiveTarGZ, "logs", true},
		{"...zip", archiveZIP, "解压内容", true},
		{"archive.tgz", "", "", false},
		{"plain.txt", "", "", false},
	}
	for _, tc := range cases {
		format, ok := detectArchiveFormat(tc.name)
		if ok != tc.ok || format != tc.format {
			t.Errorf("detectArchiveFormat(%q)=(%q,%v), want (%q,%v)", tc.name, format, ok, tc.format, tc.ok)
		}
		if got := archiveBaseName(tc.name); got != tc.base {
			t.Errorf("archiveBaseName(%q)=%q want %q", tc.name, got, tc.base)
		}
	}
}

func TestCleanArchiveEntry(t *testing.T) {
	valid := map[string]string{
		"folder/file.txt":   "folder/file.txt",
		"./folder/file.txt": "folder/file.txt",
		"目录/文件.txt":         "目录/文件.txt",
	}
	for in, want := range valid {
		got, err := cleanArchiveEntry(in)
		if err != nil || got != want {
			t.Errorf("cleanArchiveEntry(%q)=(%q,%v), want %q", in, got, err, want)
		}
	}
	for _, in := range []string{"../escape", "a/../../escape", "/absolute", `..\escape`, `C:\escape`, "bad\x00name"} {
		if _, err := cleanArchiveEntry(in); !errors.Is(err, ErrArchiveUnsafePath) {
			t.Errorf("cleanArchiveEntry(%q) err=%v, want ErrArchiveUnsafePath", in, err)
		}
	}
}

func TestFileOpExtractZIPAndConflictRename(t *testing.T) {
	svc, root := setupOpService(t)
	zipPath := filepath.Join(root, "photos.zip")
	writeZIPFixture(t, zipPath, []testArchiveEntry{
		{name: "album/", dir: true},
		{name: "album/a.txt", body: "alpha"},
		{name: "album/link", body: "../../outside", mode: os.ModeSymlink | 0o777},
		{name: "empty/", dir: true},
	})
	if err := os.MkdirAll(filepath.Join(root, "photos"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, root, "photos/keep.txt", "keep")

	snap := runExtractTask(t, svc, "/photos.zip")
	if snap.Status != model.FileOpDone || snap.DoneItems != 1 || len(snap.Results) != 1 || !snap.Results[0].OK {
		t.Fatalf("unexpected final snapshot: %+v", snap)
	}
	got, err := os.ReadFile(filepath.Join(root, "photos (2)", "album", "a.txt"))
	if err != nil || string(got) != "alpha" {
		t.Fatalf("extracted file=(%q,%v), want alpha", got, err)
	}
	if _, err := os.Stat(filepath.Join(root, "photos (2)", "empty")); err != nil {
		t.Errorf("empty directory missing: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(root, "photos (2)", "album", "link")); !os.IsNotExist(err) {
		t.Errorf("ZIP symlink entry should be skipped, stat err=%v", err)
	}
	if got, err := os.ReadFile(filepath.Join(root, "photos", "keep.txt")); err != nil || string(got) != "keep" {
		t.Errorf("existing target modified: %q, %v", got, err)
	}
	assertNoExtractTemp(t, root)
}

func TestFileOpExtractTarGZAndTraversalRollback(t *testing.T) {
	svc, root := setupOpService(t)
	goodPath := filepath.Join(root, "logs.tar.gz")
	writeTarGZFixture(t, goodPath, []testArchiveEntry{
		{name: "nested/a.log", body: "line"},
		{name: "empty", dir: true},
	})
	snap := runExtractTask(t, svc, "/logs.tar.gz")
	if snap.Status != model.FileOpDone {
		t.Fatalf("good tar.gz status=%q results=%+v", snap.Status, snap.Results)
	}
	if got, err := os.ReadFile(filepath.Join(root, "logs", "nested", "a.log")); err != nil || string(got) != "line" {
		t.Fatalf("tar.gz output=(%q,%v)", got, err)
	}

	badPath := filepath.Join(root, "unsafe.tar.gz")
	writeTarGZFixture(t, badPath, []testArchiveEntry{
		{name: "safe.txt", body: "temporarily written"},
		{name: "../escape.txt", body: "must not escape"},
	})
	snap = runExtractTask(t, svc, "/unsafe.tar.gz")
	if snap.Status != model.FileOpFailed || len(snap.Results) != 1 || snap.Results[0].Error != "archive_unsafe_path" {
		t.Fatalf("unsafe tar final snapshot: %+v", snap)
	}
	if _, err := os.Stat(filepath.Join(root, "unsafe")); !os.IsNotExist(err) {
		t.Errorf("failed extraction left final directory: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "escape.txt")); !os.IsNotExist(err) {
		t.Errorf("traversal escaped root: %v", err)
	}
	assertNoExtractTemp(t, root)
}

func TestFileOpExtractRAR(t *testing.T) {
	svc, root := setupOpService(t)
	writeRARFixture(t, filepath.Join(root, "bundle.rar"), "nested/hello.txt", "hello rar")
	snap := runExtractTask(t, svc, "/bundle.rar")
	if snap.Status != model.FileOpDone {
		t.Fatalf("RAR status=%q results=%+v error=%q", snap.Status, snap.Results, snap.Error)
	}
	got, err := os.ReadFile(filepath.Join(root, "bundle", "nested", "hello.txt"))
	if err != nil || string(got) != "hello rar" {
		t.Fatalf("RAR output=(%q,%v)", got, err)
	}
	assertNoExtractTemp(t, root)
}

func TestFileOpExtractCorruptArchiveRollsBack(t *testing.T) {
	svc, root := setupOpService(t)
	writeFile(t, root, "broken.zip", "not a zip")
	snap := runExtractTask(t, svc, "/broken.zip")
	if snap.Status != model.FileOpFailed || snap.Error != "archive_corrupt" {
		t.Fatalf("corrupt status=%q error=%q results=%+v", snap.Status, snap.Error, snap.Results)
	}
	if _, err := os.Stat(filepath.Join(root, "broken")); !os.IsNotExist(err) {
		t.Errorf("corrupt extraction left final directory: %v", err)
	}
	assertNoExtractTemp(t, root)
}

type noStreamWriterBackend struct{ storage.Backend }

func TestFileOpExtractUnsupportedWriterRollsBack(t *testing.T) {
	rootDir := t.TempDir()
	root, err := util.ResolveRoot(rootDir)
	if err != nil {
		t.Fatal(err)
	}
	localBackend := local.New(root, t.TempDir())
	files := NewFileService(&noStreamWriterBackend{Backend: localBackend}, util.NewPathLocker(), 5<<20)
	svc := NewFileOpService(files, nil)
	writeZIPFixture(t, filepath.Join(root, "readonly.zip"), []testArchiveEntry{{name: "file.txt", body: "data"}})

	snap := runExtractTask(t, svc, "/readonly.zip")
	if snap.Status != model.FileOpFailed || snap.Error != "not_supported" {
		t.Fatalf("unsupported writer snapshot: %+v", snap)
	}
	if _, err := os.Stat(filepath.Join(root, "readonly")); !os.IsNotExist(err) {
		t.Errorf("unsupported extraction left final directory: %v", err)
	}
	assertNoExtractTemp(t, root)
}

func TestBudgetWriterLimitsAndCancellation(t *testing.T) {
	t.Run("disk budget", func(t *testing.T) {
		var dst bytes.Buffer
		budget := &extractBudget{ctx: context.Background(), maxBytes: 3}
		n, err := (&budgetWriter{dst: &dst, budget: budget}).Write([]byte("four"))
		if n != 0 || !errors.Is(err, storage.ErrDiskFull) || dst.Len() != 0 {
			t.Fatalf("Write=(%d,%v), dst=%q", n, err, dst.String())
		}
	})

	t.Run("canceled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		var dst bytes.Buffer
		budget := &extractBudget{ctx: ctx}
		n, err := (&budgetWriter{dst: &dst, budget: budget}).Write([]byte("data"))
		if n != 0 || !errors.Is(err, context.Canceled) || dst.Len() != 0 {
			t.Fatalf("Write=(%d,%v), dst=%q", n, err, dst.String())
		}
	})
}

func TestNormalizeRARError(t *testing.T) {
	tests := []struct {
		err  error
		want error
	}{
		{rardecode.ErrArchiveEncrypted, ErrArchiveEncrypted},
		{rardecode.ErrArchivedFileEncrypted, ErrArchiveEncrypted},
		{rardecode.ErrBadPassword, ErrArchiveEncrypted},
		{rardecode.ErrMultiVolume, ErrArchiveMultiVolume},
		{rardecode.ErrDictionaryTooLarge, ErrUnsupportedArchive},
		{errors.New("bad rar"), ErrArchiveCorrupt},
	}
	for _, tc := range tests {
		if got := normalizeRARError(tc.err); !errors.Is(got, tc.want) {
			t.Errorf("normalizeRARError(%v)=%v, want %v", tc.err, got, tc.want)
		}
	}
}

type slowBackend struct {
	storage.Backend
	writer storage.StreamWriter
}

func (b *slowBackend) OpenWrite(ctx context.Context, p string) (io.WriteCloser, error) {
	w, err := b.writer.OpenWrite(ctx, p)
	if err != nil {
		return nil, err
	}
	return &slowWriteCloser{WriteCloser: w}, nil
}

type slowWriteCloser struct{ io.WriteCloser }

func (w *slowWriteCloser) Write(p []byte) (int, error) {
	time.Sleep(2 * time.Millisecond)
	return w.WriteCloser.Write(p)
}

func TestFileOpExtractCancelCleansTemp(t *testing.T) {
	rootDir := t.TempDir()
	root, err := util.ResolveRoot(rootDir)
	if err != nil {
		t.Fatal(err)
	}
	localBackend := local.New(root, t.TempDir())
	backend := &slowBackend{Backend: localBackend, writer: localBackend}
	files := NewFileService(backend, util.NewPathLocker(), 5<<20)
	svc := NewFileOpService(files, nil)

	zipPath := filepath.Join(root, "large.zip")
	writeZIPFixture(t, zipPath, []testArchiveEntry{{name: "large.bin", body: strings.Repeat("x", 8<<20)}})
	res, err := svc.Start(context.Background(), model.FileOpExtract, "u", []string{"/large.zip"}, "", false)
	if err != nil {
		t.Fatal(err)
	}
	ch, _, unsub := svc.Subscribe(res.TaskID, "u")
	defer unsub()
	canceled := false
	deadline := time.After(15 * time.Second)
	var final model.FileOpSnapshot
	for {
		select {
		case ev, ok := <-ch:
			if !ok {
				goto finished
			}
			if ev.Type == "item_progress" && !canceled {
				canceled = true
				svc.Cancel(res.TaskID, "u")
			}
			if ev.Type == "finished" {
				final = ev.Snapshot
				goto finished
			}
		case <-deadline:
			t.Fatal("cancel extract timed out")
		}
	}

finished:
	if !canceled {
		t.Fatal("test did not observe progress before completion")
	}
	if final.Status != model.FileOpCanceled {
		t.Fatalf("final status=%q want canceled", final.Status)
	}
	if _, err := os.Stat(filepath.Join(root, "large")); !os.IsNotExist(err) {
		t.Errorf("canceled extraction left final directory: %v", err)
	}
	assertNoExtractTemp(t, root)
}

func TestFileOpExtractThroughMux(t *testing.T) {
	tests := []struct {
		name       string
		archiveAPI string
		backend    func(storage.Backend) storage.Backend
	}{
		{
			name:       "files namespace",
			archiveAPI: "/files/mux.zip",
			backend: func(localBackend storage.Backend) storage.Backend {
				return storage.NewMux([]storage.Mount{{Name: "files", Backend: localBackend}})
			},
		},
		{
			name:       "nested drive namespace",
			archiveAPI: "/drive/usb/mux.zip",
			backend: func(localBackend storage.Backend) storage.Backend {
				deviceMux := storage.NewMux([]storage.Mount{{Name: "usb", Backend: localBackend}})
				return storage.NewMux([]storage.Mount{{Name: "drive", Backend: deviceMux}})
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rootDir := t.TempDir()
			root, err := util.ResolveRoot(rootDir)
			if err != nil {
				t.Fatal(err)
			}
			localBackend := local.New(root, t.TempDir())
			files := NewFileService(tc.backend(localBackend), util.NewPathLocker(), 5<<20)
			svc := NewFileOpService(files, nil)
			writeZIPFixture(t, filepath.Join(root, "mux.zip"), []testArchiveEntry{{name: "ok.txt", body: "mux"}})

			snap := runExtractTask(t, svc, tc.archiveAPI)
			if snap.Status != model.FileOpDone {
				t.Fatalf("mux extract failed: %+v", snap)
			}
			if got, err := os.ReadFile(filepath.Join(root, "mux", "ok.txt")); err != nil || string(got) != "mux" {
				t.Fatalf("mux output=(%q,%v)", got, err)
			}
		})
	}
}

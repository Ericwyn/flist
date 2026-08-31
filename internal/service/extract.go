package service

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	"flist/internal/model"
	"flist/internal/storage"
	"flist/internal/util"

	"github.com/nwaples/rardecode/v2"
)

type archiveFormat string

const (
	archiveZIP   archiveFormat = "zip"
	archiveRAR   archiveFormat = "rar"
	archiveTarGZ archiveFormat = "tar.gz"

	archiveMaxEntries       = 100_000
	archiveRARMaxDictionary = 128 << 20
	archiveRenameProbe      = 10_000
)

// 解压错误会作为异步任务的稳定错误名暴露给前端。
var (
	ErrUnsupportedArchive    = errors.New("archive format is not supported")
	ErrArchiveEncrypted      = errors.New("archive is encrypted")
	ErrArchiveMultiVolume    = errors.New("multi-volume archive is not supported")
	ErrArchiveUnsafePath     = errors.New("archive entry path is unsafe")
	ErrArchiveTooManyEntries = errors.New("archive has too many entries")
	ErrArchiveCorrupt        = errors.New("archive is corrupt")
)

// detectArchiveFormat 只按用户可见文件名判断格式，大小写不敏感。
func detectArchiveFormat(name string) (archiveFormat, bool) {
	lower := strings.ToLower(name)
	switch {
	case strings.HasSuffix(lower, ".tar.gz"):
		return archiveTarGZ, true
	case strings.HasSuffix(lower, ".zip"):
		return archiveZIP, true
	case strings.HasSuffix(lower, ".rar"):
		return archiveRAR, true
	default:
		return "", false
	}
}

// archiveBaseName 去掉受支持的完整扩展名，得到默认输出文件夹名。
func archiveBaseName(name string) string {
	format, ok := detectArchiveFormat(name)
	if !ok {
		return ""
	}
	base := name[:len(name)-len(string(format))-1]
	if strings.TrimSpace(base) == "" || base == "." || base == ".." || strings.ContainsAny(base, "/\\") {
		return "解压内容"
	}
	if err := util.ValidateName(base); err != nil {
		return "解压内容"
	}
	return base
}

// validateExtractSource 是发起任务前的轻量预检，不读取归档内容。
func (s *FileOpService) validateExtractSource(ctx context.Context, src string) (*model.FileInfo, error) {
	cleaned := util.CleanAPIPath(src)
	if cleaned == "/" {
		return nil, storage.ErrNotFile
	}
	info, err := s.files.backend.Stat(ctx, cleaned)
	if err != nil {
		return nil, err
	}
	if info.Type != model.TypeFile || info.IsSymlink {
		return nil, storage.ErrNotFile
	}
	if _, ok := detectArchiveFormat(info.Name); !ok {
		return nil, ErrUnsupportedArchive
	}
	return info, nil
}

// execExtract 执行单个压缩包的事务型解压：隐藏临时根写入，成功后再改名提交。
func (s *FileOpService) execExtract(t *fileOpTask) {
	ctx := t.ctx
	src := util.CleanAPIPath(t.srcs[0])
	info, err := s.validateExtractSource(ctx, src)
	if err != nil {
		s.finishExtractError(t, src, err)
		return
	}
	format, _ := detectArchiveFormat(info.Name)
	s.startItem(t, 0, info.Name, info.Size)

	parent := path.Dir(src)
	tempRoot, err := s.createExtractTempRoot(ctx, parent, t.id)
	if err != nil {
		s.finishExtractError(t, src, err)
		return
	}
	cleanup := func() {
		if tempRoot == "" {
			return
		}
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		if removeErr := s.files.backend.Remove(cleanupCtx, tempRoot); removeErr != nil && !errors.Is(removeErr, storage.ErrNotFound) {
			s.logger.Warn("extract temp cleanup failed",
				slog.String("task_id", t.id),
				slog.String("path", tempRoot),
				slog.Any("error", removeErr),
			)
		}
		tempRoot = ""
	}
	defer cleanup()

	file, openedInfo, err := s.files.backend.Open(ctx, src)
	if err != nil {
		cleanup()
		s.finishExtractError(t, src, err)
		return
	}
	defer file.Close()

	maxBytes := s.extractSpaceBudget(ctx, parent)
	budget := &extractBudget{ctx: ctx, maxBytes: maxBytes}
	var entries int
	switch format {
	case archiveZIP:
		entries, err = s.extractZIP(t, file, openedInfo.Size, tempRoot, budget)
	case archiveTarGZ:
		entries, err = s.extractTarGZ(t, file, openedInfo.Size, tempRoot, budget)
	case archiveRAR:
		entries, err = s.extractRAR(t, file, openedInfo.Size, tempRoot, budget)
	default:
		err = ErrUnsupportedArchive
	}
	if err != nil {
		cleanup()
		s.finishExtractError(t, src, err)
		return
	}

	finalPath, err := s.finalizeExtract(ctx, tempRoot, parent, archiveBaseName(info.Name))
	if err != nil {
		cleanup()
		s.finishExtractError(t, src, err)
		return
	}
	// tempRoot 已被 Move 到最终路径，defer 不再清理。
	tempRoot = ""

	t.results = append(t.results, model.OpResult{Src: src, OK: true})
	s.muSnapshot(t, func(snap *model.FileOpSnapshot) {
		snap.DoneItems = 1
		if snap.CurSize > 0 {
			snap.CurCopied = snap.CurSize
			snap.DoneBytes = snap.CurSize
		} else {
			snap.DoneBytes = budget.written
		}
	})
	s.emitItemDone(t, 0, src, true, "")
	s.finishTask(t, model.FileOpDone, t.results)
	s.logger.Info("archive extracted",
		slog.String("task_id", t.id),
		slog.String("archive_format", string(format)),
		slog.String("source", src),
		slog.String("output_path", finalPath),
		slog.Int("entries", entries),
		slog.Int64("written_bytes", budget.written),
	)
}

func (s *FileOpService) finishExtractError(t *fileOpTask, src string, err error) {
	errName := errCodeName(err)
	status := model.FileOpFailed
	if errors.Is(err, context.Canceled) || t.ctx.Err() != nil {
		errName = "canceled"
		status = model.FileOpCanceled
	}
	t.results = append(t.results, model.OpResult{Src: src, OK: false, Error: errName})
	s.emitItemDone(t, 0, src, false, errName)
	s.finishTask(t, status, t.results)
	s.logger.Warn("archive extraction failed",
		slog.String("task_id", t.id),
		slog.String("source", src),
		slog.String("status", status),
		slog.String("error_name", errName),
		slog.Any("error", err),
	)
}

func (s *FileOpService) createExtractTempRoot(ctx context.Context, parent, taskID string) (string, error) {
	suffix := taskID
	if len(suffix) > 16 {
		suffix = suffix[:16]
	}
	for i := 0; i < 8; i++ {
		if i > 0 {
			token, err := util.GenerateToken()
			if err != nil {
				return "", err
			}
			suffix = token[:16]
		}
		candidate := path.Join(parent, ".flist-extract-"+suffix)
		err := s.files.backend.Mkdir(ctx, candidate)
		if err == nil {
			return candidate, nil
		}
		if !errors.Is(err, storage.ErrExists) {
			return "", err
		}
	}
	return "", storage.ErrExists
}

func (s *FileOpService) finalizeExtract(ctx context.Context, tempRoot, parent, base string) (string, error) {
	for i := 1; i <= archiveRenameProbe; i++ {
		name := base
		if i > 1 {
			name += " (" + strconv.Itoa(i) + ")"
		}
		target := path.Join(parent, name)
		err := s.files.backend.Move(ctx, tempRoot, target)
		if err == nil {
			return target, nil
		}
		if !errors.Is(err, storage.ErrExists) {
			return "", err
		}
	}
	return "", storage.ErrExists
}

func (s *FileOpService) extractSpaceBudget(ctx context.Context, parent string) int64 {
	u, ok := s.files.backend.(storage.Usager)
	if !ok {
		return 0
	}
	_, free, err := u.Usage(ctx, parent)
	if err != nil || free > uint64(1<<63-1) {
		return 0
	}
	return int64(free)
}

// extractZIP 利用中央目录得到真实输出总量，进度按累计输出字节计算。
func (s *FileOpService) extractZIP(t *fileOpTask, file storage.File, size int64, root string, budget *extractBudget) (int, error) {
	if size < 0 {
		return 0, ErrArchiveCorrupt
	}
	zr, err := zip.NewReader(&readerAtFromSeeker{ctx: t.ctx, r: file}, size)
	if err != nil {
		return 0, wrapArchiveCorrupt(err)
	}
	if len(zr.File) > archiveMaxEntries {
		return 0, ErrArchiveTooManyEntries
	}

	type zipPlan struct {
		file *zip.File
		rel  string
		dir  bool
		skip bool
	}
	plans := make([]zipPlan, 0, len(zr.File))
	seen := make(map[string]bool)
	var total int64
	for _, zf := range zr.File {
		rel, err := cleanArchiveEntry(zf.Name)
		if err != nil {
			return 0, err
		}
		isDir := zf.FileInfo().IsDir()
		if rel == "" {
			if isDir {
				continue
			}
			return 0, fmt.Errorf("%w: empty file path", ErrArchiveUnsafePath)
		}
		skip := zf.Mode()&fs.ModeSymlink != 0
		if !skip {
			if err := registerArchivePath(seen, rel, isDir); err != nil {
				return 0, err
			}
		}
		if !isDir && !skip {
			if zf.UncompressedSize64 > uint64(1<<63-1) || total > int64(1<<63-1)-int64(zf.UncompressedSize64) {
				return 0, storage.ErrDiskFull
			}
			total += int64(zf.UncompressedSize64)
		}
		plans = append(plans, zipPlan{file: zf, rel: rel, dir: isDir, skip: skip})
	}
	if budget.maxBytes > 0 && total > budget.maxBytes {
		return 0, storage.ErrDiskFull
	}
	s.setExtractProgressTotal(t, total)
	budget.onWritten = func(written int64) { s.reportProgress(t, 0, written, total) }

	for _, plan := range plans {
		if err := t.ctx.Err(); err != nil {
			return 0, err
		}
		if plan.skip {
			continue
		}
		if plan.dir {
			if err := ensureExtractDir(t.ctx, s.files.backend, root, plan.rel); err != nil {
				return 0, err
			}
			continue
		}
		rc, err := plan.file.Open()
		if err != nil {
			if errors.Is(err, zip.ErrAlgorithm) {
				return 0, ErrUnsupportedArchive
			}
			return 0, wrapArchiveCorrupt(err)
		}
		err = writeExtractFile(t.ctx, s.files.backend, root, plan.rel, rc, budget)
		rc.Close()
		if err != nil {
			if errors.Is(err, zip.ErrAlgorithm) {
				return 0, ErrUnsupportedArchive
			}
			if errors.Is(err, zip.ErrChecksum) {
				return 0, wrapArchiveCorrupt(err)
			}
			return 0, normalizeArchiveReadError(err)
		}
	}
	return len(zr.File), nil
}

// extractTarGZ 顺序解压，进度按底层 gzip 已读取的压缩字节计算。
func (s *FileOpService) extractTarGZ(t *fileOpTask, file storage.File, size int64, root string, budget *extractBudget) (int, error) {
	s.setExtractProgressTotal(t, size)
	progress := &countingProgressReader{
		ctx:   t.ctx,
		r:     file,
		total: size,
		onProgress: func(read int64) {
			s.reportProgress(t, 0, read, size)
		},
	}
	gz, err := gzip.NewReader(progress)
	if err != nil {
		return 0, wrapArchiveCorrupt(err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	seen := make(map[string]bool)
	entries := 0
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return entries, wrapArchiveCorrupt(err)
		}
		entries++
		if entries > archiveMaxEntries {
			return entries, ErrArchiveTooManyEntries
		}
		rel, err := cleanArchiveEntry(hdr.Name)
		if err != nil {
			return entries, err
		}
		if rel == "" {
			if hdr.Typeflag == tar.TypeDir {
				continue
			}
			if hdr.Typeflag == tar.TypeReg || hdr.Typeflag == tar.TypeRegA {
				return entries, fmt.Errorf("%w: empty file path", ErrArchiveUnsafePath)
			}
			continue
		}
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := registerArchivePath(seen, rel, true); err != nil {
				return entries, err
			}
			if err := ensureExtractDir(t.ctx, s.files.backend, root, rel); err != nil {
				return entries, err
			}
		case tar.TypeReg, tar.TypeRegA:
			if hdr.Size < 0 {
				return entries, ErrArchiveCorrupt
			}
			if err := registerArchivePath(seen, rel, false); err != nil {
				return entries, err
			}
			if err := writeExtractFile(t.ctx, s.files.backend, root, rel, tr, budget); err != nil {
				return entries, normalizeArchiveReadError(err)
			}
		default:
			// 链接、设备、FIFO 等特殊条目一律不创建。
			continue
		}
	}
	// tar 在两块零 block 后即可返回 EOF；继续读完 gzip 流才能校验 gzip trailer。
	if _, err := io.Copy(io.Discard, gz); err != nil {
		return entries, normalizeArchiveReadError(err)
	}
	if size > 0 {
		s.reportProgress(t, 0, size, size)
	}
	return entries, nil
}

// extractRAR 使用纯 Go rardecode 顺序解码，兼容 solid archive 的读取要求。
func (s *FileOpService) extractRAR(t *fileOpTask, file storage.File, size int64, root string, budget *extractBudget) (int, error) {
	s.setExtractProgressTotal(t, size)
	progress := &countingProgressReader{
		ctx:   t.ctx,
		r:     file,
		total: size,
		onProgress: func(read int64) {
			s.reportProgress(t, 0, read, size)
		},
	}
	rr, err := rardecode.NewReader(progress, rardecode.MaxDictionarySize(archiveRARMaxDictionary))
	if err != nil {
		return 0, normalizeRARError(err)
	}
	seen := make(map[string]bool)
	entries := 0
	for {
		hdr, err := rr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return entries, normalizeRARError(err)
		}
		entries++
		if entries > archiveMaxEntries {
			return entries, ErrArchiveTooManyEntries
		}
		rel, err := cleanArchiveEntry(hdr.Name)
		if err != nil {
			return entries, err
		}
		if rel == "" {
			if hdr.IsDir || hdr.LinkType != rardecode.LinkTypeNone {
				continue
			}
			return entries, fmt.Errorf("%w: empty file path", ErrArchiveUnsafePath)
		}
		if hdr.LinkType != rardecode.LinkTypeNone {
			continue
		}
		if hdr.Encrypted || hdr.HeaderEncrypted {
			return entries, ErrArchiveEncrypted
		}
		if err := registerArchivePath(seen, rel, hdr.IsDir); err != nil {
			return entries, err
		}
		if hdr.IsDir {
			if err := ensureExtractDir(t.ctx, s.files.backend, root, rel); err != nil {
				return entries, err
			}
			continue
		}
		if err := writeExtractFile(t.ctx, s.files.backend, root, rel, rr, budget); err != nil {
			return entries, normalizeRARError(err)
		}
	}
	if size > 0 {
		s.reportProgress(t, 0, size, size)
	}
	return entries, nil
}

func (s *FileOpService) setExtractProgressTotal(t *fileOpTask, total int64) {
	if total < 0 {
		total = 0
	}
	s.muSnapshot(t, func(snap *model.FileOpSnapshot) {
		snap.TotalBytes = total
		snap.CurSize = total
		snap.CurCopied = 0
	})
}

// cleanArchiveEntry 将归档内名字规范成安全的相对 API 路径。
func cleanArchiveEntry(name string) (string, error) {
	if name == "" || strings.ContainsRune(name, '\x00') {
		return "", fmt.Errorf("%w: empty or NUL name", ErrArchiveUnsafePath)
	}
	normalized := strings.ReplaceAll(name, "\\", "/")
	if strings.HasPrefix(normalized, "/") || hasWindowsDrivePrefix(normalized) {
		return "", fmt.Errorf("%w: absolute path", ErrArchiveUnsafePath)
	}
	cleaned := path.Clean(normalized)
	if cleaned == "." {
		return "", nil
	}
	if cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", fmt.Errorf("%w: parent traversal", ErrArchiveUnsafePath)
	}
	for _, segment := range strings.Split(cleaned, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return "", fmt.Errorf("%w: invalid segment", ErrArchiveUnsafePath)
		}
		if err := util.ValidateName(segment); err != nil {
			return "", fmt.Errorf("%w: invalid segment", ErrArchiveUnsafePath)
		}
	}
	return cleaned, nil
}

func hasWindowsDrivePrefix(name string) bool {
	return len(name) >= 2 && ((name[0] >= 'a' && name[0] <= 'z') || (name[0] >= 'A' && name[0] <= 'Z')) && name[1] == ':'
}

// registerArchivePath 同时登记隐式父目录，拒绝重复文件和文件/目录层级冲突。
func registerArchivePath(seen map[string]bool, rel string, isDir bool) error {
	parts := strings.Split(rel, "/")
	for i := 1; i < len(parts); i++ {
		parent := strings.Join(parts[:i], "/")
		if dir, ok := seen[parent]; ok && !dir {
			return fmt.Errorf("%w: file used as directory", ErrArchiveCorrupt)
		}
		seen[parent] = true
	}
	if existingDir, ok := seen[rel]; ok {
		if existingDir && isDir {
			return nil
		}
		return fmt.Errorf("%w: duplicate path", ErrArchiveCorrupt)
	}
	seen[rel] = isDir
	return nil
}

func ensureExtractDir(ctx context.Context, backend storage.Backend, root, rel string) error {
	if rel == "" {
		return nil
	}
	parts := strings.Split(rel, "/")
	current := root
	for _, part := range parts {
		if err := ctx.Err(); err != nil {
			return err
		}
		current = path.Join(current, part)
		info, err := backend.Stat(ctx, current)
		if err == nil {
			if info.Type != model.TypeDir {
				return storage.ErrExists
			}
			continue
		}
		if !errors.Is(err, storage.ErrNotFound) {
			return err
		}
		if err := backend.Mkdir(ctx, current); err != nil {
			if errors.Is(err, storage.ErrExists) {
				info, statErr := backend.Stat(ctx, current)
				if statErr == nil && info.Type == model.TypeDir {
					continue
				}
			}
			return err
		}
	}
	return nil
}

func writeExtractFile(ctx context.Context, backend storage.Backend, root, rel string, src io.Reader, budget *extractBudget) error {
	if err := ensureExtractDir(ctx, backend, root, path.Dir(rel)); err != nil {
		return err
	}
	target := path.Join(root, rel)
	if target == root || !strings.HasPrefix(target, root+"/") {
		return ErrArchiveUnsafePath
	}
	sw, ok := backend.(storage.StreamWriter)
	if !ok {
		return storage.ErrNotSupported
	}
	wc, err := sw.OpenWrite(ctx, target)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(&budgetWriter{dst: wc, budget: budget}, src)
	closeErr := wc.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

type extractBudget struct {
	ctx       context.Context
	written   int64
	maxBytes  int64
	onWritten func(total int64)
}

type budgetWriter struct {
	dst    io.Writer
	budget *extractBudget
}

func (w *budgetWriter) Write(p []byte) (int, error) {
	if err := w.budget.ctx.Err(); err != nil {
		return 0, err
	}
	if w.budget.maxBytes > 0 && int64(len(p)) > w.budget.maxBytes-w.budget.written {
		return 0, storage.ErrDiskFull
	}
	n, err := w.dst.Write(p)
	w.budget.written += int64(n)
	if w.budget.onWritten != nil && n > 0 {
		w.budget.onWritten(w.budget.written)
	}
	return n, err
}

type countingProgressReader struct {
	ctx        context.Context
	r          io.Reader
	total      int64
	read       int64
	onProgress func(read int64)
}

func (r *countingProgressReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	n, err := r.r.Read(p)
	r.read += int64(n)
	if n > 0 && r.onProgress != nil {
		progress := r.read
		if r.total > 0 && progress > r.total {
			progress = r.total
		}
		r.onProgress(progress)
	}
	return n, err
}

// readerAtFromSeeker 为 zip.NewReader 适配 storage.File；互斥保护 Seek+ReadAt 语义。
type readerAtFromSeeker struct {
	ctx context.Context
	r   io.ReadSeeker
	mu  sync.Mutex
}

func (r *readerAtFromSeeker) ReadAt(p []byte, off int64) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, err := r.r.Seek(off, io.SeekStart); err != nil {
		return 0, err
	}
	n, err := io.ReadFull(r.r, p)
	if errors.Is(err, io.ErrUnexpectedEOF) {
		err = io.EOF
	}
	return n, err
}

func normalizeRARError(err error) error {
	if err == nil {
		return nil
	}
	switch {
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return err
	case isExtractOperationalError(err):
		return err
	case errors.Is(err, ErrArchiveUnsafePath), errors.Is(err, ErrArchiveTooManyEntries), errors.Is(err, ErrArchiveCorrupt):
		return err
	case errors.Is(err, rardecode.ErrArchiveEncrypted), errors.Is(err, rardecode.ErrArchivedFileEncrypted), errors.Is(err, rardecode.ErrBadPassword):
		return ErrArchiveEncrypted
	case errors.Is(err, rardecode.ErrMultiVolume):
		return ErrArchiveMultiVolume
	case errors.Is(err, rardecode.ErrDictionaryTooLarge), errors.Is(err, rardecode.ErrUnknownDecoder), errors.Is(err, rardecode.ErrUnsupportedDecoder):
		return ErrUnsupportedArchive
	default:
		return wrapArchiveCorrupt(err)
	}
}

func normalizeArchiveReadError(err error) error {
	if err == nil {
		return nil
	}
	switch {
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return err
	case isExtractOperationalError(err):
		return err
	case errors.Is(err, ErrArchiveUnsafePath), errors.Is(err, ErrArchiveTooManyEntries), errors.Is(err, ErrArchiveCorrupt):
		return err
	default:
		return wrapArchiveCorrupt(err)
	}
}

func isExtractOperationalError(err error) bool {
	return errors.Is(err, storage.ErrTraversal) ||
		errors.Is(err, storage.ErrInvalidName) ||
		errors.Is(err, storage.ErrNotFound) ||
		errors.Is(err, storage.ErrForbidden) ||
		errors.Is(err, storage.ErrExists) ||
		errors.Is(err, storage.ErrDiskFull) ||
		errors.Is(err, storage.ErrNotFile) ||
		errors.Is(err, storage.ErrNotDir) ||
		errors.Is(err, storage.ErrBadOp) ||
		errors.Is(err, storage.ErrNotSupported) ||
		errors.Is(err, storage.ErrReadonly)
}

func wrapArchiveCorrupt(err error) error {
	if err == nil || errors.Is(err, ErrArchiveCorrupt) {
		return ErrArchiveCorrupt
	}
	return fmt.Errorf("%w: %v", ErrArchiveCorrupt, err)
}

// Package local 实现基于本地文件系统的 storage.Backend。
//
// 它承接原 FileService 中所有与 OS 文件系统强耦合的逻辑：SafeResolve 路径安全、
// 符号链接解析、权限格式化、隐藏文件检测、递归复制 / 移动 / 删除等。对上层（service）
// 只暴露 storage.Backend / Walker 接口，入参均为相对本驱动 root 的 API 路径。
package local

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"flist/internal/model"
	"flist/internal/storage"
	"flist/internal/util"
)

// Local 是本地文件系统驱动，持有启动时缓存的 rootReal（绝对真实路径）。
// stagingDir 为分片上传的暂存根目录（通常是 <DATA_DIR>/uploads.tmp），与 root 解耦，
// 便于目录整洁与跨分区稳定；为空时表示不支持上传（StageChunk 等返回 ErrNotSupported）。
type Local struct {
	rootReal   string
	stagingDir string
}

// New 构造本地驱动。rootReal 必须是已通过 util.ResolveRoot 标准化的绝对真实路径。
// stagingDir 为分片上传暂存根目录（可为空，表示该驱动实例不承载上传）。
func New(rootReal, stagingDir string) *Local {
	return &Local{rootReal: rootReal, stagingDir: stagingDir}
}

// 确保 Local 实现了核心接口与可选接口。
var (
	_ storage.Backend         = (*Local)(nil)
	_ storage.Walker          = (*Local)(nil)
	_ storage.Usager          = (*Local)(nil)
	_ storage.Uploader        = (*Local)(nil)
	_ storage.ContentEditor   = (*Local)(nil)
	_ storage.ProgressCopier  = (*Local)(nil)
)

func (b *Local) Name() string { return "local" }

func (b *Local) Capabilities() storage.Caps {
	return storage.Caps{Write: true, Copy: true, Upload: true, DiskUsage: true, Edit: true}
}

// mapErr 将底层 OS / util 错误归一化为 storage 错误词表。
func mapErr(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, util.ErrPathTraversal):
		return storage.ErrTraversal
	case errors.Is(err, util.ErrNameInvalid):
		return storage.ErrInvalidName
	case errors.Is(err, os.ErrNotExist):
		return storage.ErrNotFound
	case errors.Is(err, os.ErrPermission):
		return storage.ErrForbidden
	case errors.Is(err, os.ErrExist):
		return storage.ErrExists
	default:
		return err
	}
}

// resolve 清理并安全解析 API 路径为本地绝对路径。
func (b *Local) resolve(apiPath string) (string, string, error) {
	cleaned := util.CleanAPIPath(apiPath)
	local, err := util.SafeResolve(b.rootReal, cleaned)
	if err != nil {
		return "", cleaned, mapErr(err)
	}
	return local, cleaned, nil
}

// Stat 返回单个文件 / 目录信息。
func (b *Local) Stat(_ context.Context, p string) (*model.FileInfo, error) {
	local, cleaned, err := b.resolve(p)
	if err != nil {
		return nil, err
	}
	fi, err := os.Lstat(local)
	if err != nil {
		return nil, mapErr(err)
	}
	parent := path.Dir(cleaned)
	info := b.buildFileInfo(parent, fi.Name(), fi)
	return &info, nil
}

// List 返回目录下的条目（已按 showHidden 过滤，未排序未分页）。
func (b *Local) List(_ context.Context, p string, showHidden bool) ([]model.FileInfo, error) {
	local, cleaned, err := b.resolve(p)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(local)
	if err != nil {
		return nil, mapErr(err)
	}
	if !info.IsDir() {
		return nil, storage.ErrNotDir
	}
	entries, err := os.ReadDir(local)
	if err != nil {
		return nil, mapErr(err)
	}
	items := make([]model.FileInfo, 0, len(entries))
	for _, de := range entries {
		name := de.Name()
		fi, lerr := de.Info() // 等价 Lstat：不跟随符号链接
		if lerr != nil {
			continue // 单条失败降级跳过，不中断整目录
		}
		if !showHidden && util.IsHidden(name, fi) {
			continue
		}
		items = append(items, b.buildFileInfo(cleaned, name, fi))
	}
	return items, nil
}

// Open 打开普通文件供下载 / 预览，返回可随机读的 *os.File。
func (b *Local) Open(_ context.Context, p string) (storage.File, *model.FileInfo, error) {
	local, cleaned, err := b.resolve(p)
	if err != nil {
		return nil, nil, err
	}
	fi, err := os.Stat(local)
	if err != nil {
		return nil, nil, mapErr(err)
	}
	if fi.IsDir() {
		return nil, nil, storage.ErrNotFile
	}
	if !fi.Mode().IsRegular() {
		return nil, nil, storage.ErrNotFile
	}
	f, err := os.Open(local)
	if err != nil {
		return nil, nil, mapErr(err)
	}
	parent := path.Dir(cleaned)
	info := b.buildFileInfo(parent, fi.Name(), fi)
	return f, &info, nil
}

// Mkdir 创建单层目录（父目录须存在，目标不存在）。
func (b *Local) Mkdir(_ context.Context, p string) error {
	cleaned := util.CleanAPIPath(p)
	if cleaned == "/" {
		return storage.ErrExists // root 已存在
	}
	if err := util.ValidateName(path.Base(cleaned)); err != nil {
		return storage.ErrInvalidName
	}
	local, err := util.SafeResolve(b.rootReal, cleaned)
	if err != nil {
		return mapErr(err)
	}
	if _, err := os.Stat(filepath.Dir(local)); err != nil {
		return mapErr(err) // 父目录不存在 → NotFound（仅建单层）
	}
	if err := os.Mkdir(local, 0o755); err != nil {
		return mapErr(err)
	}
	return nil
}

// Create 创建空文件（不存在才创建，不 truncate 已有文件）。
func (b *Local) Create(_ context.Context, p string) error {
	cleaned := util.CleanAPIPath(p)
	if cleaned == "/" {
		return storage.ErrExists
	}
	if err := util.ValidateName(path.Base(cleaned)); err != nil {
		return storage.ErrInvalidName
	}
	local, err := util.SafeResolve(b.rootReal, cleaned)
	if err != nil {
		return mapErr(err)
	}
	if _, err := os.Stat(filepath.Dir(local)); err != nil {
		return mapErr(err)
	}
	f, err := os.OpenFile(local, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return mapErr(err)
	}
	return f.Close()
}

// Move 移动 / 重命名 src 到 dst（落点须不存在，不覆盖；不得移入自身子树）。
func (b *Local) Move(ctx context.Context, src, dst string) error {
	return b.move(ctx, src, dst, nil)
}

// MoveWithProgress 与 Move 相同，但跨分区回退为复制时通过 fn 上报项内字节进度，
// 并在 ctx 取消时中止。同分区 rename 为瞬时操作，不产生进度回调。
func (b *Local) MoveWithProgress(ctx context.Context, src, dst string, fn storage.ProgressFunc) error {
	return b.move(ctx, src, dst, fn)
}

func (b *Local) move(ctx context.Context, src, dst string, fn storage.ProgressFunc) error {
	srcAPI := util.CleanAPIPath(src)
	dstAPI := util.CleanAPIPath(dst)
	if srcAPI == "/" {
		return storage.ErrBadOp // 不允许移动 root 自身
	}
	if err := util.ValidateName(path.Base(dstAPI)); err != nil {
		return storage.ErrInvalidName
	}
	srcLocal, err := util.SafeResolve(b.rootReal, srcAPI)
	if err != nil {
		return mapErr(err)
	}
	if _, err := os.Lstat(srcLocal); err != nil {
		return mapErr(err)
	}
	dstLocal, err := util.SafeResolve(b.rootReal, dstAPI)
	if err != nil {
		return mapErr(err)
	}
	if _, err := os.Lstat(dstLocal); err == nil {
		return storage.ErrExists // 落点不得已存在（不覆盖）
	}
	if isSubpath(srcLocal, dstLocal) {
		return storage.ErrBadOp // 不允许移入自身子树
	}
	var merr error
	if fn == nil {
		merr = util.MovePath(srcLocal, dstLocal)
	} else {
		merr = util.MovePathWithProgress(ctx, srcLocal, dstLocal, func(copied int64) { fn(copied) })
	}
	if err := mapErr(merr); err != nil {
		return err
	}
	return nil
}

// Remove 递归删除 p（禁止删除 root 自身）。
func (b *Local) Remove(_ context.Context, p string) error {
	cleaned := util.CleanAPIPath(p)
	if cleaned == "/" {
		return storage.ErrBadOp
	}
	local, err := util.SafeResolve(b.rootReal, cleaned)
	if err != nil {
		return mapErr(err)
	}
	if _, err := os.Lstat(local); err != nil {
		return mapErr(err)
	}
	if err := util.RemovePath(local); err != nil {
		return mapErr(err)
	}
	return nil
}

// Copy 递归复制 src 到 dst（落点须不存在；不得复制进自身子树）。
func (b *Local) Copy(ctx context.Context, src, dst string) error {
	return b.copy(ctx, src, dst, nil)
}

// CopyWithProgress 与 Copy 相同，但通过 fn 上报当前 src 项内已复制字节数，
// 并在 ctx 取消时中止。fn 为 nil 时退化为 Copy。
func (b *Local) CopyWithProgress(ctx context.Context, src, dst string, fn storage.ProgressFunc) error {
	return b.copy(ctx, src, dst, fn)
}

func (b *Local) copy(ctx context.Context, src, dst string, fn storage.ProgressFunc) error {
	srcAPI := util.CleanAPIPath(src)
	dstAPI := util.CleanAPIPath(dst)
	if srcAPI == "/" {
		return storage.ErrBadOp
	}
	if err := util.ValidateName(path.Base(dstAPI)); err != nil {
		return storage.ErrInvalidName
	}
	srcLocal, err := util.SafeResolve(b.rootReal, srcAPI)
	if err != nil {
		return mapErr(err)
	}
	if _, err := os.Lstat(srcLocal); err != nil {
		return mapErr(err)
	}
	dstLocal, err := util.SafeResolve(b.rootReal, dstAPI)
	if err != nil {
		return mapErr(err)
	}
	if _, err := os.Lstat(dstLocal); err == nil {
		return storage.ErrExists
	}
	if isSubpath(srcLocal, dstLocal) {
		return storage.ErrBadOp
	}
	var cerr error
	if fn == nil {
		cerr = util.CopyPath(srcLocal, dstLocal)
	} else {
		cerr = util.CopyPathWithProgress(ctx, srcLocal, dstLocal, func(copied int64) { fn(copied) })
	}
	if err := mapErr(cerr); err != nil {
		return err
	}
	return nil
}

// Usage 返回 p 所在文件系统的总容量与可用字节数（storage.Usager）。
func (b *Local) Usage(_ context.Context, p string) (total, free uint64, err error) {
	local, _, rerr := b.resolve(p)
	if rerr != nil {
		return 0, 0, rerr
	}
	return util.Usage(local)
}

// Walk 串行 WalkDir 遍历（NAS 多机械盘，并行遍历加剧寻道抖动）。
// 不跟随符号链接；隐藏目录直接跳过其子树。回调 relPath 为相对 root 的 API 路径片段。
//
// 错误传播约定：回调返回 storage.ErrStopWalk 或 ctx 取消时，原样向上返回，由最外层
// 调用者（service.Search）解释为「截断」或「超时」。Walk 自身不吞掉这些信号。
func (b *Local) Walk(ctx context.Context, root string, showHidden bool, fn func(string, model.FileInfo) error) error {
	local, _, err := b.resolve(root)
	if err != nil {
		return err
	}
	fi, err := os.Stat(local)
	if err != nil {
		return mapErr(err)
	}
	if !fi.IsDir() {
		return storage.ErrNotDir
	}

	return filepath.WalkDir(local, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if d != nil && d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		if p == local {
			return nil // 不把起点自身计入
		}
		info, ierr := d.Info()
		if ierr != nil {
			return nil
		}
		if !showHidden && util.IsHidden(d.Name(), info) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		rel, rerr := filepath.Rel(local, p)
		if rerr != nil {
			return nil
		}
		return fn(util.ToAPIPath(rel), b.walkInfo(info))
	})
}

// buildFileInfo 由 Lstat 结果组装 FileInfo，处理符号链接展示。
// parentAPIPath 为该条目所在目录的 API 路径，用于拼接符号链接目标。
func (b *Local) buildFileInfo(parentAPIPath, name string, fi os.FileInfo) model.FileInfo {
	out := model.FileInfo{
		Name:    name,
		Mode:    util.FormatMode(fi.Mode()),
		ModTime: fi.ModTime(),
	}
	isSymlink := fi.Mode()&os.ModeSymlink != 0
	out.IsSymlink = isSymlink
	if fi.IsDir() {
		out.Type = model.TypeDir
	} else {
		out.Type = model.TypeFile
		out.Size = fi.Size()
	}
	if isSymlink {
		b.resolveSymlink(parentAPIPath, name, &out)
	}
	return out
}

// walkInfo 为搜索遍历组装轻量 FileInfo（不解析符号链接目标，仅按自身 Lstat 信息）。
func (b *Local) walkInfo(fi os.FileInfo) model.FileInfo {
	out := model.FileInfo{
		Name:    fi.Name(),
		Mode:    util.FormatMode(fi.Mode()),
		ModTime: fi.ModTime(),
	}
	if fi.IsDir() {
		out.Type = model.TypeDir
	} else {
		out.Type = model.TypeFile
		out.Size = fi.Size()
	}
	out.IsSymlink = fi.Mode()&os.ModeSymlink != 0
	return out
}

// resolveSymlink 尽力解析符号链接：目标仍在 root 内则填 SymlinkTarget 与真实类型，
// 越界或解析失败则标记 Unreachable。
func (b *Local) resolveSymlink(parentAPIPath, name string, out *model.FileInfo) {
	linkAPIPath := path.Join(parentAPIPath, name)
	target, err := util.SafeResolve(b.rootReal, linkAPIPath)
	if err != nil {
		out.Unreachable = true
		return
	}
	ti, err := os.Stat(target)
	if err != nil {
		out.Unreachable = true
		return
	}
	rel := strings.TrimPrefix(target, b.rootReal)
	out.SymlinkTarget = util.CleanAPIPath(util.ToAPIPath(rel))
	if ti.IsDir() {
		out.Type = model.TypeDir
		out.Size = 0
	} else {
		out.Type = model.TypeFile
		out.Size = ti.Size()
	}
}

// isSubpath 判断 child 是否等于 parent 或位于 parent 子树内。
func isSubpath(parent, child string) bool {
	if parent == child {
		return true
	}
	p := parent
	if !strings.HasSuffix(p, string(os.PathSeparator)) {
		p += string(os.PathSeparator)
	}
	return strings.HasPrefix(child, p)
}

// ---- 分片上传（storage.Uploader）----
//
// 暂存布局：<stagingDir>/<uploadID>/<index>.part。uploadID 由上层（UploadService）生成的
// 高熵随机 token，仅做基本字符校验后用作目录名；分片字节先写 <index>.part.tmp 再 rename，
// 保证单分片落盘的原子性与重传幂等。

// validUploadID 校验 uploadID 仅含安全字符（token 为 Base64URL：字母数字与 - _），
// 防止借 uploadID 注入路径分隔符逃逸 stagingDir。
func validUploadID(id string) bool {
	if id == "" || len(id) > 128 {
		return false
	}
	for _, c := range id {
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') ||
			(c >= '0' && c <= '9') || c == '-' || c == '_' {
			continue
		}
		return false
	}
	return true
}

// stagingPath 返回某次上传的暂存目录绝对路径。
func (b *Local) stagingPath(uploadID string) string {
	return filepath.Join(b.stagingDir, uploadID)
}

// chunkPath 返回某分片的暂存文件绝对路径。
func chunkName(index int) string { return strconv.Itoa(index) + ".part" }

// StageChunk 将第 index 个分片写入暂存区（先写临时文件再 rename，同 index 重传幂等覆盖）。
func (b *Local) StageChunk(_ context.Context, uploadID string, index int, r io.Reader) (int64, error) {
	if b.stagingDir == "" {
		return 0, storage.ErrNotSupported
	}
	if !validUploadID(uploadID) || index < 0 {
		return 0, storage.ErrBadOp
	}
	dir := b.stagingPath(uploadID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return 0, mapErr(err)
	}
	final := filepath.Join(dir, chunkName(index))
	tmp, err := os.CreateTemp(dir, chunkName(index)+".*.tmp")
	if err != nil {
		return 0, mapErr(err)
	}
	tmpName := tmp.Name()
	n, werr := io.Copy(tmp, r)
	if cerr := tmp.Close(); werr == nil {
		werr = cerr
	}
	if werr != nil {
		_ = os.Remove(tmpName)
		return 0, mapErr(werr)
	}
	if err := os.Rename(tmpName, final); err != nil {
		_ = os.Remove(tmpName)
		return 0, mapErr(err)
	}
	return n, nil
}

// MergeUpload 按序拼接 [0, totalChunks) 分片到 dst，成功后删除暂存区。
// overwrite=false 且 dst 已存在 → ErrExists；overwrite=true 则原子替换。
func (b *Local) MergeUpload(_ context.Context, uploadID, dst string, totalChunks int, overwrite bool) error {
	if b.stagingDir == "" {
		return storage.ErrNotSupported
	}
	if !validUploadID(uploadID) || totalChunks < 1 {
		return storage.ErrBadOp
	}
	dstAPI := util.CleanAPIPath(dst)
	if dstAPI == "/" {
		return storage.ErrBadOp
	}
	if err := util.ValidateName(path.Base(dstAPI)); err != nil {
		return storage.ErrInvalidName
	}
	dstLocal, err := util.SafeResolve(b.rootReal, dstAPI)
	if err != nil {
		return mapErr(err)
	}
	if _, err := os.Stat(filepath.Dir(dstLocal)); err != nil {
		return mapErr(err) // 父目录须存在
	}
	if _, err := os.Lstat(dstLocal); err == nil && !overwrite {
		return storage.ErrExists
	}

	stageDir := b.stagingPath(uploadID)
	// 先在目标同目录写临时文件，拼接完成后 rename，避免合并中途损坏既有文件。
	tmp, err := os.CreateTemp(filepath.Dir(dstLocal), ".flist-upload-*.tmp")
	if err != nil {
		return mapErr(err)
	}
	tmpName := tmp.Name()
	if err := b.concatChunks(tmp, stageDir, totalChunks); err != nil {
		tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return mapErr(err)
	}
	// 覆盖：rename 在多数系统对已存在目标是原子替换；为兼容 Windows，先尝试删旧。
	if overwrite {
		if fi, serr := os.Lstat(dstLocal); serr == nil {
			if fi.IsDir() {
				_ = os.Remove(tmpName)
				return storage.ErrExists // 目标是目录，不可被文件覆盖
			}
			_ = os.Remove(dstLocal)
		}
	}
	if err := os.Rename(tmpName, dstLocal); err != nil {
		_ = os.Remove(tmpName)
		return mapErr(err)
	}
	_ = os.RemoveAll(stageDir) // 合并成功后清理暂存区（失败不影响结果，留待 sweep）
	return nil
}

// concatChunks 按 index 升序把所有分片内容写入 w；任一分片缺失则报错。
func (b *Local) concatChunks(w io.Writer, stageDir string, totalChunks int) error {
	for i := 0; i < totalChunks; i++ {
		cp := filepath.Join(stageDir, chunkName(i))
		in, err := os.Open(cp)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("chunk %d missing: %w", i, storage.ErrBadOp)
			}
			return mapErr(err)
		}
		_, cerr := io.Copy(w, in)
		in.Close()
		if cerr != nil {
			return mapErr(cerr)
		}
	}
	return nil
}

// AbortChunk 删除某次上传中单个分片的暂存文件（幂等）。
func (b *Local) AbortChunk(uploadID string, index int) error {
	if b.stagingDir == "" || !validUploadID(uploadID) || index < 0 {
		return nil
	}
	return os.Remove(filepath.Join(b.stagingPath(uploadID), chunkName(index)))
}

// AbortUpload 删除某次上传的暂存区（幂等）。
func (b *Local) AbortUpload(uploadID string) error {
	if b.stagingDir == "" || !validUploadID(uploadID) {
		return nil
	}
	return os.RemoveAll(b.stagingPath(uploadID))
}

// SweepStaging 删除 mtime 早于 now-maxAge 的孤儿暂存目录，返回清理数量。
func (b *Local) SweepStaging(maxAge time.Duration) (int, error) {
	if b.stagingDir == "" {
		return 0, nil
	}
	entries, err := os.ReadDir(b.stagingDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return 0, nil
		}
		return 0, mapErr(err)
	}
	cutoff := time.Now().Add(-maxAge)
	cleaned := 0
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		fi, ierr := e.Info()
		if ierr != nil {
			continue
		}
		if fi.ModTime().Before(cutoff) {
			if rerr := os.RemoveAll(filepath.Join(b.stagingDir, e.Name())); rerr == nil {
				cleaned++
			}
		}
	}
	return cleaned, nil
}

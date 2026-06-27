// Package local 实现基于本地文件系统的 storage.Backend。
//
// 它承接原 FileService 中所有与 OS 文件系统强耦合的逻辑：SafeResolve 路径安全、
// 符号链接解析、权限格式化、隐藏文件检测、递归复制 / 移动 / 删除等。对上层（service）
// 只暴露 storage.Backend / Walker 接口，入参均为相对本驱动 root 的 API 路径。
package local

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"

	"flist/internal/model"
	"flist/internal/storage"
	"flist/internal/util"
)

// Local 是本地文件系统驱动，持有启动时缓存的 rootReal（绝对真实路径）。
type Local struct {
	rootReal string
}

// New 构造本地驱动。rootReal 必须是已通过 util.ResolveRoot 标准化的绝对真实路径。
func New(rootReal string) *Local {
	return &Local{rootReal: rootReal}
}

// 确保 Local 实现了核心接口与可选接口。
var (
	_ storage.Backend = (*Local)(nil)
	_ storage.Walker  = (*Local)(nil)
)

func (b *Local) Name() string { return "local" }

func (b *Local) Capabilities() storage.Caps {
	return storage.Caps{Write: true, Copy: true, Upload: true, DiskUsage: true}
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
func (b *Local) Move(_ context.Context, src, dst string) error {
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
	if err := util.MovePath(srcLocal, dstLocal); err != nil {
		return mapErr(err)
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
func (b *Local) Copy(_ context.Context, src, dst string) error {
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
	if err := util.CopyPath(srcLocal, dstLocal); err != nil {
		return mapErr(err)
	}
	return nil
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

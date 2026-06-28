package util

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
)

// ErrMaxDepth 表示递归操作超过最大深度，可能是异常 / 恶意目录结构。
var ErrMaxDepth = errors.New("max recursion depth exceeded")

// maxRecursionDepth 限制递归复制 / 删除的最大层级，防御异常或循环结构。
const maxRecursionDepth = 100

// CopyPath 递归复制 src 到 dst（dst 必须不存在）。
// 普通文件复制内容与权限位；目录递归复制其下全部条目。
// 符号链接（flist 自身不创建）遇到时跳过，不重建链接。
func CopyPath(src, dst string) error {
	return copyPath(context.Background(), src, dst, 0, nil)
}

// CopyPathWithProgress 与 CopyPath 相同，但通过 onProgress 上报当前文件已复制字节数，
// 并在 ctx 取消时中止复制（返回 ctx.Err()）。onProgress 为 nil 时退化为 CopyPath。
// 每进入一个普通文件，onProgress 的计数从 0 重新开始（项内字节进度）。
func CopyPathWithProgress(ctx context.Context, src, dst string, onProgress func(copied int64)) error {
	return copyPath(ctx, src, dst, 0, onProgress)
}

func copyPath(ctx context.Context, src, dst string, depth int, onProgress func(int64)) error {
	if depth > maxRecursionDepth {
		return ErrMaxDepth
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	fi, err := os.Lstat(src)
	if err != nil {
		return err
	}

	switch {
	case fi.Mode()&os.ModeSymlink != 0:
		// 符号链接不重建，跳过（与「flist 自身不创建符号链接」一致）。
		return nil
	case fi.IsDir():
		if err := os.Mkdir(dst, dirPerm(fi)); err != nil {
			return err
		}
		entries, err := os.ReadDir(src)
		if err != nil {
			return err
		}
		for _, e := range entries {
			if err := ctx.Err(); err != nil {
				return err
			}
			cs := filepath.Join(src, e.Name())
			cd := filepath.Join(dst, e.Name())
			if err := copyPath(ctx, cs, cd, depth+1, onProgress); err != nil {
				return err
			}
		}
		return nil
	case fi.Mode().IsRegular():
		return copyFile(ctx, src, dst, fi.Mode().Perm(), onProgress)
	default:
		// 设备 / 管道 / 套接字等特殊文件不复制，跳过。
		return nil
	}
}

// copyFile 复制单个普通文件内容并应用权限位。dst 不存在时创建。
// onProgress 非 nil 时按写入字节数回调，并在 ctx 取消时中止（返回 ctx.Err()）。
func copyFile(ctx context.Context, src, dst string, perm os.FileMode, onProgress func(int64)) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm)
	if err != nil {
		return err
	}
	var w io.Writer = out
	if onProgress != nil {
		w = &progressWriter{w: out, ctx: ctx, fn: onProgress}
	}
	if _, err := io.Copy(w, in); err != nil {
		out.Close()
		os.Remove(dst)
		return err
	}
	return out.Close()
}

// progressWriter 包装底层 writer，按写入字节数回调进度，并在 ctx 取消时返回错误中止 io.Copy。
type progressWriter struct {
	w   io.Writer
	ctx context.Context
	n   int64
	fn  func(int64)
}

func (p *progressWriter) Write(b []byte) (int, error) {
	if err := p.ctx.Err(); err != nil {
		return 0, err
	}
	n, err := p.w.Write(b)
	p.n += int64(n)
	p.fn(p.n)
	return n, err
}

// dirPerm 返回目录复制时使用的权限位，缺省兜底 0755。
func dirPerm(fi os.FileInfo) os.FileMode {
	p := fi.Mode().Perm()
	if p == 0 {
		return 0o755
	}
	return p
}

// RemovePath 递归删除 path（深度受限）。
func RemovePath(path string) error {
	return removePath(path, 0)
}

func removePath(path string, depth int) error {
	if depth > maxRecursionDepth {
		return ErrMaxDepth
	}
	fi, err := os.Lstat(path)
	if err != nil {
		return err
	}
	// 目录（且非符号链接）需先递归清空子项。
	if fi.IsDir() && fi.Mode()&os.ModeSymlink == 0 {
		entries, err := os.ReadDir(path)
		if err != nil {
			return err
		}
		for _, e := range entries {
			if err := removePath(filepath.Join(path, e.Name()), depth+1); err != nil {
				return err
			}
		}
	}
	return os.Remove(path)
}

// MovePath 将 src 移动到 dst：优先 os.Rename（同分区原子操作）；
// 遇跨分区（EXDEV / ERROR_NOT_SAME_DEVICE）时回退为 CopyPath + RemovePath。
func MovePath(src, dst string) error {
	return movePath(context.Background(), src, dst, nil)
}

// MovePathWithProgress 与 MovePath 相同，但跨分区回退为复制时通过 onProgress 上报进度，
// 并在 ctx 取消时中止。同分区 rename 为瞬时操作，不产生进度回调。
// onProgress 为 nil 时退化为 MovePath。
func MovePathWithProgress(ctx context.Context, src, dst string, onProgress func(int64)) error {
	return movePath(ctx, src, dst, onProgress)
}

func movePath(ctx context.Context, src, dst string, onProgress func(int64)) error {
	err := os.Rename(src, dst)
	if err == nil {
		return nil
	}
	if !isCrossDevice(err) {
		return err
	}
	// 跨分区回退：先复制，成功后再删源；复制中途失败 / 取消则清理半成品 dst。
	if cerr := copyPath(ctx, src, dst, 0, onProgress); cerr != nil {
		_ = removePath(dst, 0)
		return cerr
	}
	return removePath(src, 0)
}

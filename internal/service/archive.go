// Package service —— 打包下载（archive）编排。
//
// archive 不引入新的 storage.Backend 接口，只组合既有的 Stat（预检）、Walk（递归列举）、
// Open（读文件字节），在 service 层做流式 zip 编排：边写边发，服务端内存占用与单文件
// 下载同量级（不随包大小线性增长）。智能压缩：已压缩格式用 Store 直存，其余用 Deflate。
package service

import (
	"archive/zip"
	"context"
	"errors"
	"io"
	"path"
	"strconv"
	"strings"
	"time"

	"flist/internal/model"
	"flist/internal/storage"
	"flist/internal/util"
)

// ArchiveTarget 是一个顶层打包条目：经预检解析后的 API 路径、zip 内顶层名与类型。
type ArchiveTarget struct {
	APIPath string // 相对 root 的清理后路径
	ZipName string // zip 内顶层名（已对同名去重）
	IsDir   bool
}

// ResolveArchiveTargets 预检并解析待打包的顶层路径（须在写首字节前完成）：
//   - 每个路径经 backend.Stat 校验存在性与越界，任一失败即返回错误（不发送任何 zip 字节）
//   - 计算 zip 内顶层名（取 basename），跨目录同名做 " (2)" 递增去重
//
// 返回的 targets 顺序与入参一致。
func (s *FileService) ResolveArchiveTargets(ctx context.Context, paths []string) ([]ArchiveTarget, error) {
	targets := make([]ArchiveTarget, 0, len(paths))
	used := make(map[string]bool)
	for _, p := range paths {
		cleaned := util.CleanAPIPath(p)
		// 禁止打包整个 root：无明确顶层名、易与其他选中项重名，且「下载整个根」语义不当。
		if cleaned == "/" {
			return nil, storage.ErrBadOp
		}
		info, err := s.backend.Stat(ctx, cleaned)
		if err != nil {
			return nil, err
		}
		base := path.Base(cleaned)
		name := dedupName(used, base)
		used[name] = true
		targets = append(targets, ArchiveTarget{
			APIPath: cleaned,
			ZipName: name,
			IsDir:   info.Type == model.TypeDir,
		})
	}
	return targets, nil
}

// WriteArchive 把 targets 流式打包为 zip 写入 w（边写边发，不在内存 / 磁盘暂存整包）。
// 调用方须先完成预检（ResolveArchiveTargets）并已发送响应头。
//
// 容错语义：单条文件打开失败（被并发删除 / 特殊文件 / 权限）经 onSkip 记录并跳过，
// 不中断整个归档；符号链接一律跳过。返回非 nil error 仅表示 zip 写入层面的致命错误
//（已写出部分字节，无法回滚）—— 此时不会写出 zip 中央目录，客户端据此判定下载损坏。
func (s *FileService) WriteArchive(ctx context.Context, w io.Writer, targets []ArchiveTarget, onSkip func(apiPath string, err error)) error {
	zw := zip.NewWriter(w)
	for _, t := range targets {
		if err := ctx.Err(); err != nil {
			return err
		}
		var err error
		if t.IsDir {
			err = s.archiveDir(ctx, zw, t.APIPath, t.ZipName, onSkip)
		} else {
			err = s.archiveFile(ctx, zw, t.APIPath, t.ZipName, onSkip)
		}
		if err != nil {
			return err
		}
	}
	return zw.Close()
}

// archiveFile 把单个文件作为一条 zip 条目写入。打开失败时跳过（onSkip），不视为致命。
func (s *FileService) archiveFile(ctx context.Context, zw *zip.Writer, apiPath, zipName string, onSkip func(string, error)) error {
	f, info, err := s.backend.Open(ctx, apiPath)
	if err != nil {
		if onSkip != nil {
			onSkip(apiPath, err)
		}
		return nil
	}
	defer f.Close()
	wr, err := createZipEntry(zw, zipName, info.ModTime)
	if err != nil {
		return err
	}
	_, err = io.Copy(wr, f)
	return err // 读取中途失败为致命（条目已部分写出，无法回滚）
}

// archiveDir 把目录及其子树递归写入 zip：先写目录自身条目（保留空目录），
// 再经 walk（showHidden=true）递归子项。符号链接跳过，子文件打开失败跳过。
func (s *FileService) archiveDir(ctx context.Context, zw *zip.Writer, apiPath, zipName string, onSkip func(string, error)) error {
	if err := createZipDir(zw, zipName); err != nil {
		return err
	}
	var fatal error
	walkErr := s.walk(ctx, apiPath, true, func(rel string, info model.FileInfo) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if info.IsSymlink {
			return nil // 不跟随、不打包符号链接
		}
		entryName := zipName + "/" + rel
		if info.Type == model.TypeDir {
			if err := createZipDir(zw, entryName); err != nil {
				fatal = err
				return storage.ErrStopWalk
			}
			return nil
		}
		child := path.Join(apiPath, rel)
		f, finfo, oerr := s.backend.Open(ctx, child)
		if oerr != nil {
			if onSkip != nil {
				onSkip(child, oerr) // 特殊文件 / 并发删除 / 权限：跳过
			}
			return nil
		}
		wr, cerr := createZipEntry(zw, entryName, finfo.ModTime)
		if cerr != nil {
			f.Close()
			fatal = cerr
			return storage.ErrStopWalk
		}
		_, ioerr := io.Copy(wr, f)
		f.Close()
		if ioerr != nil {
			fatal = ioerr
			return storage.ErrStopWalk
		}
		return nil
	})
	if fatal != nil {
		return fatal
	}
	if walkErr != nil && !errors.Is(walkErr, storage.ErrStopWalk) {
		return walkErr // ctx 取消等 walk 自身错误
	}
	return nil
}

// createZipEntry 创建一条文件条目并返回其写入器。按扩展名智能选择压缩方式：
// 已压缩格式用 Store 直存，其余用 Deflate。
func createZipEntry(zw *zip.Writer, name string, modTime time.Time) (io.Writer, error) {
	method := zip.Deflate
	if util.IsCompressedExt(name) {
		method = zip.Store
	}
	return zw.CreateHeader(&zip.FileHeader{
		Name:     name,
		Method:   method,
		Modified: modTime,
	})
}

// createZipDir 创建一条目录条目（名以 / 结尾，保留空目录）。
func createZipDir(zw *zip.Writer, name string) error {
	if !strings.HasSuffix(name, "/") {
		name += "/"
	}
	_, err := zw.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Store})
	return err
}

// dedupName 为 zip 顶层名去重：base 已用过则按 "name (2).ext" 递增探测首个未用名。
func dedupName(used map[string]bool, base string) string {
	if !used[base] {
		return base
	}
	ext := path.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	if stem == "" { // dotfile（如 .env）：整体作主名
		stem = base
		ext = ""
	}
	for i := 2; ; i++ {
		cand := stem + " (" + strconv.Itoa(i) + ")" + ext
		if !used[cand] {
			return cand
		}
	}
}

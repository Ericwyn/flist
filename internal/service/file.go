package service

import (
	"errors"
	"io"
	"os"
	"path"
	"sort"
	"strings"
	"time"

	"flist/internal/model"
	"flist/internal/util"
)

// 文件服务层错误，handler 据此映射错误码。
var (
	ErrNotFound  = errors.New("path not found")
	ErrNotDir    = errors.New("not a directory")
	ErrNotFile   = errors.New("not a regular file")
	ErrForbidden = errors.New("permission denied")
)

const (
	defaultPageSize = 200
	maxPageSize     = 1000
	previewMaxBytes = 64 << 10 // 64 KiB
	sniffBytes      = 512
)

// ListOptions 控制目录列表的排序、分页与隐藏文件展示。
type ListOptions struct {
	Sort       string // name | size | mtime
	Order      string // asc | desc
	ShowHidden bool
	Page       int
	PageSize   int
}

// FileService 提供只读文件操作，持有启动时缓存的 rootReal。
type FileService struct {
	rootReal string
}

// NewFileService 构造文件服务。rootReal 必须是已标准化的绝对真实路径。
func NewFileService(rootReal string) *FileService {
	return &FileService{rootReal: rootReal}
}

// mapPathErr 将底层 OS 错误归一化为服务层错误。
func mapPathErr(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, util.ErrPathTraversal):
		return util.ErrPathTraversal
	case errors.Is(err, os.ErrNotExist):
		return ErrNotFound
	case errors.Is(err, os.ErrPermission):
		return ErrForbidden
	default:
		return err
	}
}

// List 列出目录内容，支持排序、隐藏过滤与分页。
func (s *FileService) List(apiPath string, opts ListOptions) (*model.ListResult, error) {
	cleaned := util.CleanAPIPath(apiPath)
	local, err := util.SafeResolve(s.rootReal, cleaned)
	if err != nil {
		return nil, mapPathErr(err)
	}

	info, err := os.Stat(local)
	if err != nil {
		return nil, mapPathErr(err)
	}
	if !info.IsDir() {
		return nil, ErrNotDir
	}

	dirEntries, err := os.ReadDir(local)
	if err != nil {
		return nil, mapPathErr(err)
	}

	items := make([]model.FileInfo, 0, len(dirEntries))
	for _, de := range dirEntries {
		name := de.Name()
		fi, lerr := de.Info() // 等价于 Lstat：不跟随符号链接
		if lerr != nil {
			// 单条失败降级跳过，不中断整个目录。
			continue
		}
		if !opts.ShowHidden && util.IsHidden(name, fi) {
			continue
		}
		items = append(items, s.buildFileInfo(cleaned, name, fi))
	}

	total := len(items)
	sortItems(items, opts.Sort, opts.Order)

	page, pageSize := normalizePaging(opts.Page, opts.PageSize)
	start := (page - 1) * pageSize
	if start > total {
		start = total
	}
	end := start + pageSize
	if end > total {
		end = total
	}

	return &model.ListResult{
		Path:     cleaned,
		Total:    total,
		Page:     page,
		PageSize: pageSize,
		Items:    items[start:end],
	}, nil
}

// Stat 返回单个文件/目录信息。
func (s *FileService) Stat(apiPath string) (*model.FileInfo, error) {
	cleaned := util.CleanAPIPath(apiPath)
	local, err := util.SafeResolve(s.rootReal, cleaned)
	if err != nil {
		return nil, mapPathErr(err)
	}
	fi, err := os.Lstat(local)
	if err != nil {
		return nil, mapPathErr(err)
	}
	parent := path.Dir(cleaned)
	info := s.buildFileInfo(parent, fi.Name(), fi)
	return &info, nil
}

// PreviewText 读取文本文件的前 N 字节预览；非文本返回类型提示但不含内容。
func (s *FileService) PreviewText(apiPath string) (*model.PreviewResult, error) {
	cleaned := util.CleanAPIPath(apiPath)
	local, err := util.SafeResolve(s.rootReal, cleaned)
	if err != nil {
		return nil, mapPathErr(err)
	}
	fi, err := os.Stat(local)
	if err != nil {
		return nil, mapPathErr(err)
	}
	if fi.IsDir() {
		return nil, ErrNotFile
	}
	if !fi.Mode().IsRegular() {
		return nil, ErrNotFile
	}

	f, err := os.Open(local)
	if err != nil {
		return nil, mapPathErr(err)
	}
	defer f.Close()

	buf := make([]byte, previewMaxBytes)
	n, err := io.ReadFull(f, buf)
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		return nil, mapPathErr(err)
	}
	data := buf[:n]

	res := &model.PreviewResult{
		Size:         fi.Size(),
		PreviewBytes: previewMaxBytes,
	}

	// 文本判定：扩展名白名单优先，否则对样本做二进制嗅探。
	sample := data
	if len(sample) > sniffBytes {
		sample = sample[:sniffBytes]
	}
	isText := util.IsTextExt(fi.Name()) || (util.DetectKind(fi.Name()) == util.KindUnknown && util.SniffText(sample))
	// 若扩展名属于已知二进制媒体类型，直接给出类型提示。
	kind := util.DetectKind(fi.Name())
	if kind == util.KindImage || kind == util.KindVideo || kind == util.KindAudio {
		res.Type = string(kind)
		return res, nil
	}
	if !isText || !util.SniffText(sample) {
		res.Type = "binary"
		return res, nil
	}

	res.Type = "text"
	res.Content = string(data)
	res.Truncated = fi.Size() > int64(n)
	return res, nil
}

// DownloadTarget 是下载所需的打开文件与元信息，调用方负责关闭 File。
type DownloadTarget struct {
	File    *os.File
	Info    os.FileInfo
	ModTime time.Time
}

// OpenForDownload 打开普通文件供下载，返回的 File 由调用方关闭。
func (s *FileService) OpenForDownload(apiPath string) (*DownloadTarget, error) {
	cleaned := util.CleanAPIPath(apiPath)
	local, err := util.SafeResolve(s.rootReal, cleaned)
	if err != nil {
		return nil, mapPathErr(err)
	}
	fi, err := os.Stat(local)
	if err != nil {
		return nil, mapPathErr(err)
	}
	if fi.IsDir() {
		return nil, ErrNotFile
	}
	if !fi.Mode().IsRegular() {
		return nil, ErrNotFile
	}
	f, err := os.Open(local)
	if err != nil {
		return nil, mapPathErr(err)
	}
	return &DownloadTarget{File: f, Info: fi, ModTime: fi.ModTime()}, nil
}

// buildFileInfo 由 Lstat 结果组装 FileInfo，处理符号链接展示。
// parentAPIPath 为该条目所在目录的 API 路径，用于拼接符号链接目标。
func (s *FileService) buildFileInfo(parentAPIPath, name string, fi os.FileInfo) model.FileInfo {
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
		s.resolveSymlink(parentAPIPath, name, &out)
	}
	return out
}

// resolveSymlink 尽力解析符号链接：目标仍在 root 内则填 SymlinkTarget 与真实类型，
// 越界或解析失败则标记 Unreachable。
func (s *FileService) resolveSymlink(parentAPIPath, name string, out *model.FileInfo) {
	linkAPIPath := path.Join(parentAPIPath, name)
	target, err := util.SafeResolve(s.rootReal, linkAPIPath)
	if err != nil {
		out.Unreachable = true
		return
	}
	ti, err := os.Stat(target) // 跟随链接取目标信息
	if err != nil {
		out.Unreachable = true
		return
	}
	// 将真实本地路径转回相对 root 的 API 路径。
	rel := strings.TrimPrefix(target, s.rootReal)
	rel = util.CleanAPIPath(util.ToAPIPath(rel))
	out.SymlinkTarget = rel
	if ti.IsDir() {
		out.Type = model.TypeDir
		out.Size = 0
	} else {
		out.Type = model.TypeFile
		out.Size = ti.Size()
	}
}

// sortItems 按目录优先 + 指定键 + 升降序排序。
func sortItems(items []model.FileInfo, sortKey, order string) {
	desc := order == "desc"
	less := func(i, j int) bool {
		a, b := items[i], items[j]
		// 目录优先，不受 order 影响。
		aDir := a.Type == model.TypeDir
		bDir := b.Type == model.TypeDir
		if aDir != bDir {
			return aDir
		}
		var result bool
		switch sortKey {
		case "size":
			if a.Size != b.Size {
				result = a.Size < b.Size
			} else {
				result = strings.ToLower(a.Name) < strings.ToLower(b.Name)
			}
		case "mtime":
			if !a.ModTime.Equal(b.ModTime) {
				result = a.ModTime.Before(b.ModTime)
			} else {
				result = strings.ToLower(a.Name) < strings.ToLower(b.Name)
			}
		default: // name
			result = strings.ToLower(a.Name) < strings.ToLower(b.Name)
		}
		if desc {
			return !result
		}
		return result
	}
	sort.SliceStable(items, less)
}

// normalizePaging 归一化分页参数。
func normalizePaging(page, pageSize int) (int, int) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = defaultPageSize
	}
	if pageSize > maxPageSize {
		pageSize = maxPageSize
	}
	return page, pageSize
}

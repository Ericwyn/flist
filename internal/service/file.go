package service

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
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
	ErrExists    = errors.New("target already exists")
	ErrBadOp     = errors.New("invalid operation")
)

const (
	defaultPageSize = 200
	maxPageSize     = 1000
	previewMaxBytes = 64 << 10 // 64 KiB
	sniffBytes      = 512

	defaultSearchLimit = 500
	maxSearchLimit     = 1000
)

// errStopWalk 是搜索遍历提前结束的哨兵错误（命中上限或超时），非真正错误。
var errStopWalk = errors.New("stop walk")

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

// Mkdir 创建单层目录。父目录须存在，basename 经文件名校验，目标已存在则冲突。
// 返回规范化后的 API 路径。
func (s *FileService) Mkdir(apiPath string) (string, error) {
	cleaned := util.CleanAPIPath(apiPath)
	if cleaned == "/" {
		return "", ErrExists // root 已存在
	}
	if err := util.ValidateName(path.Base(cleaned)); err != nil {
		return "", util.ErrNameInvalid
	}
	local, err := util.SafeResolve(s.rootReal, cleaned)
	if err != nil {
		return "", mapPathErr(err)
	}
	// 父目录必须存在（仅建单层，不递归创建）。
	if _, err := os.Stat(filepath.Dir(local)); err != nil {
		return "", mapPathErr(err)
	}
	if err := os.Mkdir(local, 0o755); err != nil {
		if errors.Is(err, os.ErrExist) {
			return "", ErrExists
		}
		return "", mapPathErr(err)
	}
	return cleaned, nil
}

// Touch 创建空文件。父目录须存在，basename 经文件名校验，目标已存在则冲突（不 truncate）。
// 返回规范化后的 API 路径。
func (s *FileService) Touch(apiPath string) (string, error) {
	cleaned := util.CleanAPIPath(apiPath)
	if cleaned == "/" {
		return "", ErrExists
	}
	if err := util.ValidateName(path.Base(cleaned)); err != nil {
		return "", util.ErrNameInvalid
	}
	local, err := util.SafeResolve(s.rootReal, cleaned)
	if err != nil {
		return "", mapPathErr(err)
	}
	if _, err := os.Stat(filepath.Dir(local)); err != nil {
		return "", mapPathErr(err)
	}
	// O_CREATE|O_EXCL 保证「不存在才创建」的原子性。
	f, err := os.OpenFile(local, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return "", ErrExists
		}
		return "", mapPathErr(err)
	}
	_ = f.Close()
	return cleaned, nil
}

// Move 批量移动 / 重命名，尽力而为，逐项返回结果。dst 语义见 docs/4.phase2 §5.3。
func (s *FileService) Move(srcs []string, dst string) []model.OpResult {
	results := make([]model.OpResult, 0, len(srcs))
	cleanedDst := util.CleanAPIPath(dst)

	// 判定 dst 是否为已存在目录，决定「移入目录」还是「重命名」语义。
	dstExists, dstIsDir := false, false
	if dstLocal, derr := util.SafeResolve(s.rootReal, cleanedDst); derr == nil {
		if fi, err := os.Stat(dstLocal); err == nil {
			dstExists = true
			dstIsDir = fi.IsDir()
		}
	}

	for _, src := range srcs {
		srcClean := util.CleanAPIPath(src)
		var targetAPI string
		if dstExists && dstIsDir {
			// dst 是已存在目录：移入该目录，落点为 dst/basename。
			targetAPI = path.Join(cleanedDst, path.Base(srcClean))
		} else {
			// dst 不存在或为已存在文件：按「重命名 / 移动到指定名」处理，仅单个 src 合法。
			// 落点已存在（含 dst 是文件的情形）由 moveOne 的冲突检查返回 file_exists。
			if len(srcs) != 1 {
				results = append(results, opFail(srcClean, ErrNotDir))
				continue
			}
			targetAPI = cleanedDst
		}
		results = append(results, s.moveOne(srcClean, targetAPI))
	}
	return results
}

// moveOne 执行单个移动 / 重命名。
func (s *FileService) moveOne(srcAPI, dstAPI string) model.OpResult {
	if srcAPI == "/" {
		return opFail(srcAPI, ErrBadOp) // 不允许移动 root 自身
	}
	if err := util.ValidateName(path.Base(dstAPI)); err != nil {
		return opFail(srcAPI, util.ErrNameInvalid)
	}
	srcLocal, err := util.SafeResolve(s.rootReal, srcAPI)
	if err != nil {
		return opFail(srcAPI, mapPathErr(err))
	}
	if _, err := os.Lstat(srcLocal); err != nil {
		return opFail(srcAPI, mapPathErr(err))
	}
	dstLocal, err := util.SafeResolve(s.rootReal, dstAPI)
	if err != nil {
		return opFail(srcAPI, mapPathErr(err))
	}
	// 落点不得已存在（不覆盖）。
	if _, err := os.Lstat(dstLocal); err == nil {
		return opFail(srcAPI, ErrExists)
	}
	// 不允许把目录移动进其自身子树（含移动到自身）。
	if isSubpath(srcLocal, dstLocal) {
		return opFail(srcAPI, ErrBadOp)
	}
	if err := util.MovePath(srcLocal, dstLocal); err != nil {
		return opFail(srcAPI, mapPathErr(err))
	}
	return model.OpResult{Src: srcAPI, OK: true}
}

// Delete 批量递归删除，尽力而为，逐项返回结果。禁止删除 root 自身。
func (s *FileService) Delete(paths []string) []model.OpResult {
	results := make([]model.OpResult, 0, len(paths))
	for _, p := range paths {
		cleaned := util.CleanAPIPath(p)
		if cleaned == "/" {
			results = append(results, opFail(cleaned, ErrBadOp))
			continue
		}
		local, err := util.SafeResolve(s.rootReal, cleaned)
		if err != nil {
			results = append(results, opFail(cleaned, mapPathErr(err)))
			continue
		}
		if _, err := os.Lstat(local); err != nil {
			results = append(results, opFail(cleaned, mapPathErr(err)))
			continue
		}
		if err := util.RemovePath(local); err != nil {
			results = append(results, opFail(cleaned, mapPathErr(err)))
			continue
		}
		results = append(results, model.OpResult{Src: cleaned, OK: true})
	}
	return results
}

// SearchOptions 控制搜索行为。
type SearchOptions struct {
	Recursive  bool
	ShowHidden bool
	Limit      int
}

// Search 按文件名匹配搜索。串行遍历，受 ctx 超时与命中上限保护。
func (s *FileService) Search(ctx context.Context, base, query string, opts SearchOptions) (*model.SearchResult, error) {
	cleaned := util.CleanAPIPath(base)
	local, err := util.SafeResolve(s.rootReal, cleaned)
	if err != nil {
		return nil, mapPathErr(err)
	}
	fi, err := os.Stat(local)
	if err != nil {
		return nil, mapPathErr(err)
	}
	if !fi.IsDir() {
		return nil, ErrNotDir
	}

	limit := opts.Limit
	if limit < 1 {
		limit = defaultSearchLimit
	}
	if limit > maxSearchLimit {
		limit = maxSearchLimit
	}

	isGlob := strings.ContainsAny(query, "*?")
	lowerQuery := strings.ToLower(query)
	res := &model.SearchResult{Query: query, Base: cleaned, Items: []model.SearchHit{}}

	match := func(name string) bool {
		if isGlob {
			ok, err := path.Match(query, name)
			return err == nil && ok
		}
		return strings.Contains(strings.ToLower(name), lowerQuery)
	}

	if opts.Recursive {
		s.searchRecursive(ctx, local, match, opts.ShowHidden, limit, res)
	} else {
		s.searchFlat(local, match, opts.ShowHidden, limit, res)
	}
	return res, nil
}

// searchFlat 仅匹配 base 目录下的直接子项（非递归）。
func (s *FileService) searchFlat(dir string, match func(string) bool, showHidden bool, limit int, res *model.SearchResult) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, de := range entries {
		info, ierr := de.Info()
		if ierr != nil {
			continue
		}
		if !showHidden && util.IsHidden(de.Name(), info) {
			continue
		}
		if !match(de.Name()) {
			continue
		}
		if len(res.Items) >= limit {
			res.Truncated = true
			return
		}
		res.Items = append(res.Items, s.makeHit(filepath.Join(dir, de.Name()), info))
	}
}

// searchRecursive 串行 WalkDir 遍历，受 ctx 与命中上限控制；不跟随符号链接。
func (s *FileService) searchRecursive(ctx context.Context, root string, match func(string) bool, showHidden bool, limit int, res *model.SearchResult) {
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			// 单条不可读：目录则跳过其子树，文件则跳过自身。
			if d != nil && d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		select {
		case <-ctx.Done():
			res.TimedOut = true
			return errStopWalk
		default:
		}
		if p == root {
			return nil // 不把起点自身计入结果
		}
		info, ierr := d.Info()
		if ierr != nil {
			return nil
		}
		// 隐藏文件 / 目录过滤：隐藏目录直接跳过其子树。
		if !showHidden && util.IsHidden(d.Name(), info) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if match(d.Name()) {
			if len(res.Items) >= limit {
				res.Truncated = true
				return errStopWalk
			}
			res.Items = append(res.Items, s.makeHit(p, info))
		}
		return nil
	})
}

// makeHit 由本地路径与信息组装搜索命中（含转回 API 路径）。
func (s *FileService) makeHit(localPath string, info os.FileInfo) model.SearchHit {
	rel := strings.TrimPrefix(localPath, s.rootReal)
	apiPath := util.CleanAPIPath(util.ToAPIPath(rel))
	hit := model.SearchHit{
		Path:    apiPath,
		Name:    info.Name(),
		Mode:    util.FormatMode(info.Mode()),
		ModTime: info.ModTime(),
	}
	if info.IsDir() {
		hit.Type = model.TypeDir
	} else {
		hit.Type = model.TypeFile
		hit.Size = info.Size()
	}
	return hit
}

// opFail 构造一条失败的批量操作结果。
func opFail(src string, err error) model.OpResult {
	return model.OpResult{Src: src, OK: false, Error: errCodeName(err)}
}

// errCodeName 将服务层错误映射为对外的错误码名（与 handler 错误码表对应）。
func errCodeName(err error) string {
	switch {
	case errors.Is(err, util.ErrPathTraversal):
		return "path_traversal"
	case errors.Is(err, util.ErrNameInvalid):
		return "name_invalid"
	case errors.Is(err, ErrNotFound):
		return "path_not_found"
	case errors.Is(err, ErrForbidden):
		return "permission_denied"
	case errors.Is(err, ErrExists):
		return "file_exists"
	case errors.Is(err, ErrNotDir):
		return "not_a_dir"
	case errors.Is(err, ErrNotFile):
		return "not_a_file"
	case errors.Is(err, ErrBadOp):
		return "bad_request"
	default:
		return "internal_error"
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

// Package service 实现业务编排层。
//
// FileService 不再直接操作 OS 文件系统，而是面向 storage.Backend 接口做后端无关的
// 编排：排序 / 分页、搜索匹配与超时 / 截断、预览文本嗅探、批量结果聚合、能力校验。
// 具体的寻址、路径安全、符号链接、权限语义等由各驱动（local / webdav / Mux）负责。
package service

import (
	"context"
	"errors"
	"io"
	"path"
	"sort"
	"strconv"
	"strings"

	"flist/internal/model"
	"flist/internal/storage"
	"flist/internal/util"
)

// 文件服务层错误，handler 据此映射错误码。统一复用 storage 错误词表，
// 使「驱动 → 服务 → handler」三层共享同一组错误值（errors.Is 可直接命中）。
var (
	ErrNotFound     = storage.ErrNotFound
	ErrNotDir       = storage.ErrNotDir
	ErrNotFile      = storage.ErrNotFile
	ErrForbidden    = storage.ErrForbidden
	ErrExists       = storage.ErrExists
	ErrDiskFull     = storage.ErrDiskFull
	ErrBadOp        = storage.ErrBadOp
	ErrNotSupported = storage.ErrNotSupported
)

const (
	defaultPageSize = 200
	maxPageSize     = 1000
	previewMaxBytes = 64 << 10 // 64 KiB
	sniffBytes      = 512

	defaultSearchLimit = 500
	maxSearchLimit     = 1000

	maxRenameProbe = 1000 // 自动避让探测上限，防御构造大量同名导致的探测放大
)

// ListOptions 控制目录列表的排序、分页与隐藏文件展示。
type ListOptions struct {
	Sort       string // name | size | mtime
	Order      string // asc | desc
	ShowHidden bool
	Page       int
	PageSize   int
}

// FileService 提供文件操作编排，委托具体存取给注入的 storage.Backend。
type FileService struct {
	backend storage.Backend
	locker  *util.PathLocker // 写串行化（保存文本，与上传合并共享同一实例）
	maxEdit int64            // 单文件在线编辑大小上限（字节），<=0 表示不限
}

// NewFileService 构造文件服务。backend 为存储驱动（local / Mux / 远程驱动）。
// locker 用于文本保存的路径级写串行化（建议与 UploadService 共享同一实例，
// 使「文本保存」与「上传合并」对同一目标路径互斥）。maxEdit 为可编辑文件大小上限
//（字节，<=0 表示不限）。locker 为 nil 时自动创建一个独立实例（主要便于测试）。
func NewFileService(backend storage.Backend, locker *util.PathLocker, maxEdit int64) *FileService {
	if locker == nil {
		locker = util.NewPathLocker()
	}
	return &FileService{backend: backend, locker: locker, maxEdit: maxEdit}
}

// List 列出目录内容，支持排序、隐藏过滤与分页。排序 / 分页在服务层完成（后端无关）。
func (s *FileService) List(ctx context.Context, apiPath string, opts ListOptions) (*model.ListResult, error) {
	cleaned := util.CleanAPIPath(apiPath)
	items, err := s.backend.List(ctx, cleaned, opts.ShowHidden)
	if err != nil {
		return nil, err
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

// Stat 返回单个文件 / 目录信息。
func (s *FileService) Stat(ctx context.Context, apiPath string) (*model.FileInfo, error) {
	return s.backend.Stat(ctx, util.CleanAPIPath(apiPath))
}

// PreviewText 读取文本文件的前 N 字节预览；非文本返回类型提示但不含内容。
func (s *FileService) PreviewText(ctx context.Context, apiPath string) (*model.PreviewResult, error) {
	f, info, err := s.backend.Open(ctx, util.CleanAPIPath(apiPath))
	if err != nil {
		return nil, err
	}
	defer f.Close()

	buf := make([]byte, previewMaxBytes)
	n, err := io.ReadFull(f, buf)
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		return nil, err
	}
	data := buf[:n]

	res := &model.PreviewResult{
		Size:         info.Size,
		PreviewBytes: previewMaxBytes,
	}

	sample := data
	if len(sample) > sniffBytes {
		sample = sample[:sniffBytes]
	}
	// 已知二进制媒体类型直接给出类型提示，无需嗅探内容。
	kind := util.DetectKind(info.Name)
	if kind == util.KindImage || kind == util.KindVideo || kind == util.KindAudio {
		res.Type = string(kind)
		return res, nil
	}
	isText := util.IsTextExt(info.Name) || (kind == util.KindUnknown && util.SniffText(sample))
	if !isText || !util.SniffText(sample) {
		res.Type = "binary"
		return res, nil
	}

	res.Type = "text"
	res.Content = string(data)
	res.Truncated = info.Size > int64(n)
	return res, nil
}

// DownloadTarget 是下载所需的文件句柄与元信息，调用方负责关闭 File。
type DownloadTarget struct {
	File storage.File
	Info *model.FileInfo
}

// OpenForDownload 打开普通文件供下载，返回的 File 由调用方关闭。
func (s *FileService) OpenForDownload(ctx context.Context, apiPath string) (*DownloadTarget, error) {
	f, info, err := s.backend.Open(ctx, util.CleanAPIPath(apiPath))
	if err != nil {
		return nil, err
	}
	return &DownloadTarget{File: f, Info: info}, nil
}

// Mkdir 创建单层目录，返回规范化后的 API 路径。
func (s *FileService) Mkdir(ctx context.Context, apiPath string) (string, error) {
	cleaned := util.CleanAPIPath(apiPath)
	if err := s.backend.Mkdir(ctx, cleaned); err != nil {
		return "", err
	}
	return cleaned, nil
}

// Touch 创建空文件，返回规范化后的 API 路径。
func (s *FileService) Touch(ctx context.Context, apiPath string) (string, error) {
	cleaned := util.CleanAPIPath(apiPath)
	if err := s.backend.Create(ctx, cleaned); err != nil {
		return "", err
	}
	return cleaned, nil
}

// Move 批量移动 / 重命名，尽力而为，逐项返回结果。dst 语义见 docs/4.phase2 §5.3。
// dst 是否为已存在目录的判定（决定「移入」还是「重命名」）在服务层经 backend.Stat 完成，
// 单项的落点冲突 / 越界 / 自身子树等校验由 backend.Move 负责。
//
// autoRename 仅在「移入已存在目录」分支生效：落点同名时自动避让（name (2)）。
// 「重命名 / 移动到指定名」分支始终严格冲突（保留 Phase 2 §5.3 语义）。
func (s *FileService) Move(ctx context.Context, srcs []string, dst string, autoRename bool) []model.OpResult {
	results := make([]model.OpResult, 0, len(srcs))
	cleanedDst := util.CleanAPIPath(dst)
	dstExists, dstIsDir := s.statDir(ctx, cleanedDst)
	single := len(srcs) == 1

	for _, src := range srcs {
		srcClean := util.CleanAPIPath(src)
		targetAPI, fail := s.transferTarget(ctx, srcClean, cleanedDst, dstExists, dstIsDir, single, autoRename)
		if fail != nil {
			results = append(results, *fail)
			continue
		}
		if err := s.backend.Move(ctx, srcClean, targetAPI); err != nil {
			results = append(results, opFail(srcClean, err))
		} else {
			results = append(results, model.OpResult{Src: srcClean, OK: true})
		}
	}
	return results
}

// Copy 批量复制，尽力而为，逐项返回结果。dst 语义与 Move 一致。
// autoRename 同 Move；每项复制前做磁盘空间预检（驱动支持 Usager 时）。
func (s *FileService) Copy(ctx context.Context, srcs []string, dst string, autoRename bool) []model.OpResult {
	results := make([]model.OpResult, 0, len(srcs))
	cleanedDst := util.CleanAPIPath(dst)
	dstExists, dstIsDir := s.statDir(ctx, cleanedDst)
	single := len(srcs) == 1

	for _, src := range srcs {
		srcClean := util.CleanAPIPath(src)
		targetAPI, fail := s.transferTarget(ctx, srcClean, cleanedDst, dstExists, dstIsDir, single, autoRename)
		if fail != nil {
			results = append(results, *fail)
			continue
		}
		if err := s.checkSpace(ctx, srcClean, path.Dir(targetAPI)); err != nil {
			results = append(results, opFail(srcClean, err))
			continue
		}
		if err := s.backend.Copy(ctx, srcClean, targetAPI); err != nil {
			results = append(results, opFail(srcClean, err))
		} else {
			results = append(results, model.OpResult{Src: srcClean, OK: true})
		}
	}
	return results
}

// statDir 探测 dst 是否存在以及是否为目录（失败视为不存在）。
func (s *FileService) statDir(ctx context.Context, cleanedDst string) (exists, isDir bool) {
	if info, err := s.backend.Stat(ctx, cleanedDst); err == nil {
		return true, info.Type == model.TypeDir
	}
	return false, false
}

// StatDir 探测 dst 是否存在以及是否为目录（导出版，供异步文件操作服务复用）。
func (s *FileService) StatDir(ctx context.Context, dst string) (bool, bool) {
	return s.statDir(ctx, util.CleanAPIPath(dst))
}

// transferTarget 计算单项 move/copy 的落点 API 路径。
//   - dst 是已存在目录：落点 dst/basename；autoRename 时自动避让同名
//   - dst 不存在 / 是文件：仅单个 src 合法（重命名 / 移到指定名），否则该项 not_a_dir
//
// 返回非 nil 的 *OpResult 表示该项应直接记为失败。
func (s *FileService) transferTarget(ctx context.Context, srcClean, cleanedDst string, dstExists, dstIsDir, single, autoRename bool) (string, *model.OpResult) {
	if dstExists && dstIsDir {
		base := path.Base(srcClean)
		if autoRename {
			return s.avoidConflict(ctx, cleanedDst, base), nil
		}
		return path.Join(cleanedDst, base), nil
	}
	if !single {
		r := opFail(srcClean, storage.ErrNotDir)
		return "", &r
	}
	return cleanedDst, nil
}

// TransferTarget 计算单项 move/copy 的落点 API 路径（导出版，供异步文件操作服务复用）。
// 语义与 Move/Copy 内部完全一致，避免在异步路径上重复实现业务规则。
func (s *FileService) TransferTarget(ctx context.Context, src, dst string, dstExists, dstIsDir, single, autoRename bool) (string, *model.OpResult) {
	return s.transferTarget(ctx, util.CleanAPIPath(src), util.CleanAPIPath(dst), dstExists, dstIsDir, single, autoRename)
}

// avoidConflict 为「移入目录」的落点探测不冲突的名字：dir/base 已存在时，
// 按 "name (2).ext" → "name (3).ext" 递增探测首个不存在名（上限 maxRenameProbe）。
// 文件保留扩展名，目录及 dotfile 整体作为主名。
func (s *FileService) avoidConflict(ctx context.Context, dir, base string) string {
	target := path.Join(dir, base)
	if _, err := s.backend.Stat(ctx, target); err != nil {
		return target // 不存在，直接用
	}
	ext := path.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	if stem == "" { // dotfile（如 .bashrc）：整体作主名，不拆扩展名
		stem = base
		ext = ""
	}
	for i := 2; i < maxRenameProbe+2; i++ {
		cand := path.Join(dir, stem+" ("+strconv.Itoa(i)+")"+ext)
		if _, err := s.backend.Stat(ctx, cand); err != nil {
			return cand
		}
	}
	return target // 超限：回退原名，由 backend.Copy/Move 返回 ErrExists
}

// Usage 返回 apiPath 所在存储的总容量与可用空间（Phase 6 系统信息）。
// 驱动未实现 Usager（如纯虚拟根 Mux）时返回 storage.ErrNotSupported。
func (s *FileService) Usage(ctx context.Context, apiPath string) (total, free uint64, err error) {
	u, ok := s.backend.(storage.Usager)
	if !ok {
		return 0, 0, storage.ErrNotSupported
	}
	return u.Usage(ctx, util.CleanAPIPath(apiPath))
}

// ReadContent 完整读取可编辑文本（文件编辑与路径级容量优化）。
// 驱动未实现 ContentEditor 时返回 storage.ErrNotSupported；超限 / 非文本由驱动返回对应错误。
func (s *FileService) ReadContent(ctx context.Context, apiPath string) (*model.FileContentResult, error) {
	ed, ok := s.backend.(storage.ContentEditor)
	if !ok {
		return nil, storage.ErrNotSupported
	}
	return ed.ReadText(ctx, util.CleanAPIPath(apiPath), s.maxEdit)
}

// SaveContent 以乐观锁保存文本。对同一规范化路径加 path lock 串行化（与上传合并共享 locker，
// 避免「保存」与「上传合并」交错写）。驱动未实现 ContentEditor 时返回 storage.ErrNotSupported。
//
// 保存前对新内容做大小上限校验（与读取口径一致），避免绕过 ReadText 直接 PUT 超大内容。
func (s *FileService) SaveContent(ctx context.Context, apiPath string, content []byte, expected model.FileRevision, force bool) (*model.SaveContentResult, error) {
	ed, ok := s.backend.(storage.ContentEditor)
	if !ok {
		return nil, storage.ErrNotSupported
	}
	if s.maxEdit > 0 && int64(len(content)) > s.maxEdit {
		return nil, storage.ErrFileTooLarge
	}
	cleaned := util.CleanAPIPath(apiPath)
	s.locker.Lock(cleaned)
	defer s.locker.Unlock(cleaned)
	return ed.WriteText(ctx, cleaned, content, expected, force)
}

// Space 返回 apiPath 所在存储的容量信息（路径级容量）。
//
// 行为：路径必须真实存在（否则 ErrNotFound，不回退父目录）；驱动未实现 Usager 时
// 返回 supported=false 而非错误。free 与 available 同取 Usager 的 free（Bavail 语义），
// used = total - free。
func (s *FileService) Space(ctx context.Context, apiPath string) (*model.SpaceResult, error) {
	cleaned := util.CleanAPIPath(apiPath)

	// 路径级容量必须对应真实存在的目录 / 文件，不回退父目录。
	if _, err := s.backend.Stat(ctx, cleaned); err != nil {
		return nil, err
	}

	res := &model.SpaceResult{
		Path:  cleaned,
		Mount: model.MountInfo{Name: s.backend.Name(), Prefix: "/"},
	}

	u, ok := s.backend.(storage.Usager)
	if !ok {
		res.Space.Supported = false
		return res, nil
	}
	total, free, err := u.Usage(ctx, cleaned)
	if err != nil {
		// 路径存在但容量查询不被支持（如虚拟根）：降级为不支持，而非整次失败。
		if errors.Is(err, storage.ErrNotSupported) {
			res.Space.Supported = false
			return res, nil
		}
		return nil, err
	}

	var used uint64
	if total > free {
		used = total - free
	}
	var usedPercent float64
	if total > 0 {
		usedPercent = float64(used) / float64(total) * 100
	}
	res.Space = model.SpaceUsage{
		Supported:   true,
		Total:       total,
		Used:        used,
		Free:        free,
		Available:   free, // 首期 available 等同 free（Bavail 语义），见 spec
		UsedPercent: usedPercent,
	}
	return res, nil
}

// checkSpace 在复制前预检目标存储可用空间（仅当驱动实现 Usager）。
// 无法判断（不支持 Usager / 查询失败 / 计算源大小失败）时保守放行，交由实际操作处理。
func (s *FileService) checkSpace(ctx context.Context, src, dstDir string) error {
	u, ok := s.backend.(storage.Usager)
	if !ok {
		return nil
	}
	_, free, err := u.Usage(ctx, dstDir)
	if err != nil {
		return nil
	}
	need, err := s.treeSize(ctx, src)
	if err != nil {
		return nil
	}
	if need > free {
		return storage.ErrDiskFull
	}
	return nil
}

// CheckSpace 在复制前预检目标存储可用空间（导出版，供异步文件操作服务复用）。
func (s *FileService) CheckSpace(ctx context.Context, src, dstDir string) error {
	return s.checkSpace(ctx, util.CleanAPIPath(src), util.CleanAPIPath(dstDir))
}

// treeSize 计算 src 的总字节数：文件取自身大小；目录累加其下普通文件大小。
func (s *FileService) treeSize(ctx context.Context, src string) (uint64, error) {
	info, err := s.backend.Stat(ctx, src)
	if err != nil {
		return 0, err
	}
	if info.Type == model.TypeFile {
		return uint64(info.Size), nil
	}
	var total uint64
	err = s.walk(ctx, src, true, func(_ string, fi model.FileInfo) error {
		if fi.Type == model.TypeFile {
			total += uint64(fi.Size)
		}
		return nil
	})
	return total, err
}

// TreeSize 计算 src 的总字节数（导出版，供异步文件操作服务用于进度总量预估）。
func (s *FileService) TreeSize(ctx context.Context, src string) (uint64, error) {
	return s.treeSize(ctx, util.CleanAPIPath(src))
}

// Delete 批量递归删除，尽力而为，逐项返回结果。root 自身保护等由 backend.Remove 负责。
func (s *FileService) Delete(ctx context.Context, paths []string) []model.OpResult {
	results := make([]model.OpResult, 0, len(paths))
	for _, p := range paths {
		cleaned := util.CleanAPIPath(p)
		if err := s.backend.Remove(ctx, cleaned); err != nil {
			results = append(results, opFail(cleaned, err))
		} else {
			results = append(results, model.OpResult{Src: cleaned, OK: true})
		}
	}
	return results
}

// SearchOptions 控制搜索行为。
type SearchOptions struct {
	Recursive  bool
	ShowHidden bool
	Limit      int
}

// Search 按文件名匹配搜索。递归遍历优先用驱动的 Walker（高效），否则退化为基于
// List 的逐层递归。受 ctx 超时与命中上限保护。
func (s *FileService) Search(ctx context.Context, base, query string, opts SearchOptions) (*model.SearchResult, error) {
	cleaned := util.CleanAPIPath(base)

	limit := opts.Limit
	if limit < 1 {
		limit = defaultSearchLimit
	}
	if limit > maxSearchLimit {
		limit = maxSearchLimit
	}

	isGlob := strings.ContainsAny(query, "*?")
	lowerQuery := strings.ToLower(query)
	match := func(name string) bool {
		if isGlob {
			ok, err := path.Match(query, name)
			return err == nil && ok
		}
		return strings.Contains(strings.ToLower(name), lowerQuery)
	}

	res := &model.SearchResult{Query: query, Base: cleaned, Items: []model.SearchHit{}}

	visit := func(rel string, info model.FileInfo) error {
		if !match(info.Name) {
			return nil
		}
		if len(res.Items) >= limit {
			res.Truncated = true
			return storage.ErrStopWalk
		}
		res.Items = append(res.Items, makeHit(path.Join(cleaned, rel), info))
		return nil
	}

	if opts.Recursive {
		err := s.walk(ctx, cleaned, opts.ShowHidden, visit)
		switch {
		case errors.Is(err, storage.ErrStopWalk):
			return res, nil
		case errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
			res.TimedOut = true
			return res, nil
		case err != nil:
			return nil, err
		}
		return res, nil
	}

	// 非递归：仅匹配 base 目录下的直接子项。
	items, err := s.backend.List(ctx, cleaned, opts.ShowHidden)
	if err != nil {
		return nil, err
	}
	for _, it := range items {
		if !match(it.Name) {
			continue
		}
		if len(res.Items) >= limit {
			res.Truncated = true
			break
		}
		res.Items = append(res.Items, makeHit(path.Join(cleaned, it.Name), it))
	}
	return res, nil
}

// walk 递归遍历：驱动实现 Walker 则用之（高效），否则退化为基于 List 的逐层递归。
// 回调返回 storage.ErrStopWalk 或 ctx 取消时原样向上传播，由 Search 解释为截断 / 超时。
func (s *FileService) walk(ctx context.Context, root string, showHidden bool, fn func(string, model.FileInfo) error) error {
	if w, ok := s.backend.(storage.Walker); ok {
		return w.Walk(ctx, root, showHidden, fn)
	}
	return s.walkViaList(ctx, root, "", showHidden, fn)
}

// walkViaList 对不支持 Walker 的驱动的兜底递归实现。relPrefix 为相对搜索起点的路径前缀。
func (s *FileService) walkViaList(ctx context.Context, curPath, relPrefix string, showHidden bool, fn func(string, model.FileInfo) error) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	items, err := s.backend.List(ctx, curPath, showHidden)
	if err != nil {
		return err
	}
	for _, it := range items {
		rel := it.Name
		if relPrefix != "" {
			rel = relPrefix + "/" + it.Name
		}
		if err := fn(rel, it); err != nil {
			return err
		}
		if it.Type == model.TypeDir {
			child := path.Join(curPath, it.Name)
			if err := s.walkViaList(ctx, child, rel, showHidden, fn); err != nil {
				return err
			}
		}
	}
	return nil
}

// sortItems 按目录优先 + 指定键 + 升降序排序。
func sortItems(items []model.FileInfo, sortKey, order string) {
	desc := order == "desc"
	less := func(i, j int) bool {
		a, b := items[i], items[j]
		aDir := a.Type == model.TypeDir
		bDir := b.Type == model.TypeDir
		if aDir != bDir {
			return aDir // 目录优先，不受 order 影响
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

// makeHit 由 API 路径与条目信息组装搜索命中。
func makeHit(apiPath string, info model.FileInfo) model.SearchHit {
	return model.SearchHit{
		Path:    apiPath,
		Name:    info.Name,
		Type:    info.Type,
		Size:    info.Size,
		Mode:    info.Mode,
		ModTime: info.ModTime,
	}
}

// opFail 构造一条失败的批量操作结果。
func opFail(src string, err error) model.OpResult {
	return model.OpResult{Src: src, OK: false, Error: errCodeName(err)}
}

// errCodeName 将服务 / 驱动层错误映射为对外的错误码名（与 handler 错误码表对应）。
func errCodeName(err error) string {
	switch {
	case errors.Is(err, storage.ErrTraversal):
		return "path_traversal"
	case errors.Is(err, storage.ErrInvalidName):
		return "name_invalid"
	case errors.Is(err, storage.ErrNotFound):
		return "path_not_found"
	case errors.Is(err, storage.ErrForbidden):
		return "permission_denied"
	case errors.Is(err, storage.ErrExists):
		return "file_exists"
	case errors.Is(err, storage.ErrDiskFull):
		return "disk_full"
	case errors.Is(err, storage.ErrNotDir):
		return "not_a_dir"
	case errors.Is(err, storage.ErrNotFile):
		return "not_a_file"
	case errors.Is(err, storage.ErrNotSupported):
		return "not_supported"
	case errors.Is(err, storage.ErrBadOp):
		return "bad_request"
	default:
		return "internal_error"
	}
}

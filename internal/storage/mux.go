package storage

import (
	"context"
	"io"
	"path"
	"strings"
	"sync"
	"time"

	"flist/internal/model"
)

// Mount 是一个挂载点：把某个 Backend 挂到虚拟命名空间的一级目录 Name 下。
type Mount struct {
	Name    string  // 挂载点名（虚拟根下的一级目录名，如 "local"、"mybox"）
	Backend Backend // 该挂载点背后的驱动
}

// Mux 把多个 Backend 按挂载前缀组合成统一的虚拟命名空间，自身也实现 Backend：
//
//	/            → 虚拟根，List 返回各挂载点（呈现为一级目录）
//	/local/...   → 路由到名为 local 的挂载点，相对路径为 /...
//	/mybox/...   → 路由到名为 mybox 的挂载点
//
// 这样「本地 + 多个 WebDAV / 网盘并存」只是多注册几个 Mount，上层 service / handler
// 无需任何改动。单挂载且透明挂在根时无需 Mux，直接用对应 Backend 即可（见 main 装配）。
//
// 挂载点集合可在运行时动态增删（AddMount / RemoveMount），用于「设备管理」这类
// 挂载点随外接设备挂 / 卸而变化的场景。读路径（route / List / Walk 虚拟根遍历）加读锁，
// 增删加写锁；route 在锁内取出 Backend 引用后即释放锁，后端 I/O 不持锁进行。
type Mux struct {
	mu     sync.RWMutex
	mounts []Mount
	byName map[string]Backend
}

var _ Backend = (*Mux)(nil)
var _ Walker = (*Mux)(nil)
var _ Usager = (*Mux)(nil)
var _ ContentEditor = (*Mux)(nil)
var _ ProgressCopier = (*Mux)(nil)
var _ Uploader = (*Mux)(nil)

// NewMux 构造组合驱动。mounts 顺序决定虚拟根列表的展示顺序（可为 nil，构造空命名空间，
// 后续用 AddMount 动态注册，如设备 Mux）。
func NewMux(mounts []Mount) *Mux {
	byName := make(map[string]Backend, len(mounts))
	for _, m := range mounts {
		byName[m.Name] = m.Backend
	}
	return &Mux{mounts: mounts, byName: byName}
}

// AddMount 动态注册一个挂载点。同名已存在时返回 ErrExists。
func (m *Mux) AddMount(mt Mount) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.byName[mt.Name]; ok {
		return ErrExists
	}
	m.mounts = append(m.mounts, mt)
	m.byName[mt.Name] = mt.Backend
	return nil
}

// RemoveMount 移除挂载点（幂等，不存在返回 nil），返回被移除的 Backend（无则 nil），
// 供调用方在后续操作失败时回滚重挂。
func (m *Mux) RemoveMount(name string) Backend {
	m.mu.Lock()
	defer m.mu.Unlock()
	b, ok := m.byName[name]
	if !ok {
		return nil
	}
	delete(m.byName, name)
	for i, mt := range m.mounts {
		if mt.Name == name {
			m.mounts = append(m.mounts[:i], m.mounts[i+1:]...)
			break
		}
	}
	return b
}

// Mounts 返回当前挂载点名快照（顺序即展示顺序），用于诊断 / 对账。
func (m *Mux) Mounts() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	names := make([]string, len(m.mounts))
	for i, mt := range m.mounts {
		names[i] = mt.Name
	}
	return names
}

// snapshot 在读锁内复制当前挂载点切片，供虚拟根遍历 / 列举时避免持锁跨 I/O。
func (m *Mux) snapshot() []Mount {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]Mount, len(m.mounts))
	copy(out, m.mounts)
	return out
}

func (m *Mux) Name() string { return "mux" }

// Capabilities 取各挂载点能力的交集：只要有一个挂载点不支持某能力，
// 对外就保守地声明为不支持（具体能力仍由各挂载点在操作时精确决定）。
func (m *Mux) Capabilities() Caps {
	caps := Caps{Write: true, Copy: true, Upload: true, DiskUsage: true, Edit: true}
	for _, mt := range m.snapshot() {
		c := mt.Backend.Capabilities()
		caps.Write = caps.Write && c.Write
		caps.Copy = caps.Copy && c.Copy
		caps.Upload = caps.Upload && c.Upload
		caps.DiskUsage = caps.DiskUsage && c.DiskUsage
		caps.Edit = caps.Edit && c.Edit
	}
	return caps
}

// route 把虚拟 API 路径解析为 (挂载点名, 挂载点后端, 相对该后端的路径)。
// 虚拟根 "/" 返回 backend==nil，调用方需特殊处理。挂载点不存在返回 ErrNotFound。
func (m *Mux) route(p string) (name string, b Backend, rel string, err error) {
	cleaned := cleanVirtual(p)
	if cleaned == "/" {
		return "", nil, "/", nil
	}
	// 去掉前导 /，取第一段作为挂载点名，其余作为相对路径。
	rest := strings.TrimPrefix(cleaned, "/")
	idx := strings.IndexByte(rest, '/')
	if idx < 0 {
		name = rest
		rel = "/"
	} else {
		name = rest[:idx]
		rel = "/" + rest[idx+1:]
	}
	m.mu.RLock()
	b, ok := m.byName[name]
	m.mu.RUnlock()
	if !ok {
		return name, nil, rel, ErrNotFound
	}
	return name, b, rel, nil
}

// Stat 处理虚拟根 / 挂载点根 / 挂载点内路径三种情形。
func (m *Mux) Stat(ctx context.Context, p string) (*model.FileInfo, error) {
	name, b, rel, err := m.route(p)
	if err != nil {
		return nil, err
	}
	if b == nil {
		return virtualDir("/"), nil // 虚拟根
	}
	info, err := b.Stat(ctx, rel)
	if err != nil {
		return nil, err
	}
	if rel == "/" {
		// 挂载点根：对外名字用挂载点名而非后端的根名。
		info.Name = name
	}
	m.rewriteTarget(name, info)
	return info, nil
}

// List 处理虚拟根（列出挂载点）与挂载点内目录两种情形。
func (m *Mux) List(ctx context.Context, p string, showHidden bool) ([]model.FileInfo, error) {
	name, b, rel, err := m.route(p)
	if err != nil {
		return nil, err
	}
	if b == nil {
		// 虚拟根：每个挂载点呈现为一个一级目录。
		mounts := m.snapshot()
		items := make([]model.FileInfo, 0, len(mounts))
		for _, mt := range mounts {
			items = append(items, *virtualDir(mt.Name))
		}
		return items, nil
	}
	items, err := b.List(ctx, rel, showHidden)
	if err != nil {
		return nil, err
	}
	for i := range items {
		m.rewriteTarget(name, &items[i])
	}
	return items, nil
}

func (m *Mux) Open(ctx context.Context, p string) (File, *model.FileInfo, error) {
	name, b, rel, err := m.route(p)
	if err != nil {
		return nil, nil, err
	}
	if b == nil {
		return nil, nil, ErrNotFile // 虚拟根不是文件
	}
	f, info, err := b.Open(ctx, rel)
	if err != nil {
		return nil, nil, err
	}
	m.rewriteTarget(name, info)
	return f, info, nil
}

func (m *Mux) Mkdir(ctx context.Context, p string) error {
	_, b, rel, err := m.route(p)
	if err != nil {
		return err
	}
	if b == nil || rel == "/" {
		return ErrBadOp // 不能在虚拟根或挂载点层级建目录
	}
	return b.Mkdir(ctx, rel)
}

func (m *Mux) Create(ctx context.Context, p string) error {
	_, b, rel, err := m.route(p)
	if err != nil {
		return err
	}
	if b == nil || rel == "/" {
		return ErrBadOp
	}
	return b.Create(ctx, rel)
}

func (m *Mux) Remove(ctx context.Context, p string) error {
	_, b, rel, err := m.route(p)
	if err != nil {
		return err
	}
	if b == nil || rel == "/" {
		return ErrBadOp // 不能删除虚拟根或卸载挂载点
	}
	return b.Remove(ctx, rel)
}

// Move 移动 src 到 dst：同一挂载点内委托后端（同盘 rename 瞬时）；跨挂载点则
// 「流式复制 + 删源」（复制失败 / 取消时清理目标半成品，不删源）。
func (m *Mux) Move(ctx context.Context, src, dst string) error {
	return m.transfer(ctx, src, dst, true, nil)
}

// Copy 复制 src 到 dst：同一挂载点内委托后端；跨挂载点则跨后端流式递归复制。
func (m *Mux) Copy(ctx context.Context, src, dst string) error {
	return m.transfer(ctx, src, dst, false, nil)
}

// CopyWithProgress 实现 storage.ProgressCopier：带项内字节进度与取消的跨（含同）挂载复制。
func (m *Mux) CopyWithProgress(ctx context.Context, src, dst string, fn ProgressFunc) error {
	return m.transfer(ctx, src, dst, false, fn)
}

// MoveWithProgress 实现 storage.ProgressCopier：带项内字节进度与取消的跨（含同）挂载移动。
func (m *Mux) MoveWithProgress(ctx context.Context, src, dst string, fn ProgressFunc) error {
	return m.transfer(ctx, src, dst, true, fn)
}

// transfer 统一 copy/move 的路由与执行：同挂载点委托后端（可能实现 ProgressCopier），
// 跨挂载点走 crossCopy 流式复制；isMove 时复制成功后删源。
func (m *Mux) transfer(ctx context.Context, src, dst string, isMove bool, fn ProgressFunc) error {
	_, sb, sRel, err := m.route(src)
	if err != nil {
		return err
	}
	_, db, dRel, err := m.route(dst)
	if err != nil {
		return err
	}
	if sb == nil || db == nil || sRel == "/" {
		return ErrBadOp
	}

	// 同挂载点：委托后端。优先带进度接口，退化为普通 Copy/Move。
	if sb == db {
		if isMove {
			return moveOnBackend(ctx, sb, sRel, dRel, fn)
		}
		return copyOnBackend(ctx, sb, sRel, dRel, fn)
	}

	// 跨挂载点：跨后端流式递归复制。
	if err := m.crossCopy(ctx, sb, sRel, db, dRel, fn); err != nil {
		return err
	}
	if isMove {
		// 复制成功后删源。删源失败向上返回，但已复制的数据保留。
		return sb.Remove(ctx, sRel)
	}
	return nil
}

// copyOnBackend 在单个后端内复制，优先走 ProgressCopier。
func copyOnBackend(ctx context.Context, b Backend, src, dst string, fn ProgressFunc) error {
	if fn != nil {
		if pc, ok := b.(ProgressCopier); ok {
			return pc.CopyWithProgress(ctx, src, dst, fn)
		}
	}
	return b.Copy(ctx, src, dst)
}

// moveOnBackend 在单个后端内移动，优先走 ProgressCopier。
func moveOnBackend(ctx context.Context, b Backend, src, dst string, fn ProgressFunc) error {
	if fn != nil {
		if pc, ok := b.(ProgressCopier); ok {
			return pc.MoveWithProgress(ctx, src, dst, fn)
		}
	}
	return b.Move(ctx, src, dst)
}

// crossCopy 用两个 Backend 接口在虚拟层完成递归复制（不依赖底层驱动类型）。
// 目录递归建目录 + 逐项复制；普通文件走 crossCopyFile 流式写入。落点已存在由目标后端
// 的 Mkdir / OpenWrite 返回 ErrExists。ctx 取消时中止。
func (m *Mux) crossCopy(ctx context.Context, sb Backend, sRel string, db Backend, dRel string, fn ProgressFunc) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	info, err := sb.Stat(ctx, sRel)
	if err != nil {
		return err
	}
	if info.Type == model.TypeDir {
		if err := db.Mkdir(ctx, dRel); err != nil {
			return err
		}
		items, err := sb.List(ctx, sRel, true) // showHidden=true：复制要完整
		if err != nil {
			return err
		}
		for _, it := range items {
			if err := m.crossCopy(ctx,
				sb, path.Join(sRel, it.Name),
				db, path.Join(dRel, it.Name), fn); err != nil {
				return err
			}
		}
		return nil
	}
	return crossCopyFile(ctx, sb, sRel, db, dRel, fn)
}

// crossCopyFile 单文件跨后端流式写入：源 Open 读、目标 StreamWriter 写。
// 每个文件项 fn 从 0 计数（项内字节进度）。中途出错 / 取消由 streamWriter 清理半成品。
func crossCopyFile(ctx context.Context, sb Backend, sRel string, db Backend, dRel string, fn ProgressFunc) error {
	sw, ok := db.(StreamWriter)
	if !ok {
		return ErrNotSupported // 目标后端不支持流式写
	}
	rc, _, err := sb.Open(ctx, sRel)
	if err != nil {
		return err
	}
	defer rc.Close()
	wc, err := sw.OpenWrite(ctx, dRel)
	if err != nil {
		return err
	}
	var w io.Writer = wc
	if fn != nil {
		w = &muxProgressWriter{w: wc, ctx: ctx, fn: fn}
	}
	if _, err := io.Copy(w, rc); err != nil {
		wc.Close() // Close 负责清理半成品临时文件
		return err
	}
	return wc.Close()
}

// muxProgressWriter 包装目标 writer，按累计写入字节回调进度，并在 ctx 取消时中止 io.Copy。
// 与 util.progressWriter 语义一致，此处独立一份避免跨包耦合。
type muxProgressWriter struct {
	w   io.Writer
	ctx context.Context
	n   int64
	fn  ProgressFunc
}

func (p *muxProgressWriter) Write(b []byte) (int, error) {
	if err := p.ctx.Err(); err != nil {
		return 0, err
	}
	n, err := p.w.Write(b)
	p.n += int64(n)
	p.fn(p.n)
	return n, err
}

// Usage 路由到命中挂载点并委托其 Usage（storage.Usager）。
// 虚拟根 / 挂载点后端未实现 Usager 时返回 ErrNotSupported。
func (m *Mux) Usage(ctx context.Context, p string) (total, free uint64, err error) {
	_, b, rel, rerr := m.route(p)
	if rerr != nil {
		return 0, 0, rerr
	}
	if b == nil {
		return 0, 0, ErrNotSupported // 虚拟根无单一存储用量
	}
	u, ok := b.(Usager)
	if !ok {
		return 0, 0, ErrNotSupported
	}
	return u.Usage(ctx, rel)
}

// ReadText 路由到命中挂载点并委托其 ContentEditor.ReadText（storage.ContentEditor）。
// 返回结果的 Path 改写回虚拟命名空间。虚拟根 / 不支持的后端返回 ErrNotSupported。
func (m *Mux) ReadText(ctx context.Context, p string, maxBytes int64) (*model.FileContentResult, error) {
	name, b, rel, err := m.route(p)
	if err != nil {
		return nil, err
	}
	if b == nil {
		return nil, ErrNotFile // 虚拟根不是文件
	}
	ed, ok := b.(ContentEditor)
	if !ok {
		return nil, ErrNotSupported
	}
	res, err := ed.ReadText(ctx, rel, maxBytes)
	if err != nil {
		return nil, err
	}
	res.Path = joinVirtual(name, res.Path)
	return res, nil
}

// WriteText 路由到命中挂载点并委托其 ContentEditor.WriteText（storage.ContentEditor）。
// 返回结果的 Path 改写回虚拟命名空间。虚拟根 / 不支持的后端返回 ErrNotSupported。
func (m *Mux) WriteText(ctx context.Context, p string, content []byte, expected model.FileRevision, force bool) (*model.SaveContentResult, error) {
	name, b, rel, err := m.route(p)
	if err != nil {
		return nil, err
	}
	if b == nil || rel == "/" {
		return nil, ErrNotFile
	}
	ed, ok := b.(ContentEditor)
	if !ok {
		return nil, ErrNotSupported
	}
	res, err := ed.WriteText(ctx, rel, content, expected, force)
	if err != nil {
		return nil, err
	}
	res.Path = joinVirtual(name, res.Path)
	return res, nil
}

// stagingBackend 返回承载分片上传暂存的挂载点后端：取第一个实现 Uploader 的挂载点
//（挂载顺序即优先级，本地 files 后端排在最前，持有 DATA_DIR 下的全局暂存目录）。
//
// 分片暂存目录与落点 root 解耦，且 stage/abort/sweep 这类操作只有 uploadID、没有落点
// 路径信息，无法按路由分发，故统一委托给这一暂存后端。MergeUpload 有 dst，按路由分发，
// 并要求落点后端即暂存后端（读的是同一 staging），否则拒绝。
func (m *Mux) stagingBackend() Backend {
	for _, mt := range m.snapshot() {
		if _, ok := mt.Backend.(Uploader); ok {
			return mt.Backend
		}
	}
	return nil
}

// StageChunk 将分片写入暂存后端（storage.Uploader）。无暂存后端时返回 ErrNotSupported。
func (m *Mux) StageChunk(ctx context.Context, uploadID string, index int, r io.Reader) (int64, error) {
	b := m.stagingBackend()
	if b == nil {
		return 0, ErrNotSupported
	}
	return b.(Uploader).StageChunk(ctx, uploadID, index, r)
}

// MergeUpload 把暂存分片合并到 dst：路由到落点后端并委托其 MergeUpload（rel 为相对该后端根
// 的路径）。要求落点后端即暂存后端（同一 staging 目录，能读到 StageChunk 落下的分片），
// 否则返回 ErrNotSupported——如上传到不承载暂存的设备挂载点。
func (m *Mux) MergeUpload(ctx context.Context, uploadID, dst string, totalChunks int, overwrite bool) error {
	_, b, rel, err := m.route(dst)
	if err != nil {
		return err
	}
	if b == nil || rel == "/" {
		return ErrBadOp
	}
	u, ok := b.(Uploader)
	if !ok || b != m.stagingBackend() {
		return ErrNotSupported
	}
	return u.MergeUpload(ctx, uploadID, rel, totalChunks, overwrite)
}

// AbortChunk 删除暂存后端中单个分片的暂存文件（幂等）。
func (m *Mux) AbortChunk(uploadID string, index int) error {
	b := m.stagingBackend()
	if b == nil {
		return nil
	}
	return b.(Uploader).AbortChunk(uploadID, index)
}

// AbortUpload 删除暂存后端中某次上传的暂存区（幂等）。
func (m *Mux) AbortUpload(uploadID string) error {
	b := m.stagingBackend()
	if b == nil {
		return nil
	}
	return b.(Uploader).AbortUpload(uploadID)
}

// SweepStaging 委托暂存后端清理 mtime 超期的孤儿暂存目录，返回清理数量。
func (m *Mux) SweepStaging(maxAge time.Duration) (int, error) {
	b := m.stagingBackend()
	if b == nil {
		return 0, nil
	}
	return b.(Uploader).SweepStaging(maxAge)
}

// Walk 在虚拟根时遍历所有挂载点（relPath 以挂载点名为前缀）；在挂载点子树时
// 委托给对应后端（要求其实现 Walker），relPath 直接相对于搜索起点。
func (m *Mux) Walk(ctx context.Context, root string, showHidden bool, fn func(string, model.FileInfo) error) error {
	name, b, rel, err := m.route(root)
	if err != nil {
		return err
	}
	if b == nil {
		// 虚拟根：逐个挂载点遍历，relPath 前缀挂载点名。
		for _, mt := range m.snapshot() {
			if err := fn(mt.Name, *virtualDir(mt.Name)); err != nil {
				return err
			}
			w, ok := mt.Backend.(Walker)
			if !ok {
				continue // 不支持高效遍历的后端在虚拟根遍历时跳过其子树
			}
			prefix := mt.Name
			err := w.Walk(ctx, "/", showHidden, func(rp string, info model.FileInfo) error {
				if info.IsSymlink && info.SymlinkTarget != "" {
					info.SymlinkTarget = joinVirtual(mt.Name, info.SymlinkTarget)
				}
				return fn(path.Join(prefix, rp), info)
			})
			if err != nil {
				return err
			}
		}
		return nil
	}
	w, ok := b.(Walker)
	if !ok {
		return ErrNotSupported
	}
	return w.Walk(ctx, rel, showHidden, func(rp string, info model.FileInfo) error {
		if info.IsSymlink && info.SymlinkTarget != "" {
			info.SymlinkTarget = joinVirtual(name, info.SymlinkTarget)
		}
		return fn(rp, info)
	})
}

// rewriteTarget 把后端返回的（相对其自身根的）符号链接目标改写到虚拟命名空间。
func (m *Mux) rewriteTarget(mount string, info *model.FileInfo) {
	if info != nil && info.IsSymlink && info.SymlinkTarget != "" {
		info.SymlinkTarget = joinVirtual(mount, info.SymlinkTarget)
	}
}

// virtualDir 构造一个合成目录条目（虚拟根 / 挂载点）。
func virtualDir(name string) *model.FileInfo {
	return &model.FileInfo{
		Name:    name,
		Type:    model.TypeDir,
		Mode:    "0755",
		ModTime: time.Time{},
	}
}

// cleanVirtual 归一化虚拟 API 路径（与 util.CleanAPIPath 同义，避免循环依赖在此自洽实现）。
func cleanVirtual(p string) string {
	return path.Clean("/" + strings.TrimLeft(p, "/"))
}

// joinVirtual 把挂载点名与「相对该挂载点的 API 路径」拼成虚拟命名空间路径。
func joinVirtual(mount, rel string) string {
	return cleanVirtual("/" + mount + "/" + strings.TrimLeft(rel, "/"))
}

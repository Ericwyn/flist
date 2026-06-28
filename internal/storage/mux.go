package storage

import (
	"context"
	"path"
	"strings"
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
type Mux struct {
	mounts []Mount
	byName map[string]Backend
}

var _ Backend = (*Mux)(nil)
var _ Walker = (*Mux)(nil)
var _ Usager = (*Mux)(nil)
var _ ContentEditor = (*Mux)(nil)

// NewMux 构造组合驱动。mounts 顺序决定虚拟根列表的展示顺序。
func NewMux(mounts []Mount) *Mux {
	byName := make(map[string]Backend, len(mounts))
	for _, m := range mounts {
		byName[m.Name] = m.Backend
	}
	return &Mux{mounts: mounts, byName: byName}
}

func (m *Mux) Name() string { return "mux" }

// Capabilities 取各挂载点能力的交集：只要有一个挂载点不支持某能力，
// 对外就保守地声明为不支持（具体能力仍由各挂载点在操作时精确决定）。
func (m *Mux) Capabilities() Caps {
	caps := Caps{Write: true, Copy: true, Upload: true, DiskUsage: true, Edit: true}
	for _, mt := range m.mounts {
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
	b, ok := m.byName[name]
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
		items := make([]model.FileInfo, 0, len(m.mounts))
		for _, mt := range m.mounts {
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

// Move 仅支持同一挂载点内的移动；跨挂载点移动暂不支持（返回 ErrNotSupported），
// 留待后续以「流式复制 + 删除」实现（见 docs 驱动抽象设计）。
func (m *Mux) Move(ctx context.Context, src, dst string) error {
	sName, sb, sRel, err := m.route(src)
	if err != nil {
		return err
	}
	dName, db, dRel, err := m.route(dst)
	if err != nil {
		return err
	}
	if sb == nil || db == nil || sRel == "/" {
		return ErrBadOp
	}
	if sName != dName {
		return ErrNotSupported // 跨挂载点移动
	}
	return sb.Move(ctx, sRel, dRel)
}

// Copy 仅支持同一挂载点内的复制；跨挂载点复制暂不支持（留待后续流式实现）。
func (m *Mux) Copy(ctx context.Context, src, dst string) error {
	sName, sb, sRel, err := m.route(src)
	if err != nil {
		return err
	}
	dName, db, dRel, err := m.route(dst)
	if err != nil {
		return err
	}
	if sb == nil || db == nil || sRel == "/" {
		return ErrBadOp
	}
	if sName != dName {
		return ErrNotSupported
	}
	return sb.Copy(ctx, sRel, dRel)
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

// Walk 在虚拟根时遍历所有挂载点（relPath 以挂载点名为前缀）；在挂载点子树时
// 委托给对应后端（要求其实现 Walker），relPath 直接相对于搜索起点。
func (m *Mux) Walk(ctx context.Context, root string, showHidden bool, fn func(string, model.FileInfo) error) error {
	name, b, rel, err := m.route(root)
	if err != nil {
		return err
	}
	if b == nil {
		// 虚拟根：逐个挂载点遍历，relPath 前缀挂载点名。
		for _, mt := range m.mounts {
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

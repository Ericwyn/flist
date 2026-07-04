# 设备管理 实现细节

> 本文承接 `20260704-device-management-overview.md`，聚焦具体代码改造点、结构体、接口签名与迁移步骤。阅读前请先读 overview。

## 目录改动总览

```text
internal/
├── storage/
│   ├── mux.go                  改：加 RWMutex + AddMount/RemoveMount + 跨挂载 Copy/Move
│   └── mux_test.go             改：动态增删 + 跨挂载传输测试
├── service/
│   └── device/                 新增（Linux-only 逻辑，含 build tag）
│       ├── device.go           DeviceService 接口 + 通用类型
│       ├── device_linux.go     lsblk / udisksctl 实现
│       ├── device_other.go     非 Linux 空实现
│       └── device_linux_test.go
├── handler/
│   ├── device.go               新增：DeviceHandler
│   └── system.go               改：Info 增 DeviceManagement 字段
├── model/
│   ├── model.go                改：SystemInfo 增字段
│   └── device.go               新增：Device DTO
├── server/
│   └── routes.go               改：注册 /api/devices*，Deps 增 Devices
├── store/
│   ├── db.go                   改：加 schema_meta 表 + 收藏夹前缀迁移
│   └── bookmark.go             改：迁移辅助查询（可选）
└── cmd/flist/main.go           改：装配 rootMux(files+drive) + deviceMux

frontend/src/
├── types.ts                    改：Device / SystemInfo 类型
├── lib/api.ts                  改：api.devices.*
├── components/
│   ├── Sidebar.tsx             改：默认落地页 /files + 「设备管理」入口
│   └── DeviceManager.tsx       新增：设备管理页 / 弹窗
└── fsStore.ts                  改：默认 currentPath 从 / 改为 /files（视实现）
```

## 一、storage.Mux 改造

### 1.1 动态挂载点 + 并发安全

现状 `mounts` / `byName` 构造后只读、无锁。设备 Mux 需运行时增删，为其加读写锁。**注意**：顶层 rootMux 是静态的、不会增删，但为简化只维护一份 `Mux` 类型，统一加锁（读路径开销极小）。

```go
type Mux struct {
    mu     sync.RWMutex // 保护 mounts / byName 的并发增删与读取
    mounts []Mount
    byName map[string]Backend
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

// RemoveMount 移除挂载点（幂等，不存在返回 nil）。
func (m *Mux) RemoveMount(name string) error {
    m.mu.Lock()
    defer m.mu.Unlock()
    if _, ok := m.byName[name]; !ok {
        return nil
    }
    delete(m.byName, name)
    for i, mt := range m.mounts {
        if mt.Name == name {
            m.mounts = append(m.mounts[:i], m.mounts[i+1:]...)
            break
        }
    }
    return nil
}

// Mounts 返回当前挂载点名快照（用于诊断 / 设备服务对账）。
func (m *Mux) Mounts() []string { ... }
```

改造点：所有读方法（`route` / `List` / `Stat` / `Walk` 虚拟根遍历等）在读取 `mounts` / `byName` 前加 `m.mu.RLock()`。`route` 目前直接读 `m.byName`，改为在锁内取出 backend 引用后释放锁再执行后端操作（避免长时间持锁跨越 I/O）。

推荐模式：

```go
func (m *Mux) route(p string) (name string, b Backend, rel string, err error) {
    // ... 解析 name / rel ...
    m.mu.RLock()
    b, ok := m.byName[name]
    m.mu.RUnlock()
    if !ok {
        return name, nil, rel, ErrNotFound
    }
    return name, b, rel, nil
}
```

> 竞态注意：`RemoveMount` 与「正在进行的读操作」可能并发。因为 `route` 取出的是 `Backend` 引用（`*local.Local`），移除挂载点只是从 map 删除，不影响已取出引用的后端继续完成当次操作。卸载真正的物理不可用由 `udisksctl unmount` 决定，届时后端 I/O 自然报错并被 `mapErr` 归一化。

### 1.2 跨挂载点 Copy / Move

现状 `Mux.Copy/Move` 在 `sName != dName` 时返回 `ErrNotSupported`。改为：同挂载点走原逻辑，跨挂载点走新的流式实现。

```go
func (m *Mux) Copy(ctx context.Context, src, dst string) error {
    return m.transfer(ctx, src, dst, false, nil)
}
func (m *Mux) Move(ctx context.Context, src, dst string) error {
    return m.transfer(ctx, src, dst, true, nil)
}

// CopyWithProgress / MoveWithProgress 实现 storage.ProgressCopier，
// 使跨挂载点传输也能上报字节进度、支持取消（供 FileOpService 使用）。
func (m *Mux) CopyWithProgress(ctx context.Context, src, dst string, fn ProgressFunc) error {
    return m.transfer(ctx, src, dst, false, fn)
}
func (m *Mux) MoveWithProgress(ctx context.Context, src, dst string, fn ProgressFunc) error {
    return m.transfer(ctx, src, dst, true, fn)
}

func (m *Mux) transfer(ctx context.Context, src, dst string, isMove bool, fn ProgressFunc) error {
    sName, sb, sRel, err := m.route(src)
    if err != nil { return err }
    dName, db, dRel, err := m.route(dst)
    if err != nil { return err }
    if sb == nil || db == nil || sRel == "/" { return ErrBadOp }

    // 同挂载点：委托后端（同盘 rename 瞬时；驱动可能实现 ProgressCopier）。
    if sName == dName {
        if isMove { return moveOn(ctx, sb, sRel, dRel, fn) }
        return copyOn(ctx, sb, sRel, dRel, fn)
    }

    // 跨挂载点：流式复制 src(sb,sRel) -> dst(db,dRel)。
    if err := m.crossCopy(ctx, sb, sRel, db, dRel, fn); err != nil {
        return err
    }
    if isMove {
        // 复制成功后删源；删源失败不影响已复制的数据，返回错误由上层记录。
        return sb.Remove(ctx, sRel)
    }
    return nil
}
```

`var _ ProgressCopier = (*Mux)(nil)` 断言补上，`Capabilities().Copy` 已为各挂载点交集，保持不变。

#### crossCopy：跨后端递归流式复制

核心是「用两个 `Backend` 接口在虚拟层完成递归复制」，不依赖底层是不是同一种驱动：

```go
func (m *Mux) crossCopy(ctx context.Context, sb Backend, sRel string, db Backend, dRel string, fn ProgressFunc) error {
    info, err := sb.Stat(ctx, sRel)
    if err != nil { return err }

    if info.Type == model.TypeDir {
        // 目标建目录（已存在则 ErrExists，与同挂载语义一致：落点不得已存在）
        if err := db.Mkdir(ctx, dRel); err != nil { return err }
        items, err := sb.List(ctx, sRel, true) // showHidden=true：复制要完整
        if err != nil { return err }
        for _, it := range items {
            if err := ctx.Err(); err != nil { return err }
            if err := m.crossCopy(ctx,
                sb, path.Join(sRel, it.Name),
                db, path.Join(dRel, it.Name), fn); err != nil {
                return err
            }
        }
        return nil
    }

    // 普通文件：源 Open 流式 -> 目标写入。
    return m.crossCopyFile(ctx, sb, sRel, db, dRel, fn)
}
```

#### crossCopyFile：单文件跨后端写入

难点：目标 `Backend` 接口没有「写文件流」的方法（只有 `Create` 建空文件 + `Uploader` 分片）。方案分两种，取其一：

| 方案 | 说明 | 取舍 |
| --- | --- | --- |
| **B1（推荐）**：新增可选接口 `StreamWriter` | 给 `Backend` 加可选接口 `OpenWrite(ctx, p) (io.WriteCloser, error)`，local 实现为「同目录临时文件 + rename」。crossCopyFile 从 `sb.Open` 读、写入 `db.OpenWrite`，`io.Copy` 包 `progressWriter` 上报进度 + 取消 | 通用、干净；未来 WebDAV 也能实现；改动集中在接口 + local |
| B2：复用 `Uploader` | 把源文件当作单次上传走 `StageChunk`+`MergeUpload` | 语义绕、要造 uploadID、进度粒度差；不推荐 |

**采用 B1**。新增接口：

```go
// StreamWriter 是可选接口：驱动提供「打开一个可写入的目标文件流」，
// 供跨后端流式复制使用。返回的 WriteCloser 关闭时原子落地（同目录临时文件 + rename）。
// 落点已存在返回 ErrExists（与 Copy/Move 不覆盖语义一致）。
type StreamWriter interface {
    OpenWrite(ctx context.Context, p string) (io.WriteCloser, error)
}
```

local 实现要点（`internal/storage/local/content.go` 或新文件）：

- `SafeResolve` + `ValidateName` + 父目录存在性校验（复用现有 `resolve` 逻辑）。
- 落点已存在 → `ErrExists`。
- 写「同目录 `.tmp` 临时文件」，`Close()` 时 `rename` 到目标；写入中途出错 / ctx 取消 → 删临时文件（复用 `progressWriter` 的 ctx 检查思路）。

crossCopyFile：

```go
func (m *Mux) crossCopyFile(ctx context.Context, sb Backend, sRel string, db Backend, dRel string, fn ProgressFunc) error {
    sw, ok := db.(StreamWriter)
    if !ok { return ErrNotSupported } // 目标不支持流式写（当前 local 均支持）
    rc, _, err := sb.Open(ctx, sRel)
    if err != nil { return err }
    defer rc.Close()
    wc, err := sw.OpenWrite(ctx, dRel)
    if err != nil { return err }
    var w io.Writer = wc
    if fn != nil {
        w = &progressWriter{w: wc, ctx: ctx, fn: fn} // 复用既有 progressWriter 语义
    }
    if _, err := io.Copy(w, rc); err != nil {
        wc.Close() // Close 内部负责清理半成品临时文件
        return err
    }
    return wc.Close()
}
```

> 因 `progressWriter` 目前在 `internal/util`（非导出），跨包复用需要：要么在 storage 包内复制一份等价的小结构，要么把它提升为导出类型。倾向在 storage 包内放一个等价 `progressWriter`（与 util 的实现一致，几行代码，避免跨包耦合）。

### 1.3 测试（mux_test.go）

- `TestMuxAddRemoveMount`：增删后 `List("/")` / `route` 反映最新集合。
- `TestMuxConcurrentAddRemoveList`：并发增删 + 读，`go test -race` 无竞态。
- `TestMuxCrossCopyFile` / `TestMuxCrossCopyDir`：两个临时目录 local 后端间复制文件 / 目录树，内容与结构一致。
- `TestMuxCrossMove`：move 后源不存在、目标存在。
- `TestMuxCrossCopyExists`：目标已存在 → `ErrExists`。
- `TestMuxCrossCopyCancel`：`ctx` 取消 → 中止且目标半成品被清理、源保留（move 不删源）。

## 二、设备服务（internal/service/device）

### 2.1 通用类型与接口（device.go，无 build tag）

```go
package device

// Device 是一个块设备 / 分区的对外视图（对应 model.Device）。
type Device struct {
    Device     string // /dev/sdc1
    ID         string // 路径安全的挂载点名（sdc1）
    Name       string // 展示名
    Label      string // 卷标（可空）
    FSType     string // 文件系统类型（可空）
    Size       int64
    Mounted    bool
    Mountpoint string // 仅 Mounted 时有值
    Removable  bool
    Readonly   bool
}

// Service 抽象设备控制能力。Linux 有真实实现，其他平台为空实现。
type Service interface {
    Supported() bool
    List(ctx context.Context) ([]Device, error)
    Mount(ctx context.Context, device string) (*Device, error)
    Unmount(ctx context.Context, device string) (*Device, error)
}

var (
    ErrUnsupported = errors.New("device management not supported")
    ErrNotFound    = errors.New("device not found")
    ErrBusy        = errors.New("device busy")
    ErrInvalid     = errors.New("invalid device")
)
```

设备服务持有内层 `deviceMux` 引用，挂 / 卸时调用其 `AddMount` / `RemoveMount`：

```go
type linuxService struct {
    mux        *storage.Mux // deviceMux
    logger     *slog.Logger
    // 命令路径（探测缓存）
    lsblkPath  string
    udisksPath string
    mu         sync.Mutex // 串行化挂 / 卸，避免并发操作同设备
}
```

### 2.2 Linux 实现（device_linux.go）

**探测**（决定 `Supported()` 与 `device_management`）：`exec.LookPath("lsblk")` 且 `exec.LookPath("udisksctl")` 均成功。

**List**：

```go
// 用 -J 输出 JSON，-b 字节单位，-o 指定列。
// lsblk -J -b -o NAME,PATH,TYPE,SIZE,FSTYPE,LABEL,MOUNTPOINT,RM,RO
cmd := exec.CommandContext(ctx, s.lsblkPath, "-J", "-b",
    "-o", "NAME,PATH,TYPE,SIZE,FSTYPE,LABEL,MOUNTPOINT,RM,RO")
```

解析 JSON（`blockdevices` 树，含 `children`），筛选 `type == "part"`（分区）或有文件系统的 `disk`。每个条目：

- `ID` = 由 `NAME` 派生的路径安全名（见 2.4）。
- `Mounted` = `MOUNTPOINT != ""`。
- 已挂载但未在 deviceMux 注册的（如系统自动挂载）→ 惰性补注册。

**Mount**：

```go
func (s *linuxService) Mount(ctx context.Context, device string) (*Device, error) {
    s.mu.Lock()
    defer s.mu.Unlock()
    // 1. 重新 List 校验 device 存在且是分区（不信任入参）
    dev, err := s.findAndValidate(ctx, device)
    if err != nil { return nil, err }
    if dev.Mounted {
        // 已挂载：确保已注册进 mux，直接返回
        s.ensureRegistered(dev)
        return dev, nil
    }
    // 2. udisksctl mount -b <device>（分离参数，不走 shell）
    out, err := s.run(ctx, s.udisksPath, "mount", "-b", dev.Device)
    if err != nil { return nil, mapUdisksErr(err, out) }
    // 3. 解析挂载点（优先解析输出 "Mounted ... at /path."，兜底再 lsblk 查 MOUNTPOINT）
    mp := parseMountpoint(out)
    if mp == "" { mp = s.reReadMountpoint(ctx, dev.Device) }
    // 4. 注册进 deviceMux
    dev.Mounted = true
    dev.Mountpoint = mp
    if err := s.mux.AddMount(storage.Mount{
        Name:    dev.ID,
        Backend: local.New(mp, ""), // 设备后端不承载分片上传暂存，staging 传空
    }); err != nil && !errors.Is(err, storage.ErrExists) {
        return nil, err
    }
    return dev, nil
}
```

**Unmount**：先 `RemoveMount(id)`（防止卸载过程中被访问），再 `udisksctl unmount -b <device>`；若 unmount 失败（busy），回滚重新 `AddMount`。

```go
func (s *linuxService) Unmount(ctx context.Context, device string) (*Device, error) {
    s.mu.Lock(); defer s.mu.Unlock()
    dev, err := s.findAndValidate(ctx, device)
    if err != nil { return nil, err }
    prevBackend := s.detach(dev.ID)              // RemoveMount + 记住以便回滚
    out, uerr := s.run(ctx, s.udisksPath, "unmount", "-b", dev.Device)
    if uerr != nil {
        if prevBackend != nil {                   // 回滚
            _ = s.mux.AddMount(storage.Mount{Name: dev.ID, Backend: prevBackend})
        }
        return nil, mapUdisksErr(uerr, out)
    }
    dev.Mounted = false
    dev.Mountpoint = ""
    return dev, nil
}
```

**run 辅助**：`exec.CommandContext`，捕获 stdout/stderr，超时（如 30s）由 ctx 控制；debug 级别打印命令与输出。

**mapUdisksErr**：按 stderr 关键字归一化——含 `not authorized` / `AccessDenied` → 权限错误（提示配 polkit）；含 `target is busy` → `ErrBusy`；其余 → 通用命令失败。

### 2.3 非 Linux 空实现（device_other.go）

```go
//go:build !linux

func New(_ *storage.Mux, _ *slog.Logger) Service { return unsupported{} }

type unsupported struct{}
func (unsupported) Supported() bool { return false }
func (unsupported) List(context.Context) ([]Device, error) { return nil, ErrUnsupported }
func (unsupported) Mount(context.Context, string) (*Device, error) { return nil, ErrUnsupported }
func (unsupported) Unmount(context.Context, string) (*Device, error) { return nil, ErrUnsupported }
```

Linux 侧的 `New` 在 `device_linux.go`（`//go:build linux`）中做命令探测，探测失败也返回一个 `Supported()==false` 的实例（而非 nil），保证 handler 逻辑统一。

### 2.4 设备 ID 派生（路径安全）

`<id>` 用作 `/drive/<id>` 的路径段，必须无 `/`、无空格、无 `..`、非空。采用 kernel `NAME`（如 `sdc1`、`nvme0n1p2`、`mmcblk0p1`），它们天然是安全字符集 `[a-z0-9]`。仍加一层白名单校验兜底：

```go
func safeID(name string) (string, bool) {
    if name == "" || len(name) > 64 { return "", false }
    for _, c := range name {
        if (c>='a'&&c<='z')||(c>='A'&&c<='Z')||(c>='0'&&c<='9')||c=='-'||c=='_'||c=='.' {
            continue
        }
        return "", false
    }
    if name == "." || name == ".." { return "", false }
    return name, true
}
```

> 若后续采纳 PARTUUID（见 overview 待确认 §3），`safeID` 对 UUID 的连字符同样适用，仅需在 List 时改取该字段。

### 2.5 测试（device_linux_test.go）

解析逻辑与 exec 解耦：把「跑命令拿字节」抽成可注入的函数字段（或用一个 `run func(...) ([]byte, error)` 字段），测试注入固定 `lsblk -J` JSON 样本，断言 `[]Device` 解析结果。样本覆盖：分区 / 无 fstype 分区 / 已挂载 / 只读 / 含中文卷标 / 嵌套 children。`safeID` 单测覆盖非法字符拒绝。

## 三、handler 与路由

### 3.1 model.Device（model/device.go）

```go
type Device struct {
    Device     string `json:"device"`
    ID         string `json:"id"`
    Name       string `json:"name"`
    Label      string `json:"label"`
    FSType     string `json:"fstype"`
    Size       int64  `json:"size"`
    Mounted    bool   `json:"mounted"`
    Mountpoint string `json:"mountpoint"`
    DrivePath  string `json:"drive_path"` // /drive/<id>
    Removable  bool   `json:"removable"`
    Readonly   bool   `json:"readonly"`
}

type DeviceListResult struct {
    Supported bool     `json:"supported"`
    Devices   []Device `json:"devices"`
}
```

`DrivePath` 由 handler 拼 `"/drive/" + id`（前端「进入」直接用）。

### 3.2 DeviceHandler（handler/device.go）

```go
type DeviceHandler struct {
    svc    device.Service
    logger *slog.Logger
}

func (h *DeviceHandler) List(w, r)    // GET  /api/devices
func (h *DeviceHandler) Mount(w, r)   // POST /api/devices/mount   {device}
func (h *DeviceHandler) Unmount(w, r) // POST /api/devices/unmount {device}
```

错误映射（复用 `handler/errors.go` 风格，新增少量码）：

| device 错误 | HTTP / code | 提示 |
| --- | --- | --- |
| `ErrUnsupported` | 4000 / 不支持 | 「当前系统不支持设备管理」 |
| `ErrNotFound` | 404 / 2001 | 「设备不存在」 |
| `ErrBusy` | 409 / 新增 3001 | 「设备正忙，请先关闭正在使用它的程序」 |
| `ErrInvalid` | 400 / 4000 | 「非法设备」 |
| 权限（not authorized） | 403 / 2003 | 「无挂载权限，请检查 polkit 配置」 |

请求体校验：`device` 非空、以 `/dev/` 开头（初步校验，真正校验在 service 端重新 lsblk）。

### 3.3 路由（server/routes.go）

`Deps` 增 `Devices device.Service`。在受保护组内注册（写操作套写限流）：

```go
// 设备管理（Linux；不支持时接口内部返回不支持码）。列表只读，挂 / 卸为写操作。
protected.Get("/devices", deviceHandler.List)
protected.Group(func(wr chi.Router) {
    wr.Use(writeLimit)
    wr.Post("/devices/mount", deviceHandler.Mount)
    wr.Post("/devices/unmount", deviceHandler.Unmount)
})
```

### 3.4 system/info 扩展

`model.SystemInfo` 增 `DeviceManagement bool`；`SystemHandler` 需要能拿到 device.Service 判断 `Supported()`：

```go
type SystemHandler struct{ devices device.Service }
func NewSystemHandler(devices device.Service) *SystemHandler { ... }

func (h *SystemHandler) Info(w, r) {
    OK(w, model.SystemInfo{
        OS: runtime.GOOS, Arch: runtime.GOARCH, ServerTime: time.Now(),
        DeviceManagement: h.devices != nil && h.devices.Supported(),
    })
}
```

## 四、main 装配（cmd/flist/main.go）

`buildBackend` 改为构造分层 Mux，并返回 deviceMux 供设备服务使用：

```go
func buildBackend(cfg *config.Config, rootReal, stagingDir string, logger *slog.Logger) (storage.Backend, *storage.Mux, error) {
    filesBackend := local.New(rootReal, stagingDir)
    deviceMux := storage.NewMux(nil) // 空的动态设备命名空间

    rootMux := storage.NewMux([]storage.Mount{
        {Name: "files", Backend: filesBackend},
        {Name: "drive", Backend: deviceMux},
    })
    return rootMux, deviceMux, nil
}
```

`run` 中：

```go
backend, deviceMux, err := buildBackend(cfg, rootReal, stagingDir, logger)
...
deviceSvc := device.New(deviceMux, logger) // Linux 真实现 / 其他空实现
...
systemHandler := handler.NewSystemHandler(deviceSvc) // 注入
router, err := server.NewRouter(server.Deps{
    ..., Devices: deviceSvc,
})
```

`NewMux(nil)` 需容忍 nil（当前 `NewMux` 遍历 mounts，nil 切片安全；`byName` 用 `make` 初始化即可）。

> **能力交集副作用**：`rootMux.Capabilities()` 取 files 与 drive(deviceMux) 的交集。空 deviceMux 的 `Capabilities()` 目前返回全 `true`（无挂载点时交集初值即全开），OK。但要确认：当某个设备后端能力较弱时不会误关全局能力——local 全能力，故设备挂载不降级 files 的能力。保持现状即可。

## 五、收藏夹迁移（A1 路径前缀）

### 5.1 迁移目标

存量 `bookmarks.path` 形如 `/photos/2024`，需变为 `/files/photos/2024`。要求：**幂等**（重复启动不重复加前缀）、**可逆**（回滚脚本能去前缀）、**安全**（不误伤已是 `/files` 或 `/drive` 前缀的路径）。

### 5.2 引入 schema_meta 版本标记

当前 DB 无版本表。新增一张 KV 表记录已应用的迁移，避免用「路径是否已有前缀」这种脆弱判断：

```sql
CREATE TABLE IF NOT EXISTS schema_meta (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);
```

迁移在 `store.migrate()` 末尾按序执行，键如 `migration_bookmark_files_prefix = "1"`。

### 5.3 迁移逻辑（store/db.go）

```go
func (s *Store) migrateBookmarkFilesPrefix() error {
    const key = "migration_bookmark_files_prefix"
    if s.metaGet(key) == "1" {
        return nil // 已迁移，幂等跳过
    }
    tx, _ := s.db.Begin()
    defer tx.Rollback()
    // 给所有不以 /files 或 /drive 开头的收藏加 /files 前缀。
    // 根 "/" 收藏（若有）→ "/files"。
    _, err := tx.Exec(`
        UPDATE bookmarks
        SET path = CASE WHEN path = '/' THEN '/files'
                        ELSE '/files' || path END
        WHERE path NOT LIKE '/files%' AND path NOT LIKE '/drive%'`)
    if err != nil { return err }
    if _, err := tx.Exec(
        `INSERT OR REPLACE INTO schema_meta(key,value) VALUES(?,?)`, key, "1"); err != nil {
        return err
    }
    return tx.Commit()
}
```

> 唯一索引 `(user_id, path)` 在加前缀后仍唯一（前缀对所有行一致，不产生新冲突）。

### 5.4 可逆（回滚脚本 / 反向迁移）

回滚代码版本后，若需还原收藏路径，提供一条反向 SQL（写入 README 或运维脚本，不随程序自动执行）：

```sql
UPDATE bookmarks SET path = CASE WHEN path='/files' THEN '/'
                                 ELSE substr(path, length('/files')+1) END
WHERE path LIKE '/files%';
DELETE FROM schema_meta WHERE key='migration_bookmark_files_prefix';
```

> `/drive/*` 收藏不反向处理（旧版本无此概念，回滚后这些收藏本就无效，用户可手动删）。

## 六、前端改造

### 6.1 类型（types.ts）

```ts
export interface Device {
  device: string; id: string; name: string; label: string;
  fstype: string; size: number; mounted: boolean; mountpoint: string;
  drivePath: string; removable: boolean; readonly: boolean;
}
export interface SystemInfo {
  os: string; arch: string; serverTime: string;
  deviceManagement: boolean;
}
```

### 6.2 API（lib/api.ts）

```ts
devices: {
  list(): Promise<{ supported: boolean; devices: Device[] }>,
  mount(device: string): Promise<Device>,
  unmount(device: string): Promise<Device>,
}
```

字段 snake_case → camelCase 映射沿用现有 `mapEntry` 风格（`drive_path` → `drivePath`）。

### 6.3 默认落地页与导航

- `Sidebar` 「我的文件」按钮：`navigate('/')` → `navigate('/files')`；`atRoot` 判断相应改为 `currentPath === '/files'`。
- 应用初始 `currentPath`：`fsStore` 默认值从 `/` 改为 `/files`（确认无其他地方硬编码 `/` 作为初始路径）。
- 顶层 `/` 仍可访问（面包屑回退时会经过），展示 `files` / `drive` 两个入口目录。

### 6.4 设备管理入口与页面

`Sidebar` 导航区「我的文件」下方加按钮（仅当 `systemInfo.deviceManagement` 为真时渲染）：

```tsx
{deviceManagement && (
  <button onClick={() => setDeviceManagerOpen(true)}>
    <HardDrive .../> 设备管理
  </button>
)}
```

`DeviceManager.tsx`（弹窗或独立面板）：

- 加载时 `api.devices.list()`，渲染设备卡片列表：图标（`removable` 区分 U 盘 / 硬盘）、名称、卷标、大小（`formatBytes`）、fstype、挂载状态。
- 操作按钮：
  - 未挂载 → 「挂载」：`api.devices.mount(device)` → 成功后刷新列表。
  - 已挂载 → 「进入」（`navigate(dev.drivePath)` 并关闭弹窗）、「卸载」（`api.devices.unmount` → 刷新）。
- loading / error 态；busy 卸载失败弹提示。
- 顶部「刷新」按钮重新拉列表（对应非目标：不做实时热插拔推送）。

`systemInfo` 的获取：应用启动或首次进设备管理时调 `/api/system/info` 读 `deviceManagement`，存入某个 store（可放 `store.ts`）。

## 七、实施顺序（细化自 overview）

1. **storage.Mux**：`sync.RWMutex` + `AddMount`/`RemoveMount`/`Mounts`；读路径加锁。→ 测试。
2. **storage 流式写**：`StreamWriter` 接口 + local `OpenWrite`（临时文件 + rename + 清理）。→ 测试。
3. **storage.Mux 跨挂载**：`transfer`/`crossCopy`/`crossCopyFile` + `ProgressCopier` 实现。→ 测试（含 -race、取消、不删源）。
4. **device 服务**：类型 + 接口（device.go）、Linux 实现（device_linux.go）、空实现（device_other.go）、解析测试。
5. **model**：`Device`/`DeviceListResult`、`SystemInfo` 加字段。
6. **handler**：`DeviceHandler`、`system.Info` 注入 device.Service。错误码补 3001（busy）。
7. **routes**：`Deps.Devices` + 注册路由。
8. **main**：分层 Mux 装配 + deviceMux 注入设备服务 + systemHandler 注入。
9. **store**：`schema_meta` 表 + 收藏夹前缀迁移（幂等 + meta 标记）。
10. **前端**：types → api → systemInfo 读取 → Sidebar（落地页 + 入口）→ DeviceManager 组件。
11. `make build` → `go test ./... -race` → 真机手动验证（含 supervisor 无会话下的 polkit）。

## 八、编码前敲定的点（已确认）

1. **设备 ID 取值** → **kernel name**（如 `sdc1`）。`List` 取 `lsblk` 的 `NAME` 字段，`safeID` 白名单校验兜底。
2. **收藏夹迁移** → 接受一次性加 `/files` 前缀，幂等 + `schema_meta` 标记 + 附反向回滚 SQL（§5）。
3. **polkit 授权**（阻塞部署，非阻塞编码）：先在目标机验证无登录会话下 `udisksctl mount` 是否被拒；若拒，落一条 polkit 规则放行 flist 运行用户的 `org.freedesktop.udisks2.filesystem-mount*`。不影响代码编写，仅影响真机验证。

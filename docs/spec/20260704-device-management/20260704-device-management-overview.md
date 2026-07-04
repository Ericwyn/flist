# 设备管理（可移动存储挂载）方案概述

## 背景

flist 目前只能管理启动时通过 `--root` 指定的单一目录树。但在 Linux NAS / VPS 场景下，用户经常需要临时接入 U 盘、移动硬盘、SD 卡等可移动存储，把文件在「常驻数据盘」与「外接设备」之间搬运。

现状下用户只能 SSH 登录服务器手动敲命令：

```bash
lsblk                                  # 查看块设备与分区
udisksctl mount   -b /dev/sdc1         # 挂载某分区
udisksctl unmount -b /dev/sdc1         # 卸载某分区
```

挂载后得到一个类似 `/run/media/<user>/<LABEL>` 的目录，但这个目录在 `--root` 之外，flist 的 Web UI 根本看不到，用户还是得回到命令行操作。整个体验割裂：**Web 管文件，SSH 管设备**。

如果能在 Web UI 里直接列出设备、一键挂载 / 卸载、并像浏览普通目录一样进入设备内容、在设备与常驻盘之间互传文件，就能把「设备管理 + 文件管理」统一到一个界面，免去 SSH。

## 当前现状

### 存储层：已预留多挂载点抽象

项目在 `4a166b1` 就把文件存取抽象成了 `storage.Backend` 驱动接口，并提供了组合驱动 `storage.Mux`，这正是本次功能的核心接入点。

```text
main.buildBackend
  -> local.New(rootReal, stagingDir)     当前：单个本地驱动透明挂在根
  （注释已写明扩展形态）
  -> storage.NewMux([]Mount{...})        目标：多挂载点组合成虚拟命名空间
```

关键事实（均已阅代码确认）：

| 事实 | 位置 | 意义 |
| --- | --- | --- |
| `Mux` 自身实现 `Backend`，可嵌套 | `internal/storage/mux.go` | 支持「Mux 套 Mux」实现分层命名空间 |
| 挂载点是虚拟根下的一级目录 | `Mux.route` / `Mux.List` | `/local/...`、`/drive/...` 天然成立 |
| 上层 service / handler 路径无关 | `internal/service/file.go` | 接入新挂载点后，浏览 / 上传 / 编辑 / 打包全自动可用 |
| 跨挂载点 copy/move 被显式拒绝 | `Mux.Copy` / `Mux.Move` 返回 `ErrNotSupported` | 注释已写明「留待后续以流式复制 + 删除实现」 |
| 本地驱动已支持带进度 / 可取消的复制 | `local.CopyWithProgress`、`util.CopyPathWithProgress` | 跨挂载流式传输可直接复用 |
| 跨分区 move 已用「复制 + 删源」实现 | `util.movePath`（EXDEV 回退） | 跨挂载 move 语义与之同构 |
| 容量查询按路径路由到命中挂载点 | `Mux.Usage` -> 各后端 `Usager` | 设备容量显示零成本复用 |

### 文件操作链路

同步批量操作（`FileService.Copy/Move`）与异步后台任务（`FileOpService`，`84ee10e` 引入，SSE 进度 + 取消）都只面向 `Backend` 接口，且已导出 `StatDir` / `TransferTarget` / `CheckSpace` / `TreeSize` 供异步路径复用。这意味着：**只要 Mux 支持跨挂载点 copy/move，同步与异步两条链路都自动获得跨设备传输能力，无需各自改造。**

### 路径与前端导航

- 后端所有 `/api/fs/*` 接口入参都是「虚拟 API 路径」（`/` 开头），经 `util.CleanAPIPath` 归一化。
- 前端 `fsStore.navigate(path)` 驱动目录浏览，`Sidebar` 的「我的文件」按钮固定 `navigate('/')`。
- 收藏夹（`bookmarks` 表）持久化的是**绝对 API 路径**字符串（`internal/store/bookmark.go`）。

### 系统信息接口

`GET /api/system/info` 目前只返回 `OS / Arch / ServerTime`（`internal/handler/system.go`）。前端据此可做平台相关的能力开关。

### 当前问题点

1. 外接设备目录在 `--root` 之外，Web UI 完全不可见；
2. 挂载 / 卸载只能 SSH 手动执行；
3. 跨挂载点（未来的设备 ↔ 常驻盘）传输被 `Mux` 直接拒绝，能力缺失。

## 目标

1. **路径分层**：普通文件迁移到 `/files/` 前缀下；新增 `/drive/` 前缀作为设备总入口（`/drive/<id>/...` 为某设备内容）。
2. **设备列举**：Web UI 可列出当前系统的块设备 / 分区，展示名称、大小、文件系统类型、卷标、挂载状态、挂载点。
3. **挂载 / 卸载**：提供挂载、卸载（弹出）操作，底层调用 `udisksctl`。
4. **进入设备**：已挂载设备可像普通目录一样进入浏览（`navigate('/drive/<id>/')`），复用现有全部文件浏览 / 预览 / 编辑能力。
5. **跨挂载传输**：补齐 `Mux` 的跨挂载点 copy 与 move（move = 流式复制 + 删源），使 `/files/ ↔ /drive/<id>/` 可互相复制 / 移动，并复用异步任务的进度 / 取消。
6. **跨平台降级**：非 Linux（Windows）或 udisks 不可用时，能力开关关闭，前端隐藏「设备管理」入口，文件浏览不受影响。
7. **侧边栏入口**：在「我的文件」下方新增「设备管理」入口。

## 非目标

1. **不做磁盘分区 / 格式化 / 建文件系统**（`mkfs`、`fdisk` 等破坏性操作），只做挂载 / 卸载。
2. **不做设备的持久化 fstab 配置**，仅运行时挂载（重启后需重新挂载，与 `udisksctl` 语义一致）。
3. **不支持网络存储（NFS/SMB/WebDAV）挂载**，本期仅本地块设备；WebDAV 走既有 `Backend` 扩展点，另行规划。
4. **不做设备热插拔的实时推送**（不监听 udev 事件主动 push），前端靠「进入设备管理页 / 手动刷新」拉取最新设备列表。
5. **不改多用户模型**：flist 当前单管理员，设备操作即管理员操作，不引入按用户的设备权限。
6. **不做 RAID / LVM / 加密卷（LUKS）解锁**等高级存储管理。
7. **不改造删除的字节级进度**（沿用 `FileOpService` 既有策略）。

## 核心设计

### 总体原则

设备管理拆成正交的两层，各自独立、互不耦合：

1. **设备控制层**（新增，Linux-only）：`lsblk` 列举 + `udisksctl` 挂 / 卸，维护「设备 → 挂载点目录」映射。它只关心「设备现在挂在哪个 OS 目录」。
2. **存储接入层**（复用 `Mux`）：把每个已挂载设备的 OS 目录包成一个 `local.Backend`，动态注册进一个内层「设备 Mux」。文件操作全部走既有链路。

两层通过一个动态可增删挂载点的内层 Mux 衔接：**挂载设备 = 往设备 Mux 注册一个 mount；卸载设备 = 移除该 mount。**

### 路径布局：Mux 套 Mux（分层命名空间）

利用 `Mux` 自身也是 `Backend`、可嵌套的特性，构造两层命名空间：

```text
/                              顶层 Mux（rootMux，静态：两个固定挂载点）
├── /files/  → local.New(cfg.Root, staging)      普通文件（原 --root）
└── /drive/  → deviceMux（内层 Mux，动态增删）    设备总入口
      ├── /drive/<id>/  → local.New(/run/media/<user>/<LABEL>, "")   已挂载设备 A
      └── /drive/<id>/  → local.New(...)                             已挂载设备 B
```

- `/drive/` 点进去 = 内层 deviceMux 的虚拟根，`List` 自动返回当前所有已挂载设备（呈现为一级目录）。
- `/drive/<id>/` = 进入该设备内容，完全是普通本地目录浏览。
- 动态性被**完全隔离在内层 deviceMux**：挂 / 卸只增删内层 mount，顶层 rootMux 结构永远不变。

`<id>` 用**稳定且路径安全**的设备标识（详见 implementation，倾向用分区的 kernel name 如 `sdc1`，或 `PARTUUID` 派生的安全短名），避免用可能含空格 / 特殊字符的卷标做路径段。

### 挂载点动态增删的并发安全

现有 `Mux` 的 `mounts` / `byName` 在构造后只读，无锁。设备 Mux 需要运行时增删，故为内层 Mux 增加读写锁与 `AddMount` / `RemoveMount` 方法；读路径（route/List/Stat…）加读锁。顶层 rootMux 仍是静态构造，不受影响。

### 跨挂载点传输：补齐 Mux.Copy / Move

当前 `Mux.Copy/Move` 在 `sName != dName` 时直接 `ErrNotSupported`。补齐方案：

| 场景 | 实现 |
| --- | --- |
| 同挂载点 copy/move | 保持不变（委托后端，同盘 rename 瞬时） |
| 跨挂载点 copy | 从源后端 `Open()` 流式读、写入目标后端；目录递归。走 `ProgressCopier` 回调进度、`ctx` 可取消 |
| 跨挂载点 move | = 跨挂载点 copy + 源删除（`Remove`）。复制失败 / 取消则清理半成品，不删源 |

关键点：`/files/` 与 `/drive/<id>/` 背后都是**同机本地盘**，跨挂载传输本质是本地跨目录 / 跨分区拷贝，与 `util.movePath` 处理 EXDEV 的「复制 + 删源」完全同构，不涉及网络与协议差异，实现直接。补齐后同步 (`FileService`) 与异步 (`FileOpService`) 两条链路自动受益。

### 方案取舍

| 决策点 | 选择 | 理由 |
| --- | --- | --- |
| 路径布局 | A1：`/files/` + `/drive/`（用户确认） | 直接复用已测试的 `Mux`，仅需一次收藏夹迁移；语义清晰 |
| 分层实现 | Mux 套 Mux | 动态性隔离在内层，顶层静态不变；无需新写自定义路由 |
| 设备控制 | 外部命令 `lsblk -J` + `udisksctl` | 复用发行版成熟工具，无需自己解析 `/sys`、调 mount(2)、管 polkit |
| 设备标识 `<id>` | 分区 kernel name（如 `sdc1`） | 稳定、路径安全、用户在 `lsblk` 里能对上号 |
| 跨挂载 move | 复制 + 删源（用户确认） | 与既有 EXDEV 回退同构，复用 `ProgressCopier` |
| 平台隔离 | build tag（`_linux.go` / `_windows.go`） | 沿用项目既有跨平台模式（`disk_*`、`fsop_*`、`name_*`） |
| 能力上报 | `/api/system/info` 增 `device_management` 布尔 | 前端据此显隐入口，与既有平台判断一致 |
| 设备操作鉴权 | 复用现有认证 + 写限流 | 单管理员模型，无需新权限体系 |

### 替代方案不选原因

| 方案 | 不选原因 |
| --- | --- |
| A2：普通文件留在 `/`，`/drive/` 做保留前缀 | 需新写自定义根路由特判 `/drive/`，且用户根目录不能有真实 `drive` 文件夹；用户已选 A1 |
| 直接调 mount(2) 系统调用 | 需 root / CAP_SYS_ADMIN，且要自己管理挂载点目录、文件系统探测、权限；udisks 已封装好这些 |
| 监听 udev 实时热插拔推送 | 复杂度高，收益有限；手动 / 进页刷新已够用（非目标） |
| 用卷标 LABEL 做路径 `<id>` | 卷标可能含空格 / 中文 / 特殊字符 / 重复，做路径段不安全 |

## 关键流程

### 挂载设备

```text
前端「设备管理」页点「挂载」
  -> POST /api/devices/mount {device: "/dev/sdc1"}
  -> DeviceHandler.Mount
    -> DeviceService.Mount("/dev/sdc1")
       -> 重新 lsblk 校验该设备存在且为分区（不信任客户端传值）
       -> exec udisksctl mount -b /dev/sdc1   (分离参数，不走 shell)
       -> 解析输出得挂载点目录 /run/media/<user>/<LABEL>
       -> deviceMux.AddMount(Mount{Name: id, Backend: local.New(mountpoint, "")})
    -> 返回设备最新状态（含 /drive/<id> 虚拟路径）
  -> 前端刷新设备列表；用户可点「进入」→ navigate('/drive/<id>/')
```

### 卸载设备

```text
点「卸载 / 弹出」
  -> POST /api/devices/unmount {device: "/dev/sdc1"}
  -> DeviceService.Unmount
     -> deviceMux.RemoveMount(id)          先摘掉虚拟挂载点，避免卸载中被访问
     -> exec udisksctl unmount -b /dev/sdc1
  -> 返回最新状态
```

### 跨设备传输（复用现有异步任务）

```text
用户在 /files/ 选中文件，粘贴 / 拖到 /drive/sdc1/
  -> POST /api/fs/op/copy  (或 move)   （既有异步接口，无需新增）
  -> FileOpService -> Mux.CopyWithProgress
     -> 跨挂载点分支：源 Open 流式 -> 目标写入，进度回调 -> SSE
  -> 传输面板实时进度 / 可取消（既有能力）
```

## 数据结构 / 协议设计

### 新增接口

| 方法 | 路径 | 鉴权 | 说明 |
| --- | --- | --- | --- |
| GET | `/api/devices` | 认证 | 列出块设备 / 分区及挂载状态 |
| POST | `/api/devices/mount` | 认证 + 写限流 | 挂载指定分区 |
| POST | `/api/devices/unmount` | 认证 + 写限流 | 卸载指定分区 |

非 Linux / udisks 不可用时，这些接口返回统一错误码（能力不支持），前端本就隐藏入口不会调用。

### 设备条目（`GET /api/devices` 响应）

```json
{
  "code": 0,
  "message": "ok",
  "data": {
    "supported": true,
    "devices": [
      {
        "device": "/dev/sdc1",
        "id": "sdc1",
        "name": "sdc1",
        "label": "KINGSTON",
        "fstype": "exfat",
        "size": 61530439680,
        "mounted": true,
        "mountpoint": "/run/media/flist/KINGSTON",
        "drive_path": "/drive/sdc1",
        "removable": true,
        "readonly": false
      }
    ]
  }
}
```

字段说明：

| 字段 | 含义 | 备注 |
| --- | --- | --- |
| `device` | 分区块设备路径 | 操作时的稳定标识，服务端会重新校验 |
| `id` | 虚拟命名空间挂载点名 | 路径安全，`/drive/<id>` |
| `label` | 卷标 | 可能为空，仅展示用 |
| `fstype` | 文件系统类型 | 空表示未知 / 无文件系统 |
| `mounted` | 是否已挂载 | 决定「挂载」还是「卸载」按钮 |
| `mountpoint` | OS 挂载目录 | 仅 `mounted=true` 时有值 |
| `drive_path` | 虚拟浏览路径 | 前端「进入」直接 `navigate` 此值 |
| `removable` | 是否可移动设备 | 展示图标区分 |

### 挂载 / 卸载请求与响应

```json
// 请求
{ "device": "/dev/sdc1" }

// 响应 data：更新后的单个设备条目（结构同上）
```

### `/api/system/info` 扩展

```go
type SystemInfo struct {
    OS               string    `json:"os"`
    Arch             string    `json:"arch"`
    ServerTime       time.Time `json:"server_time"`
    DeviceManagement bool      `json:"device_management"` // 新增：设备管理是否可用
}
```

`DeviceManagement` 由后端启动时探测（Linux + `lsblk`/`udisksctl` 存在）决定。

## 兼容与降级

| 场景 | 处理方式 |
| --- | --- |
| 现有收藏夹绝对路径（`/foo/bar`） | 启动时一次性迁移：`bookmarks` 表所有 path 加 `/files` 前缀（详见 implementation 迁移步骤，幂等） |
| 前端默认落地页 | 「我的文件」按钮从 `navigate('/')` 改为 `navigate('/files')`；顶层 `/` 仍可访问（列出 files / drive 两项） |
| 非 Linux（Windows） | build tag：`DeviceService` 空实现，`device_management=false`，接口返回不支持；前端隐藏入口 |
| `lsblk` / `udisksctl` 未安装 | 启动探测失败 → `device_management=false`，同上降级 |
| 客户端传伪造 device 路径 | 服务端重新 `lsblk` 校验设备存在且为分区，`exec.Command` 分离参数防注入，拒绝非法值 |
| 设备已被系统自动挂载 | `lsblk` 读到 `mountpoint`，`mounted=true`；进入设备管理时按现状注册进 deviceMux |
| 卸载时设备正忙（有打开的文件） | `udisksctl` 返回 busy 错误，原样映射为可读错误提示，虚拟挂载点回滚（重新 AddMount） |
| 跨挂载 move 复制中途失败 / 取消 | 清理目标半成品，**不删源**（与 `util.movePath` EXDEV 回退一致） |
| 服务重启 | deviceMux 内存态清空；`udisksctl` 挂载的设备可能仍挂在 OS 上，重进设备管理页会重新 `lsblk` 感知并可再次「进入」 |
| 顶层 `/` 的容量查询 | rootMux 虚拟根无单一存储用量，`Usage` 返回 `ErrNotSupported`，前端容量条静默隐藏（既有降级逻辑） |

## 可观测性

1. 结构化日志（slog）：
   - 挂载成功：`logger.Info("device mounted", device, id, mountpoint)`
   - 卸载成功：`logger.Info("device unmounted", device, id)`
   - 命令失败：`logger.Warn("udisksctl failed", device, op, stderr)`
2. `device_management` 探测结果启动时 `logger.Info` 记录（便于排查为何前端无入口）。
3. 不记录设备内文件内容；`udisksctl` 输出仅在 debug 级别打印。

## 测试计划

### 单元测试

1. **Mux 动态增删**（`mux_test.go`）：`AddMount` / `RemoveMount` 后 `List` / `route` 正确，并发读写无竞态（`go test -race`）。
2. **跨挂载点 copy/move**（`mux_test.go`）：两个内存 / 临时目录后端间复制单文件、目录树、move 后源被删、复制失败不删源、`ctx` 取消中止并清理半成品。
3. **设备输出解析**（`device_linux_test.go`）：喂入固定 `lsblk -J` JSON 样本，验证设备条目解析（含无文件系统分区、已挂载、只读、卷标含特殊字符等 case）。
4. **id 生成安全性**：验证由设备名派生的 `<id>` 恒为路径安全字符（无 `/`、空格、`..`）。
5. **平台降级**（`device_windows` 编译）：Windows build 下 `device_management=false`，接口返回不支持。

### 集成 / 手动验证（需真实 Linux + 一块可移动设备）

1. 插入 U 盘 → 进设备管理 → 列表出现该分区，`mounted=false`。
2. 点「挂载」→ 成功，出现挂载点 → 点「进入」→ `/drive/<id>/` 列出设备内容。
3. 从 `/files/` 复制大文件到 `/drive/<id>/` → 传输面板字节进度 → 落盘正确。
4. 反向：从设备 move 文件到 `/files/` → 源删除、目标存在。
5. 点「卸载」→ 成功，`/drive/<id>/` 不再可访问。
6. 卸载正忙设备 → 报可读错误，虚拟挂载点仍可用。
7. supervisor 环境下（无 login session）验证 udisksctl 能否免密挂载（见风险 §1）。

### 回归测试

- `go test ./...` 全量通过；重点确认路径布局改动后既有 `fs/*` 测试对 `/files/` 前缀无回归（测试用例可能需相应更新）。
- 收藏夹迁移幂等：重复启动不重复加前缀。

## 上线与回滚

### 上线步骤

1. 后端：Mux 动态增删 + 锁 → Mux 跨挂载 copy/move → 设备服务（Linux/Windows build tag）→ 设备 handler + 路由 → `system/info` 扩展 → main 装配 rootMux/deviceMux → 收藏夹迁移。
2. 前端：types → api（devices）→ 设备管理页组件 → Sidebar 入口 → 默认落地页改 `/files`。
3. `make build` 重新打包前端进二进制。
4. 部署前确认目标机 `lsblk`、`udisksctl` 可用，且 supervisor 运行用户具备 udisks 挂载权限（见风险）。

### 回滚

1. 回滚代码到上一版本。
2. **收藏夹迁移是有状态改动**：回滚后收藏路径带着 `/files` 前缀会失效。需提供反向迁移（去前缀）或在迁移时记录版本标记，回滚脚本据此还原。这是本次唯一需要数据回滚的点，implementation 中详述迁移的幂等与可逆设计。
3. deviceMux 内存态随进程消失；`udisksctl` 已挂载的设备留在 OS 上不受影响，不产生脏数据。

## 建议实施顺序

1. `Mux` 增加读写锁 + `AddMount`/`RemoveMount`（先让组合驱动支持动态增删）。
2. `Mux` 补齐跨挂载点 `Copy`/`Move`（流式复制 + 删源，接 `ProgressCopier`）+ 测试。
3. `DeviceService`（Linux 实现 + Windows 空实现，build tag）+ 输出解析测试。
4. 设备 handler + 路由（认证 + 写限流）+ `system/info` 扩展。
5. main 装配：rootMux（files + drive）+ deviceMux 注入设备服务。
6. 收藏夹迁移（启动时幂等执行）。
7. 前端：types / api / 设备管理页 / Sidebar 入口 / 默认落地页。
8. `make build` + 全量测试 + 真机手动验证。

## 风险与待确认问题

### 已确认决策

1. 路径布局采用 A1（`/files/` + `/drive/`），接受收藏夹一次性迁移 —— 用户确认。
2. 跨挂载 copy 与 move 都做，move = 复制 + 删源 —— 用户确认。
3. flist 以**普通用户**身份、经 **supervisor** 后台运行 —— 用户告知（引出风险 1）。
4. 设备 `<id>` 采用 **kernel name**（如 `sdc1`）—— 用户确认（符合本期非持久化定位；重新插拔可能变名，可接受）。
5. 接受一次性给存量收藏加 `/files` 前缀（幂等 + 附反向回滚 SQL）—— 用户确认。

### 风险（阻塞，需在实现 / 部署前确认）

1. **【最高优先级】supervisor + 普通用户下 udisksctl 的 polkit 授权**：
   `udisksctl` 依赖 polkit 判断调用者是否有权挂载。桌面环境靠「本地活动会话（active session）」自动免密，但 **supervisor 拉起的进程通常没有 systemd/logind 登录会话**，polkit 的 `active` 检查会失败，导致挂载被拒（`Not authorized to perform operation` / 提示需要认证）。
   可选对策（需你确认部署侧能接受哪种）：
   - **(a) 配 polkit 规则**：为运行 flist 的用户放行 `org.freedesktop.udisks2.filesystem-mount`（及 `-other-seat`）动作，`/etc/polkit-1/rules.d/` 下加一条规则。**推荐**，最小权限、不需 root 跑 flist。
   - **(b) 用 `udisksctl` 的 system-bus 无会话路径**：部分版本对 `filesystem-mount-other-seat` 有独立动作，同样靠 polkit 规则放行。
   - **(c) flist 以能免密的方式运行**：不推荐（放大权限面）。
   这一条直接决定功能在你的部署上是否可用，建议**先在目标机上手动验证** `sudo -u <flist_user> udisksctl mount -b /dev/sdX1` 在无登录会话下是否成功，再决定 polkit 规则内容。

2. **收藏夹迁移的可逆性**：A1 需要给存量收藏路径加 `/files` 前缀，这是有状态迁移。回滚需要反向去前缀，implementation 会用「迁移版本标记」保证幂等与可逆，但仍需你确认可以接受这次一次性数据变更。

3. **设备 `<id>` 的稳定性**：用 kernel name（`sdc1`）做 `<id>` 简单直观，但同一设备重新插拔后 kernel name 可能变化（`sdc` → `sdd`）。因为 deviceMux 是运行时动态的、每次进页重新 `lsblk`，这在本期「非持久化」定位下可接受；但若你希望收藏某个设备内的子目录长期有效，需要改用 `PARTUUID` 派生 `<id>`（更稳定但对用户不直观）。**待确认：`<id>` 用 kernel name 还是 PARTUUID？**（默认倾向 kernel name，符合非持久化定位。）

### 非阻塞待确认

1. 「弹出」和「卸载」是否要区分（弹出 = unmount + power-off 断电，`udisksctl power-off`）？本期默认二者合一，都走 unmount；后续可加 power-off。
2. 设备列表是否需要展示已用 / 可用容量（需对已挂载设备额外 `Usage` 查询）？可作为列表项的增量信息，非阻塞。
3. 是否需要在文件浏览的面包屑 / 顶层 `/` 视图对 `files` 与 `drive` 做特殊图标 / 文案区分？属 UI 细节，实现时定。

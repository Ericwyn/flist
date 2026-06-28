# Linux 文件权限管理 MVP 方案概述

## 背景

flist 当前定位是部署在无界面 NAS 上的 Web 文件管理器，已经支持文件浏览、预览、下载、新建、删除、移动、复制、上传、打包下载和在线编辑等能力。NAS 上常见的真实使用场景是：同一块磁盘或同一组共享目录里，文件可能属于不同 Linux 用户或用户组，例如 `alice:media`、`bob:users`、`root:root`。

当前 flist 执行复制、上传、新建文件等写操作时，文件通常由运行 flist 的 Linux 进程用户创建。用户如果发现 owner、group 或 mode 不符合预期，只能通过 SSH 登录 NAS 后手动执行 `chown` / `chmod` 修正。这与 flist 希望提供完整 Web 管理能力的目标不一致。

本需求希望先做一个尽量简单的 Linux 文件权限管理 MVP，使用户可以：

1. 在文件列表中直接看到文件 owner / group / mode。
2. 通过右键菜单修改文件或目录的 owner / group。
3. 通过右键菜单修改文件或目录的 Unix 权限位。
4. 在不打开 SSH 的情况下完成常见 NAS 权限修正。

本 MVP 不做真正的「按请求切换 Linux 执行身份」。页面上如果后续出现“默认归属身份”选择，也应理解为“后续新建/上传/复制后自动 chown 到该 owner/group”，而不是让 Go Web 服务进程在请求期间 `setuid` 成其他 Linux 用户。

---

## 当前现状

### 后端现状

当前文件相关链路已经完成存储抽象分层：

```text
HTTP Handler
  -> service.FileService
  -> storage.Backend
  -> storage/local.Local
  -> OS filesystem
```

关键结构和接口：

1. `internal/model/file.go`
   - `model.FileInfo` 是 `GET /api/fs/list` 和 `GET /api/fs/stat` 返回的文件元信息。
   - 目前包含 `name`、`type`、`size`、`mode`、`mod_time`、`is_symlink` 等字段。
   - `mode` 已经返回八进制字符串，例如 `0644`、`0755`。

2. `internal/storage/backend.go`
   - `storage.Backend` 定义了 `Stat`、`List`、`Open`、`Mkdir`、`Create`、`Move`、`Remove`、`Copy` 等基础文件能力。
   - 可选能力已经通过接口扩展，例如 `Walker`、`Usager`、`Uploader`、`ContentEditor`。
   - 当前没有权限修改相关接口。

3. `internal/storage/local/local.go`
   - 本地文件系统驱动 `Local` 负责 `SafeResolve`、符号链接解析、隐藏文件过滤、权限 mode 格式化、复制/移动/删除等本地行为。
   - `buildFileInfo` 当前从 `os.FileInfo` 组装 `model.FileInfo`，但未从 Linux `syscall.Stat_t` 里提取 `uid/gid`。

4. `internal/handler/file.go`
   - 文件读写接口统一由 `FileHandler` 提供。
   - 写操作有审计日志 `audit`。
   - 驱动错误通过 `failFileErr` 映射为统一响应。

5. `internal/server/routes.go`
   - `/api/fs/list`、`/api/fs/stat` 等只读接口已注册。
   - `/api/fs/mkdir`、`/api/fs/touch`、`/api/fs/move`、`/api/fs/delete`、`/api/fs/copy`、`/api/fs/content` 保存、上传 init/complete 等写接口在写限流组内。
   - 权限修改接口应同样属于写操作，需要进入写限流组。

6. `internal/handler/system.go`
   - 当前系统接口只返回 OS、Arch、ServerTime。
   - 没有运行用户、权限能力、Linux 用户/用户组列表相关接口。

### 前端现状

关键文件：

1. `frontend/src/types.ts`
   - `FileEntry` 对应后端 `model.FileInfo`。
   - 当前有 `name`、`type`、`size`、`mode`、`modTime`、`isSymlink` 等字段。
   - 没有 `uid/gid/owner/group` 字段。

2. `frontend/src/lib/api.ts`
   - `api.fs.list`、`api.fs.stat` 会把后端 snake_case 字段映射成前端 camelCase。
   - 当前没有 `chmod`、`chown`、系统用户/组列表和权限能力接口。

3. `frontend/src/components/FileBrowser.tsx`
   - 当前文件列表已展示文件名称、大小、修改时间等信息。
   - 右键菜单已支持预览、下载、属性、新建、重命名、删除、复制、剪切、粘贴、收藏、上传等操作。
   - 可以在此基础上增加“修改权限”和“修改所有者”菜单项。

4. `frontend/src/components/PropertiesModal.tsx`
   - 当前属性弹窗可以扩展展示 owner/group/uid/gid。

### 当前问题点

1. `mode` 已展示，但 owner/group 不展示，用户无法直接判断文件归属。
2. flist 无法通过 Web 修改 owner/group/mode。
3. 复制、上传、新建等操作导致 owner 变成 flist 运行用户后，用户需要 SSH 修复。
4. 如果直接设计“页面切换 Linux 身份”，会引入 `setuid`、并发请求、root 权限恢复、supplementary groups 等复杂问题，不适合作为简单 MVP。

---

## 目标

本 MVP 要达成以下目标：

1. **文件元信息展示 owner/group**
   - Linux 下 `list/stat/search` 返回 `uid/gid/owner/group`。
   - 前端文件列表和属性弹窗可展示 owner、group、mode。

2. **支持修改权限位**
   - 增加 `PATCH /api/fs/chmod`。
   - 支持对单个文件或目录修改 Unix mode，例如 `0644`、`0755`。
   - 操作必须经过现有路径安全校验，不能逃逸 root。

3. **支持修改所有者和用户组**
   - 增加 `PATCH /api/fs/chown`。
   - 支持按用户名/用户组名修改 owner/group。
   - 也允许用户输入纯数字字符串作为 uid/gid。
   - root 启动时完整支持；非 root 启动时接口存在但实际操作可能返回 `permission_denied`，前端默认禁用 chown 入口。

4. **支持用户/组选择**
   - 增加 Linux 用户列表接口。
   - 增加 Linux 用户组列表接口。
   - 前端修改 owner/group 时优先使用下拉选择，也允许手动输入。

5. **能力显式化**
   - 增加权限能力接口，告诉前端当前平台是否支持读取 owner、修改 mode、修改 owner。
   - 非 Linux 或不支持的后端返回 `not_supported` 或能力为 false，前端隐藏/禁用对应入口。

6. **保持现有文件操作行为不变**
   - 本 MVP 不改变复制、上传、新建、保存文件时的 owner/mode 策略。
   - 用户需要修改时通过右键手动执行。

---

## 非目标

本 MVP 明确不做以下内容：

1. **不做真正的 Linux 身份切换**
   - 不在 Go Web 服务中按请求 `setuid` / `seteuid`。
   - 不实现“以 alice 身份执行所有文件操作”的进程级身份切换。

2. **不做默认归属身份自动应用**
   - 暂不实现“页面选择 alice:media 后，上传/复制/新建自动 chown”的功能。
   - 该能力可作为后续 Phase 2：默认文件归属策略。

3. **不做递归 chmod/chown**
   - MVP 只修改当前路径对应的单个文件或目录 inode。
   - 暂不对目录子树递归应用，避免大目录阻塞、失败聚合、回滚和 symlink 策略复杂化。
   - API 可以预留 `recursive` 字段，但 MVP 中传 `true` 返回 `not_supported`。

4. **不做 ACL / xattr / Samba ACL / NFS ACL**
   - 只处理 Unix 基础 owner、group、mode。

5. **不做 Windows 权限管理**
   - Windows 下继续保留现有简化 mode 映射。
   - owner/group/chown/chmod 能力返回不支持。

6. **不新增数据库表**
   - 用户/组来自 Linux 系统文件。
   - 权限操作不持久化业务配置。

7. **不改变认证模型**
   - flist 登录用户仍是应用层用户，不映射到 Linux 用户。
   - 只有已认证的 flist 用户可以调用权限接口。

---

## 核心设计

### 设计原则

1. **简单优先**
   - 第一版只做“看见 owner/group”和“手动 chmod/chown”。
   - 不引入复杂身份切换、递归任务和后台任务系统。

2. **Linux-only，能力驱动 UI**
   - 后端暴露能力，前端根据能力显示或禁用入口。
   - 不支持的平台不报错影响主流程。

3. **权限能力属于存储后端能力**
   - 通过 `storage` 可选接口扩展，而不是让 handler/service 直接操作 OS。
   - `local` Linux 实现权限修改；非 Linux 返回 `ErrNotSupported`。

4. **路径安全沿用存储驱动**
   - chmod/chown 必须经过 local driver 的 `SafeResolve`。
   - symlink 目标如果逃逸 root，必须拒绝。

5. **纯 Go 构建优先**
   - 用户/组解析优先通过解析 `/etc/passwd` 和 `/etc/group` 实现，避免引入依赖 CGO 的用户查询路径。
   - 保持项目单二进制、易交叉编译的交付目标。

### 为什么不做真正身份切换

| 方案 | 优点 | 缺点 | 结论 |
| --- | --- | --- | --- |
| Web 进程按请求 `setuid` | 语义接近“切换身份” | Go 多 goroutine/多线程下风险高；需要 root；恢复身份复杂；安全边界难控制 | 不选 |
| 每次操作 fork 子进程并以目标用户执行 | 隔离更清晰 | 需要 sudo/su/helper；配置复杂；跨平台和审计复杂 | 后续可评估，不进 MVP |
| flist 进程执行后 `chown/chmod` | 实现简单；符合 NAS 权限修正目标 | 不是严格意义的“以某用户执行” | MVP 选择 |

MVP 选择第三种方式：

```text
用户在页面右键选择 chmod/chown
  -> 后端验证认证与路径
  -> local backend SafeResolve
  -> Linux os.Chmod / os.Chown
  -> 返回结果并审计
```

---

## 数据结构 / 协议设计

### 1. 扩展 `model.FileInfo`

建议在 `internal/model/file.go` 中扩展：

```go
type FileInfo struct {
    Name          string    `json:"name"`
    Type          string    `json:"type"`
    Size          int64     `json:"size"`
    Mode          string    `json:"mode"`
    ModTime       time.Time `json:"mod_time"`
    IsSymlink     bool      `json:"is_symlink"`
    SymlinkTarget string    `json:"symlink_target,omitempty"`
    Unreachable   bool      `json:"unreachable,omitempty"`

    UID   *int   `json:"uid,omitempty"`
    GID   *int   `json:"gid,omitempty"`
    Owner string `json:"owner,omitempty"`
    Group string `json:"group,omitempty"`
}
```

字段说明：

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `uid` | `*int` | Linux uid；使用指针是为了支持 root 的 `0`，同时在不支持平台省略字段 |
| `gid` | `*int` | Linux gid；使用指针是为了支持 root group 的 `0` |
| `owner` | `string` | uid 对应的用户名；解析失败时回退为 uid 字符串 |
| `group` | `string` | gid 对应的用户组名；解析失败时回退为 gid 字符串 |

`model.SearchHit` 建议同步增加相同字段，保证搜索结果和普通列表展示一致。

### 2. 权限能力接口

新增：

```http
GET /api/system/permission-capabilities
```

认证：需要登录。

响应示例：

```json
{
  "code": 0,
  "message": "success",
  "data": {
    "os": "linux",
    "runtime_user": "root",
    "uid": 0,
    "gid": 0,
    "read_owner": true,
    "chmod": true,
    "chown": true
  }
}
```

非 root Linux 示例：

```json
{
  "code": 0,
  "message": "success",
  "data": {
    "os": "linux",
    "runtime_user": "flist",
    "uid": 998,
    "gid": 998,
    "read_owner": true,
    "chmod": true,
    "chown": false
  }
}
```

非 Linux 示例：

```json
{
  "code": 0,
  "message": "success",
  "data": {
    "os": "windows",
    "runtime_user": "",
    "uid": null,
    "gid": null,
    "read_owner": false,
    "chmod": false,
    "chown": false
  }
}
```

字段说明：

| 字段 | 说明 |
| --- | --- |
| `os` | `runtime.GOOS` |
| `runtime_user` | 当前 flist 进程用户，Linux 下从 `/etc/passwd` 解析当前 euid |
| `uid/gid` | 当前进程 euid/egid；非 Linux 可为 null |
| `read_owner` | 当前平台和后端是否能读取 owner/group |
| `chmod` | 是否启用 chmod UI；Linux 下为 true，但具体文件仍可能因权限不足返回 403 |
| `chown` | 是否启用 chown UI；MVP 中 Linux root 为 true，非 root 为 false |

### 3. Linux 用户列表接口

新增：

```http
GET /api/system/linux-users
```

认证：需要登录。

响应示例：

```json
{
  "code": 0,
  "message": "success",
  "data": {
    "users": [
      { "name": "root", "uid": 0 },
      { "name": "alice", "uid": 1001 },
      { "name": "bob", "uid": 1002 }
    ]
  }
}
```

非 Linux：返回 `not_supported`，或返回空数组并由 capabilities 控制前端不调用。MVP 推荐返回 `not_supported`，便于暴露环境差异。

### 4. Linux 用户组列表接口

新增：

```http
GET /api/system/linux-groups
```

认证：需要登录。

响应示例：

```json
{
  "code": 0,
  "message": "success",
  "data": {
    "groups": [
      { "name": "root", "gid": 0 },
      { "name": "media", "gid": 1001 },
      { "name": "users", "gid": 100 }
    ]
  }
}
```

### 5. chmod 接口

新增：

```http
PATCH /api/fs/chmod
```

认证：需要登录。属于写操作，套写限流和审计。

请求：

```json
{
  "path": "/movie/a.mkv",
  "mode": "0644",
  "recursive": false
}
```

响应：

```json
{
  "code": 0,
  "message": "success",
  "data": {
    "path": "/movie/a.mkv",
    "mode": "0644"
  }
}
```

规则：

1. `path` 必填。
2. `mode` 必填，支持 `644` 或 `0644` 输入，后端统一规范化为 `0644`。
3. MVP 只允许 `0000` 到 `0777`，不支持 setuid/setgid/sticky 特殊位。
4. `recursive=true` 在 MVP 中返回 `not_supported`。
5. 文件不存在返回 `path_not_found`。
6. 权限不足返回 `permission_denied`。
7. 非 Linux 或后端不支持返回 `not_supported`。

### 6. chown 接口

新增：

```http
PATCH /api/fs/chown
```

认证：需要登录。属于写操作，套写限流和审计。

请求：

```json
{
  "path": "/movie/a.mkv",
  "owner": "alice",
  "group": "media",
  "recursive": false
}
```

响应：

```json
{
  "code": 0,
  "message": "success",
  "data": {
    "path": "/movie/a.mkv",
    "uid": 1001,
    "gid": 1001,
    "owner": "alice",
    "group": "media"
  }
}
```

规则：

1. `path` 必填。
2. `owner` 和 `group` 至少提供一个。
3. `owner` 可以是用户名，也可以是纯数字 uid 字符串。
4. `group` 可以是用户组名，也可以是纯数字 gid 字符串。
5. 未提供的 owner/group 保持不变；调用 `os.Chown(path, uid, -1)` 或 `os.Chown(path, -1, gid)`。
6. 用户名或用户组名不存在时返回 `bad_request`，message 可为 `user_not_found` 或 `group_not_found`。
7. `recursive=true` 在 MVP 中返回 `not_supported`。
8. 非 root Linux 进程调用 chown 通常返回 `permission_denied`；前端应在 capabilities 中 `chown=false` 时禁用入口。
9. 非 Linux 或后端不支持返回 `not_supported`。

---

## 实现方案

### 1. 后端 model 层

修改：

- `internal/model/file.go`

改动：

1. `FileInfo` 增加 `UID/GID/Owner/Group`。
2. `SearchHit` 增加 `UID/GID/Owner/Group`。
3. 新增权限相关响应结构：

```go
type PermissionCapabilities struct {
    OS          string `json:"os"`
    RuntimeUser string `json:"runtime_user"`
    UID         *int   `json:"uid"`
    GID         *int   `json:"gid"`
    ReadOwner   bool   `json:"read_owner"`
    Chmod       bool   `json:"chmod"`
    Chown       bool   `json:"chown"`
}

type LinuxUser struct {
    Name string `json:"name"`
    UID  int    `json:"uid"`
}

type LinuxGroup struct {
    Name string `json:"name"`
    GID  int    `json:"gid"`
}

type ChmodResult struct {
    Path string `json:"path"`
    Mode string `json:"mode"`
}

type ChownResult struct {
    Path  string `json:"path"`
    UID   *int   `json:"uid,omitempty"`
    GID   *int   `json:"gid,omitempty"`
    Owner string `json:"owner,omitempty"`
    Group string `json:"group,omitempty"`
}
```

### 2. storage 抽象层

修改：

- `internal/storage/backend.go`

新增可选接口：

```go
type Permissioner interface {
    Chmod(ctx context.Context, p string, mode fs.FileMode) error
    Chown(ctx context.Context, p string, uid, gid *int) (*model.FileInfo, error)
}
```

说明：

1. `Chmod` 只负责修改 mode。
2. `Chown` 修改后返回最新 `FileInfo`，便于 handler 组装响应。
3. service 通过类型断言判断 backend 是否实现该接口。
4. 后续 `Mux` 启用多挂载时，可以按路径路由到命中的挂载点实现。

是否扩展 `storage.Caps`：

MVP 推荐先不强制扩展 `Caps`，避免影响现有测试和远期驱动；权限能力由 `Permissioner` + system capabilities 接口提供。后续如果前端需要路径级能力，再扩展 `Caps` 或新增 `PermissionCapabilities(ctx,path)` 可选接口。

### 3. local Linux 实现

新增文件：

```text
internal/storage/local/permission_linux.go
internal/storage/local/permission_unsupported.go
```

Linux 实现：

1. `Chmod(ctx,p,mode)`
   - `CleanAPIPath`。
   - `SafeResolve`。
   - `os.Chmod(localPath, mode)`。
   - `os.ErrPermission` 映射为 `storage.ErrForbidden`。
   - 其他路径错误沿用 `mapErr`。

2. `Chown(ctx,p,uid,gid)`
   - `CleanAPIPath`。
   - `SafeResolve`。
   - 未提供 uid 时传 `-1`。
   - 未提供 gid 时传 `-1`。
   - `os.Chown(localPath, uidValue, gidValue)`。
   - 修改完成后调用 `Stat(ctx,p)` 返回最新信息。

3. symlink 行为
   - MVP 沿用 `SafeResolve`：最终目标必须在 root 内。
   - 如果 API path 是 symlink 且目标在 root 内，chmod/chown 作用于目标文件。
   - 如果 symlink 不可达或目标逃逸 root，返回 `path_traversal` / `path_not_found`。
   - MVP 不支持修改 symlink 自身 owner。

非 Linux 实现：

1. `Chmod` 返回 `storage.ErrNotSupported`。
2. `Chown` 返回 `storage.ErrNotSupported`。

### 4. Linux owner/group 解析工具

新增：

```text
internal/util/user_linux.go
internal/util/user_unsupported.go
```

Linux 下提供：

```go
type LinuxUser struct { Name string; UID int }
type LinuxGroup struct { Name string; GID int }

func ListLinuxUsers() ([]LinuxUser, error)
func ListLinuxGroups() ([]LinuxGroup, error)
func LookupUserNameByUID(uid int) string
func LookupGroupNameByGID(gid int) string
func ResolveUser(value string) (int, string, error)
func ResolveGroup(value string) (int, string, error)
func CurrentProcessIdentity() (name string, uid int, gid int)
```

实现约束：

1. 解析 `/etc/passwd` 获取用户。
2. 解析 `/etc/group` 获取用户组。
3. 不使用 `os/user`，避免潜在 CGO 依赖。
4. 名称解析失败时，如果输入是纯数字，则按 uid/gid 使用。
5. 展示 owner/group 时，如果 uid/gid 在系统文件中找不到，则回退为数字字符串。
6. 允许缓存解析结果，降低大目录 `List` 时重复解析成本。

缓存建议：

```text
首次请求 list/stat/search
  -> lazy load /etc/passwd 和 /etc/group
  -> 缓存在进程内
  -> 后续复用
```

如果担心系统用户变更后缓存不刷新，可以在 MVP 中接受进程重启刷新；后续再做 TTL。

### 5. local FileInfo 填充 owner/group

修改：

- `internal/storage/local/local.go`

改动点：

1. 在 `buildFileInfo` 中，Linux 下从 `fi.Sys()` 读取 `syscall.Stat_t.Uid/Gid`。
2. 填充：
   - `UID`
   - `GID`
   - `Owner`
   - `Group`
3. 在 `walkInfo` 中也填充相同字段，保证搜索结果可展示 owner/group。
4. 非 Linux 下不填充这些字段。

建议通过工具函数隔离平台差异：

```go
func enrichOwnership(info *model.FileInfo, fi os.FileInfo)
```

对应文件：

```text
internal/storage/local/ownership_linux.go
internal/storage/local/ownership_unsupported.go
```

### 6. service 层

修改：

- `internal/service/file.go`

新增方法：

```go
func (s *FileService) Chmod(ctx context.Context, apiPath string, mode string, recursive bool) (*model.ChmodResult, error)
func (s *FileService) Chown(ctx context.Context, apiPath string, owner, group string, recursive bool) (*model.ChownResult, error)
```

职责：

1. 清理 API path。
2. 校验 `recursive`：MVP 中 `true` 返回 `storage.ErrNotSupported`。
3. 校验和解析 mode：
   - 允许 `644` / `0644`。
   - 规范化为 `0644`。
   - 超出 `0777` 返回 `storage.ErrBadOp`。
4. 解析 owner/group：
   - owner/group 至少一个非空。
   - 调用 `util.ResolveUser` / `util.ResolveGroup`。
5. 通过 `Permissioner` 类型断言调用 backend。
6. backend 未实现时返回 `storage.ErrNotSupported`。

### 7. handler 层

修改：

- `internal/handler/file.go`
- `internal/handler/system.go`

新增请求结构：

```go
type chmodRequest struct {
    Path      string `json:"path"`
    Mode      string `json:"mode"`
    Recursive bool   `json:"recursive"`
}

type chownRequest struct {
    Path      string `json:"path"`
    Owner     string `json:"owner"`
    Group     string `json:"group"`
    Recursive bool   `json:"recursive"`
}
```

新增 handler：

1. `FileHandler.Chmod`
2. `FileHandler.Chown`
3. `SystemHandler.PermissionCapabilities`
4. `SystemHandler.LinuxUsers`
5. `SystemHandler.LinuxGroups`

审计日志：

```text
action=chmod path=/x result=ok/fail user=<flist-user> request_id=<id> ip=<ip>
action=chown path=/x result=ok/fail user=<flist-user> request_id=<id> ip=<ip>
```

### 8. 路由层

修改：

- `internal/server/routes.go`

新增受保护只读系统接口：

```go
protected.Get("/system/permission-capabilities", systemHandler.PermissionCapabilities)
protected.Get("/system/linux-users", systemHandler.LinuxUsers)
protected.Get("/system/linux-groups", systemHandler.LinuxGroups)
```

新增写操作接口，放入写限流组：

```go
wr.Patch("/fs/chmod", fileHandler.Chmod)
wr.Patch("/fs/chown", fileHandler.Chown)
```

### 9. 前端类型与 API

修改：

- `frontend/src/types.ts`
- `frontend/src/lib/api.ts`

扩展 `FileEntry`：

```ts
export interface FileEntry {
  name: string;
  type: EntryType;
  size: number;
  mode: string;
  modTime: string;
  isSymlink: boolean;
  symlinkTarget?: string;
  unreachable?: boolean;
  uid?: number;
  gid?: number;
  owner?: string;
  group?: string;
}
```

新增类型：

```ts
export interface PermissionCapabilities {
  os: string;
  runtimeUser: string;
  uid: number | null;
  gid: number | null;
  readOwner: boolean;
  chmod: boolean;
  chown: boolean;
}

export interface LinuxUser { name: string; uid: number }
export interface LinuxGroup { name: string; gid: number }
```

新增 API：

```ts
api.system.permissionCapabilities()
api.system.linuxUsers()
api.system.linuxGroups()
api.fs.chmod(path, mode, recursive)
api.fs.chown(path, owner, group, recursive)
```

### 10. 前端 UI

修改：

- `frontend/src/components/FileBrowser.tsx`
- `frontend/src/components/PropertiesModal.tsx`
- 可新增 `PermissionModal.tsx` / `OwnershipModal.tsx`

文件列表：

1. 在列表视图增加 owner/group 展示。
2. mode 已存在则继续展示。
3. 如果 capabilities `read_owner=false`，隐藏 owner/group 列。
4. 如果 owner/group 字段为空，显示 `-`。

右键菜单：

1. 当 `chmod=true` 时展示：`修改权限...`
2. 当 `chown=true` 时展示：`修改所有者...`
3. 如果文件 `unreachable=true`，禁用这两个操作。

权限弹窗：

```text
修改权限

路径：/movie/a.mkv
当前权限：0644
新权限：[0644]

[取消] [保存]
```

校验：

1. 前端只做基础正则校验。
2. 后端仍做最终校验。

所有者弹窗：

```text
修改所有者

路径：/movie/a.mkv
当前：alice:media
用户：[alice v]  或手动输入
用户组：[media v] 或手动输入

[取消] [保存]
```

交互：

1. 打开弹窗时懒加载 users/groups。
2. 加载失败时允许手动输入。
3. 成功后刷新当前目录。
4. 失败时 toast 展示后端错误。

---

## 兼容与降级

### 旧客户端兼容

1. `FileInfo` 只新增可选字段，旧前端忽略即可。
2. 新接口不影响旧接口。
3. 不需要数据库迁移。

### 非 Linux 降级

1. `uid/gid/owner/group` 字段不返回。
2. capabilities 中 `read_owner/chmod/chown=false`。
3. `/api/system/linux-users`、`/api/system/linux-groups` 返回 `not_supported`。
4. `/api/fs/chmod`、`/api/fs/chown` 返回 `not_supported`。

### 非 root 降级

1. `read_owner=true`。
2. `chmod=true`，但具体文件可能返回 `permission_denied`。
3. `chown=false`，前端默认禁用“修改所有者”。
4. 如果用户直接调用 chown API，后端执行后返回 `permission_denied` 或能力检查返回 `not_supported`。MVP 推荐仍尝试调用 backend，让 OS 决定最终权限，便于具备特殊能力的运行环境。

### 用户/组解析失败

1. 展示时 uid/gid 找不到名称，owner/group 回退为数字字符串。
2. chown 请求中 owner/group 名称找不到时返回 `bad_request`。
3. 输入纯数字时按 uid/gid 处理，不要求必须存在于 `/etc/passwd` 或 `/etc/group`。

### 路径和 symlink

1. 路径安全继续由 `SafeResolve` 保证。
2. symlink 目标必须在 root 内。
3. unreachable symlink 不允许 chmod/chown。
4. MVP 不修改 symlink 自身 owner/mode。

---

## 可观测性

### 日志

新增审计日志：

```text
action=chmod path=/x result=ok user=admin request_id=... ip=...
action=chmod path=/x result=fail user=admin request_id=... ip=...
action=chown path=/x result=ok user=admin request_id=... ip=...
action=chown path=/x result=fail user=admin request_id=... ip=...
```

建议额外记录：

```text
mode=0644
owner=alice
uid=1001
group=media
gid=1001
```

注意不要记录敏感 token 或完整请求头。

### 指标

当前项目未引入 metrics 系统，MVP 不新增指标。后续如引入 metrics，可统计：

1. chmod 成功/失败次数。
2. chown 成功/失败次数。
3. permission_denied 次数。
4. not_supported 次数。

---

## 测试计划

### 单元测试

1. `util` 用户/组解析
   - 正常解析 `/etc/passwd` 风格内容。
   - 正常解析 `/etc/group` 风格内容。
   - 跳过空行、注释、格式非法行。
   - uid/gid 名称查找失败时回退数字。
   - 纯数字 owner/group 输入可以解析。

2. mode 校验
   - `644` 规范化为 `0644`。
   - `0644` 保持 `0644`。
   - `777` 规范化为 `0777`。
   - `1777` 返回不支持或 bad request。
   - `abc`、`888`、空字符串返回 bad request。

3. local permissioner
   - Linux 下临时文件 chmod 后 mode 变化。
   - 非 root 环境下 chown 可能跳过或断言返回 `permission_denied`。
   - path 不存在返回 `ErrNotFound`。
   - 越界路径返回 `ErrTraversal`。

4. service
   - backend 不实现 `Permissioner` 返回 `ErrNotSupported`。
   - `recursive=true` 返回 `ErrNotSupported`。
   - owner/group 都为空返回 `ErrBadOp`。

5. handler
   - JSON 缺字段返回 bad request。
   - 权限错误映射为统一信封。
   - chmod/chown 成功返回预期 data。

### 集成测试

1. 启动测试 server，登录后调用：
   - `GET /api/system/permission-capabilities`
   - `GET /api/fs/list`
   - `PATCH /api/fs/chmod`
   - `PATCH /api/fs/chown`
2. 验证未登录请求返回 401。
3. 验证 chmod/chown 接口进入写限流组。

### 手动验证

Linux root 启动：

1. 文件列表能看到 owner/group/mode。
2. 右键修改文件 `0644 -> 0600` 成功。
3. 右键修改目录 `0755 -> 0775` 成功。
4. 右键修改 owner/group 成功。
5. SSH 上 `ls -l` 验证结果一致。
6. 修改不存在用户返回错误。
7. 修改不存在路径返回错误。
8. symlink 指向 root 外时无法修改。

Linux 非 root 启动：

1. 文件列表仍能看到 owner/group/mode。
2. chown 菜单禁用。
3. chmod 自己拥有的文件成功。
4. chmod 不属于自己的文件返回 permission denied。

Windows：

1. owner/group 列不展示。
2. chmod/chown 菜单不展示。
3. 直接调用接口返回 not_supported。

---

## 上线与回滚

### 上线

1. 不需要数据库迁移。
2. 不改变已有配置。
3. 默认所有用户登录后可使用权限能力；如果后续需要角色控制，再单独设计。
4. root 启动时功能完整；非 root 启动时能力降级。

### 回滚

1. 回滚代码即可。
2. 新增 JSON 字段不会影响旧版本数据。
3. chmod/chown 已经对文件系统产生真实副作用，代码回滚不会恢复文件权限。
4. 因此权限修改操作必须有明确确认弹窗和审计日志。

---

## 建议实施顺序

1. **后端 owner/group 展示**
   - 扩展 `FileInfo` / `SearchHit`。
   - local Linux 下填充 uid/gid/owner/group。
   - 前端类型和列表展示 owner/group。

2. **系统能力与用户/组列表**
   - 增加 capabilities。
   - 增加 linux-users/linux-groups。
   - 前端根据 capabilities 控制列和菜单。

3. **chmod API + UI**
   - 新增 `Permissioner.Chmod`。
   - 新增 `PATCH /api/fs/chmod`。
   - 前端权限弹窗。

4. **chown API + UI**
   - 新增 `Permissioner.Chown`。
   - 新增 `PATCH /api/fs/chown`。
   - 前端 owner/group 弹窗。

5. **补齐测试与审计**
   - 单测、server 测试、手动验证。
   - 确认 audit 日志字段。

---

## 风险与待确认问题

### 风险

1. **root 启动风险**
   - root 启动后 Web 服务具备修改暴露目录 owner/group/mode 的能力。
   - 需要确保服务只暴露在可信网络，并使用强密码。

2. **chmod/chown 是真实文件系统副作用**
   - 错误修改可能导致 Samba/NFS/应用无法访问文件。
   - 前端需要确认弹窗，尤其是 chown。

3. **用户/组来源不完整**
   - MVP 解析 `/etc/passwd` 和 `/etc/group`，对 LDAP/SSSD/NIS 用户可能不完整。
   - 允许手动输入纯数字 uid/gid 作为兜底。

4. **非 root 能力展示不完全准确**
   - `chmod=true` 只代表系统支持 chmod，不代表对每个文件都有权限。
   - 具体成功与否仍以 OS 返回为准。

5. **symlink 行为需要用户理解**
   - MVP 修改 symlink 在 root 内的目标，而非 symlink 本身。
   - 需要在文档或 UI 中提示。

### 待确认问题

1. MVP 是否确认 **不支持递归 chmod/chown**？如果用户强依赖目录递归修复权限，可以把递归作为第一版的一部分，但实现和风险都会上升。
2. `chown` 接口在非 root 时，是前端禁用但后端仍尝试调用，还是后端在 capabilities 判断非 root 后直接返回 `not_supported`？本文建议后端仍尝试，让具备特殊 capability 的环境可用。
3. 用户/组列表是否接受 MVP 只解析 `/etc/passwd` 和 `/etc/group`？如果 NAS 使用 LDAP/SSSD，第一版下拉可能不完整，但可手动输入数字 uid/gid。
4. 文件列表默认是否展示 owner/group 列，还是放到“列设置”里默认关闭？本文建议 Linux 下默认展示，非 Linux 隐藏。
5. chmod 是否允许特殊位，如 `1777`、`2755`？本文建议 MVP 仅允许 `0000-0777`，特殊位后续再支持。

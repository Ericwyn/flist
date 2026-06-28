# 文件编辑与路径级容量优化方案概述

> 创建日期：2026-06-26  
> 初始 spec：`docs/spec/20260626-project-init/`  
> 本文档描述在 Phase 0–6 已完成基础上的新增优化，不直接修改初始 spec 的历史结论。

## 背景

flist 当前已经完成初始阶段规划中的认证、文件浏览、下载、基础写操作、复制、收藏夹、分片上传、批量操作、打包下载和系统信息展示。项目定位是面向无界面 NAS 的远程文件浏览器，用户在浏览文件之外，还会有两个常见诉求：

1. 对配置、脚本、Markdown、日志片段等文本文件进行轻量编辑，而不是每次下载到本地修改后再上传覆盖。
2. 在 Docker、多磁盘、多挂载或未来远程存储场景中，查看“当前目录所在存储”的剩余容量，而不是只看服务 root 所在文件系统的容量。

现有 Phase 6 的磁盘用量展示已经可用，但它更适合单 root、单磁盘、本地部署场景。随着项目已引入 `storage.Backend` 和 `storage.Mux` 抽象，容量展示也应从全局系统信息演进为路径级存储能力。

## 当前现状

### 后端现状

当前主链路为：

```text
HTTP Handler
  -> service.FileService
  -> storage.Backend / 可选接口
  -> local / Mux / 未来远程驱动
```

关键现状：

1. `internal/storage/backend.go` 已定义核心 `Backend` 接口，包含 `Stat`、`List`、`Open`、`Mkdir`、`Create`、`Move`、`Remove`、`Copy`。
2. `storage.File` 要求实现 `io.ReadSeeker` + `io.Closer`，以支撑下载和预览中的 Range / Seek 语义。
3. `storage.Usager` 已作为可选接口存在：

```go
type Usager interface {
    Usage(ctx context.Context, p string) (total, free uint64, err error)
}
```

4. `service.FileService.Usage(ctx, apiPath)` 已通过类型断言调用 `storage.Usager`，当前 `system/info` 固定传入 `/`。
5. `GET /api/system/info` 当前返回 `disk_total`、`disk_used`、`disk_free`，含义是 root 所在文件系统的容量信息。
6. `GET /api/fs/preview` 只读取前 `64 KiB` 文本内容，适合预览，不适合作为完整编辑接口。
7. `GET /api/fs/download` 已根据 `size + modTime.UnixNano()` 生成 ETag，但该 ETag 仅用于下载缓存协商，尚未作为写入冲突校验协议。
8. 写操作已有审计日志、写限流、路径安全中间件和驱动层统一错误词表。

### 前端现状

1. `frontend/src/components/PreviewModal.tsx` 支持文本预览、图片、视频、音频内联查看。
2. 文本预览使用 `api.fs.preview(path)`，内容可能被截断。
3. `frontend/src/components/Sidebar.tsx` 登录后调用一次 `api.system.info()`，在侧边栏底部展示磁盘总量、剩余和使用率。
4. `frontend/src/lib/api.ts` 目前只有 `api.system.info()`，没有路径级容量接口。
5. 前端 token 当前存储在 `localStorage`，新窗口打开同源 SPA 页面时可以复用登录态。
6. 右键菜单已经存在文件操作入口，但没有“编辑”或“新窗口打开编辑器”入口。

## 当前问题

1. **文本文件无法直接编辑**：只能预览或下载，修改文件需要离开 flist 工作流。
2. **预览接口不适合编辑**：预览有截断、类型提示和轻量读取逻辑，不应承担完整内容加载与保存。
3. **缺少写入冲突保护**：NAS 文件可能被 SMB/NFS/SSH/其他浏览器窗口或容器内进程修改，直接保存会产生静默覆盖。
4. **容量展示口径不准确**：`system/info` 固定展示 root 所在存储容量，无法表达当前目录所在磁盘或挂载点。
5. **多挂载扩展不自然**：未来启用 `Mux` 后，虚拟根、本地挂载、WebDAV/网盘挂载的容量语义不同，全局 `system/info` 难以统一表达。
6. **刷新策略不符合用户直觉**：当前容量登录后只加载一次，上传、删除、复制、移动或切换目录后不会反映当前路径的容量变化。

## 目标

1. 支持文本文件完整读取、在线编辑和保存。
2. 保存时支持乐观锁校验，避免静默覆盖外部或其他窗口修改。
3. 支持右键菜单中“编辑”和“新窗口编辑”。
4. 新增独立编辑器页面，支持通过 SPA 路由直接打开指定文件。
5. 新增路径级容量接口，返回当前路径所在存储的容量 / 配额信息。
6. 前端在切换目录和写操作完成后刷新当前目录容量。
7. 设计保持与 `storage.Backend` / `Mux` 抽象兼容，未来远程驱动可以用自身 ETag、Last-Modified、Quota 等能力接入。
8. 保持现有预览、下载、上传、复制、删除等接口行为不变。

## 非目标

1. 不做二进制文件编辑。
2. 不做超大文本文件流式编辑；首期只支持大小上限内的完整加载和完整保存。
3. 不做多人实时协作、在线 diff/merge 或 CRDT。
4. 不做复杂编码自动识别与跨编码保存；首期仅支持 UTF-8 / UTF-8 BOM。
5. 不新增数据库表保存文件内容或编辑历史。
6. 不在 URL 中携带认证 token。
7. 不要求所有存储后端都必须支持容量和 revision；不支持时返回明确能力状态或错误。
8. 不废弃 `GET /api/system/info` 这个接口本身，但本轮直接移除其中的磁盘容量字段，使其只承载系统级信息。

## 核心设计

### 总体原则

1. **编辑接口独立于预览接口**：预览继续轻量截断读取；编辑接口完整读取并返回保存所需 revision。
2. **revision 是不透明 token**：前端不理解 token 生成规则，只在保存时原样带回。
3. **保存采用乐观锁**：服务端保存前重新读取当前 revision，和 `expected_revision` 不一致则返回冲突。
4. **路径级容量优先**：容量查询按用户当前目录路由到对应 backend，而不是固定查询全局 root。
5. **能力差异显式表达**：本地驱动支持文本编辑和容量；未来远程驱动可按能力逐步接入，不支持时可降级。
6. **现有接口兼容**：保留 `system/info` 和 `preview` 语义，新增接口承载新增能力。

### 方案取舍

| 方案 | 优点 | 缺点 | 结论 |
| --- | --- | --- | --- |
| 复用 `preview` 做编辑读取 | 改动少 | 预览有截断语义，容易误保存不完整内容 | 不采用 |
| 新增 `fs/content` 读写接口 | 语义清晰，可单独控制大小、编码、revision | 需要新增后端和前端模型 | 采用 |
| 使用 `mtime + size` 做乐观锁 | 成本低，不必读完整内容 | 弱校验，部分文件系统时间精度有限 | 可作为弱 revision 或 fallback |
| 使用内容 hash 做乐观锁 | 强校验，能准确发现内容变化 | 需要读取内容计算 hash | 本地文本编辑首选 |
| 容量继续放在 `system/info` | 接口少 | 多磁盘 / 多挂载语义不准确 | 不采用作为主方案 |
| 新增 `fs/space?path=...` | 与当前目录一致，适合 Docker、多磁盘、多挂载 | 需要前端刷新策略 | 采用 |

## 协议设计

### 1. 读取可编辑文本

```http
GET /api/fs/content?path=/notes/todo.md
```

成功响应：

```json
{
  "path": "/notes/todo.md",
  "name": "todo.md",
  "size": 1024,
  "mime": "text/markdown; charset=utf-8",
  "encoding": "utf-8",
  "line_ending": "lf",
  "content": "# Todo\n\n...",
  "mod_time": "2026-06-26T10:00:00Z",
  "revision": {
    "token": "sha256:...",
    "weak": false
  },
  "editable": true,
  "readonly": false
}
```

字段说明：

| 字段 | 含义 |
| --- | --- |
| `path` | 规范化后的 API 路径 |
| `name` | 文件名 |
| `size` | 文件大小，字节 |
| `mime` | 后端推断的 MIME |
| `encoding` | 当前内容编码，首期为 `utf-8` 或 `utf-8-bom` |
| `line_ending` | `lf` / `crlf` / `mixed` / `none` |
| `content` | 完整文本内容 |
| `mod_time` | 文件修改时间 |
| `revision.token` | 保存时必须带回的不透明版本 token |
| `revision.weak` | 是否为弱校验 token |
| `editable` | 当前文件是否可编辑 |
| `readonly` | 当前文件或后端是否只读 |

### 2. 保存文本

```http
PUT /api/fs/content
```

请求：

```json
{
  "path": "/notes/todo.md",
  "content": "# Todo\n\nupdated...",
  "expected_revision": "sha256:...",
  "encoding": "utf-8",
  "line_ending": "lf",
  "force": false
}
```

成功响应：

```json
{
  "path": "/notes/todo.md",
  "size": 2048,
  "mod_time": "2026-06-26T10:05:00Z",
  "revision": {
    "token": "sha256:new...",
    "weak": false
  }
}
```

冲突响应：

```http
409 Conflict
```

```json
{
  "code": 2012,
  "message": "file_modified",
  "data": {
    "path": "/notes/todo.md",
    "current_mod_time": "2026-06-26T10:04:00Z",
    "current_revision": {
      "token": "sha256:current...",
      "weak": false
    }
  }
}
```

说明：

1. `expected_revision` 缺失时默认拒绝保存，除非 `force=true`。
2. `force=true` 表示用户明确选择覆盖当前版本，服务端仍应执行路径安全、类型、大小和权限校验。
3. 保存成功后返回新的 revision，前端更新本地编辑态。
4. **行尾处理**：服务端对 `content` 字节**原样保存，不做行尾转换**。`encoding` / `line_ending` 仅作为读取时探测结果和前端编辑器配置提示，保存时不依据它们改写内容，避免对原本 CRLF 文件产生整文件 diff。若未来需要“统一行尾”能力，由前端在提交前显式转换 `content`。

### 3. 路径级容量

```http
GET /api/fs/space?path=/movies
```

成功响应：

```json
{
  "path": "/movies",
  "mount": {
    "name": "local",
    "prefix": "/"
  },
  "space": {
    "supported": true,
    "total": 4000787030016,
    "used": 2100450021376,
    "free": 1900337008640,
    "available": 1850000000000,
    "used_percent": 52.5
  },
  "readonly": false
}
```

驱动不支持容量时：

```json
{
  "path": "/remote",
  "mount": {
    "name": "webdav",
    "prefix": "/remote"
  },
  "space": {
    "supported": false
  },
  "readonly": false
}
```

字段说明：

| 字段 | 含义 |
| --- | --- |
| `path` | 规范化后的查询路径 |
| `mount.name` | 命中的存储驱动或挂载点名称；单挂载透明模式可为空或 `local` |
| `mount.prefix` | 命中的挂载前缀；单挂载透明模式为 `/` |
| `space.supported` | 当前后端是否支持容量查询 |
| `space.total` | 总容量 |
| `space.used` | 已用容量 |
| `space.free` | 文件系统空闲容量 |
| `space.available` | 当前进程 / 当前用户可用容量；首期本地可先等同 `free` |
| `space.used_percent` | 使用率百分比 |
| `readonly` | 当前路径所在后端是否只读 |

### 4. `system/info` 职责调整

`GET /api/system/info` 本轮直接收敛为纯系统级信息，不再返回 `disk_total` / `disk_used` / `disk_free`。容量展示统一改用 `GET /api/fs/space?path=...`。

```json
{
  "os": "linux",
  "arch": "amd64",
  "server_time": "2026-06-26T10:00:00Z"
}
```

兼容策略：

1. 当前项目仍处于快速迭代阶段，本轮允许直接调整 `system/info` 返回体。
2. 新前端容量展示改用 `/api/fs/space`。
3. 若旧前端仍调用 `api.system.info()` 读取磁盘字段，需要随本轮前端改造同步删除该依赖。

## 后端实现方案

### 1. 模型与错误码

建议新增模型：

```go
type FileRevision struct {
    Token string `json:"token"`
    Weak  bool   `json:"weak"`
}

type FileContentResult struct {
    Path       string       `json:"path"`
    Name       string       `json:"name"`
    Size       int64        `json:"size"`
    MIME       string       `json:"mime"`
    Encoding   string       `json:"encoding"`
    LineEnding string       `json:"line_ending"`
    Content    string       `json:"content"`
    ModTime    time.Time    `json:"mod_time"`
    Revision   FileRevision `json:"revision"`
    Editable   bool         `json:"editable"`
    Readonly   bool         `json:"readonly"`
}

type SaveContentResult struct {
    Path     string       `json:"path"`
    Size     int64        `json:"size"`
    ModTime  time.Time    `json:"mod_time"`
    Revision FileRevision `json:"revision"`
}
```

建议新增错误码：

| Code | Message | 场景 |
| --- | --- | --- |
| `2012` | `file_modified` | 保存时 revision 不匹配 |
| `2013` | `unsupported_media_type` | 文件不是可编辑文本 |
| `2014` | `file_too_large` | 超过可编辑大小上限 |
| `2015` | `readonly_storage` | 后端或文件只读 |
| `2016` | `invalid_revision` | `expected_revision` 缺失或格式非法 |

> `2001`–`2011` 均已被占用：`2007`/`2008` 为 `not_a_file`/`not_a_dir`，`2009`/`2010`/`2011` 为分片上传的 `upload_too_large`/`upload_not_found`/`upload_incomplete`。新错误码必须从当前最大值之后续号，故从 `2012` 起。落地时以 `internal/handler/errors.go` 实际最大值为准，并同步更新前端 `frontend/src/fsStore.ts` 的 code→文案映射。

### 2. 配置

新增配置：

| 配置项 | 环境变量 | 启动参数 | 默认值 | 说明 |
| --- | --- | --- | --- | --- |
| 最大可编辑文件大小 | `FLIST_MAX_EDIT_SIZE` | `--max-edit-size` | `5MiB` | 超过上限拒绝编辑 |

本轮不增加独立的编辑功能开关，默认允许文本编辑。若需要临时收紧能力，可通过把 `--max-edit-size` 设置为较小值实现近似禁用；不额外引入 `--enable-edit` / `--disable-edit`。

### 3. 存储层可选能力

为避免 service 直接假设所有后端都能原子写文件，建议增加可选接口：

```go
type ContentEditor interface {
    ReadText(ctx context.Context, p string, maxBytes int64) (*model.FileContentResult, error)
    WriteText(ctx context.Context, p string, content []byte, expected model.FileRevision, force bool) (*model.SaveContentResult, error)
}
```

但从当前代码演进成本看，也可以首期先在 `FileService` 基于现有 `Backend.Open` + 本地驱动新增写入方法实现。两种方案取舍如下：

| 方案 | 优点 | 缺点 | 建议 |
| --- | --- | --- | --- |
| 新增 `ContentEditor` 可选接口 | 远程后端可使用原生 ETag / If-Match；职责清晰 | 需要改 storage 接口和 local 实现 | 推荐作为正式设计 |
| service 基于 `Open` + local 特化写入 | 初期改动小 | service 需要感知更多后端细节，远期难扩展 | 不推荐长期使用 |

正式设计建议采用 `ContentEditor`。同时，按现有 `Caps` 标志位与可选接口成对出现的约定（`Caps.Upload` ↔ `Uploader`、`Caps.DiskUsage` ↔ `Usager`），新增 `Caps.Edit bool` 与 `ContentEditor` 配对：

```go
type Caps struct {
    Write     bool
    Copy      bool
    Upload    bool
    DiskUsage bool
    Edit      bool // 文本读取 / 保存（配合可选 ContentEditor 接口）
}
```

service 通过 `Caps.Edit` + 类型断言探测能力；前端可据 capabilities 提前置灰编辑入口，避免无谓请求。`local` 驱动首期实现：

1. `ReadText`：SafeResolve、普通文件校验、大小上限校验、文本嗅探、读取完整内容、计算 hash revision。
2. `WriteText`：路径级锁外层由 service 控制，驱动内重新读取当前 revision，匹配后写同目录临时文件并原子替换。

### 4. revision 生成策略

本地驱动：

1. 读取编辑内容时计算 `sha256`：`sha256:<hex>`，`weak=false`。
2. 如果后续为了性能允许不读完整内容，可使用 `mtime-size:<unixnano>-<size>`，`weak=true`。
3. 保存时必须重新计算当前 token 后再对比。

远程驱动预留：

| 后端 | revision 来源 |
| --- | --- |
| WebDAV | ETag / Last-Modified |
| S3 / 对象存储 | ETag / VersionID / Generation |
| 网盘 SDK | 文件 revision / hash / update_time |

### 5. 保存流程

```text
PUT /api/fs/content
  -> Auth / PathGuard / 写限流
  -> handler decode JSON
  -> FileService.SaveContent
  -> 获取路径级锁
  -> storage.ContentEditor.WriteText
       -> 解析路径并确认普通文件
       -> 校验后端可写、文件大小、文本类型
       -> 重新读取当前 revision
       -> expected_revision 不匹配且 force=false：返回 ErrFileModified
       -> 写入同目录临时文件
       -> fsync / close
       -> rename 原子替换
       -> stat + 计算新 revision
  -> audit(action="edit", result="ok/fail")
  -> 返回新 revision
```

注意：进程内路径锁只能保护 flist 自身并发写；外部 SMB/NFS/SSH/容器进程仍可能修改文件，因此 revision 校验仍然必要。

**路径级锁复用现有 `util.PathLocker`**：项目已有 `internal/util/pathlock.go` 提供按 key（路径）互斥的 keyed mutex（引用计数、归零回收），且已被 `UploadService` 用于合并落盘的路径级串行化。本轮不新造轮子，直接把同一个 `*util.PathLocker` 实例注入 `FileService`：

1. 以规范化后的 API path 为 key 调用 `Lock` / `Unlock`，把 `SaveContent` 对同一文件的写串行化。
2. 与 `UploadService` 共享同一 locker 实例，使「分片合并落盘」与「文本保存」对同一目标路径也互斥，避免交错写。
3. `main.go` 构造一个 `util.NewPathLocker()` 同时传给 `FileService` 与 `UploadService`。

> 审计 action 新增 `edit` / `edit_conflict`。现有审计为结构化 slog（`h.audit(r, action, target, result)`），action 取值无固定枚举校验，直接传新值即可，无需额外白名单改造。

### 6. 路径级容量实现

现有 `storage.Usager` 可以继续复用。建议新增 service 方法：

```go
func (s *FileService) Space(ctx context.Context, apiPath string) (*model.SpaceResult, error)
```

行为：

1. 清理 API path。
2. 判断 backend 是否实现 `Usager`。
3. 支持时调用 `Usage(ctx, cleanedPath)`，计算 `used` 和 `used_percent`。
4. 不支持时返回 `supported=false`，而不是直接把整次请求作为错误。
5. 如果路径不存在，返回 `path_not_found`，不查询最近存在的父目录容量。路径级容量必须对应真实存在的当前目录或文件，避免展示与用户当前位置不一致的容量信息。

**`free` / `available` / `used` 口径（首期决策）**：

现有 `util.Usage` / `storage.Usager` 仅返回 `(total, free)`，其中 `free` 在 Linux 取的是 `Bavail`（非特权用户实际可写空间，已扣除 root 保留块），Windows 取调用方配额可用空间。首期不扩展 `Usager` 接口，按以下口径填充响应：

1. `space.free` 与 `space.available` 同值，均取 `Usager` 返回的 `free`（即 `Bavail` 语义）。
2. `space.used = total - free`，`space.used_percent = used / total * 100`（`total = 0` 时置 `0`）。
3. 该口径下的 `used` 表示「非特权用户视角的已用」，与 `df` 的 `Used` 可能有 root 保留块大小的差异；前端文案统一表述为「已用 / 剩余」，不承诺与 `df` 逐字节一致。
4. 若未来需要严格区分 `free`（`Bfree`）与 `available`（`Bavail`），再扩展 `Usager`（例如新增可选 `UsagerV2` 返回 `(total, free, avail)`），不阻塞本轮。

`Mux` 场景：

1. 对普通路径路由到命中的挂载点。
2. 对虚拟根 `/`，可以返回 `supported=false` 或聚合多个挂载点；首期建议 `supported=false`，避免不同后端容量口径混合。
3. 返回 mount 元信息，帮助前端展示“当前目录所在存储”。

## 前端实现方案

### 1. API 和类型

新增类型：

```ts
export interface FileRevision {
  token: string;
  weak: boolean;
}

export interface FileContent {
  path: string;
  name: string;
  size: number;
  mime: string;
  encoding: string;
  lineEnding: 'lf' | 'crlf' | 'mixed' | 'none';
  content: string;
  modTime: string;
  revision: FileRevision;
  editable: boolean;
  readonly: boolean;
}

export interface SpaceInfo {
  supported: boolean;
  total?: number;
  used?: number;
  free?: number;
  available?: number;
  usedPercent?: number;
}
```

`api.fs` 新增：

1. `content(path)`：读取可编辑文本。
2. `saveContent(req)`：保存文本。
3. `space(path)`：获取路径级容量。

### 2. 编辑器入口

新增 SPA 路由或页面状态：

```text
/editor?path=/notes/todo.md
```

右键菜单新增：

1. `编辑`：在当前窗口打开编辑器。
2. `新窗口编辑`：`window.open('/editor?path=...', '_blank', 'noopener')`。
3. 对目录和不可编辑文件不展示或置灰。

新窗口认证：

1. 当前 token 在 `localStorage`，同源新窗口可读取。
2. 不在 URL 中携带 token。
3. 如果后续切换到 HttpOnly Cookie，新窗口仍然可复用同源登录态。

### 3. 编辑器交互

编辑器应包含：

1. 文件名、路径、修改时间、大小展示。
2. 文本编辑区域。
3. 保存按钮和保存快捷键。
4. 未保存变更离开提醒。
5. 保存成功后更新 revision。
6. 保存冲突弹窗：
   - 重新加载远端内容。
   - 强制覆盖。
   - 复制当前编辑内容后取消。
7. 文件过大、非文本、只读后端时展示不可编辑原因。

首期直接引入轻量级代码编辑器，推荐优先使用 CodeMirror 6：它比 Monaco 更轻，支持行号、基础语法高亮、快捷键和主题定制，适合 flist 的“轻量文本编辑”定位。Monaco 更接近完整 IDE，体积和集成复杂度更高，暂不作为首选。

### 4. 容量展示刷新

侧边栏容量展示改为基于当前路径：

```text
currentPath 变化
  -> api.fs.space(currentPath)
  -> 更新底部容量状态栏
```

同时在以下操作成功后刷新：

1. 上传完成。
2. 删除完成。
3. 复制完成。
4. 移动完成。
5. 编辑保存完成。

建议加 5–15 秒短缓存或请求合并，避免快速切目录时频繁查询。

## 兼容与降级

1. 旧的 `GET /api/fs/preview` 保持不变，仍用于预览。
2. `GET /api/system/info` 本轮改为纯系统信息，不再承载磁盘字段；路径容量统一由 `GET /api/fs/space` 提供。
3. 新前端优先使用 `GET /api/fs/space`，失败或 `supported=false` 时隐藏容量状态栏或显示“容量不可用”。
4. 驱动不支持 `ContentEditor` 时，编辑入口置灰或接口返回 `not_supported`。
5. 保存时 revision 冲突返回 `409`，不自动覆盖。
6. 前端新窗口打开失败时，不影响当前窗口编辑。
7. 回滚后新增接口不可用，但不影响旧的浏览、预览、下载和文件操作。

## 可观测性

### 日志

新增审计日志：

| action | path | result | 说明 |
| --- | --- | --- | --- |
| `edit` | 文件路径 | `ok` / `fail` | 文本保存 |
| `edit_conflict` | 文件路径 | `fail` | revision 冲突 |

建议普通请求日志中保留 request id、用户、IP、耗时。

### 可选指标

如果后续引入 metrics，可以记录：

1. 文本读取次数、失败次数。
2. 文本保存次数、冲突次数、失败次数。
3. 容量查询次数、失败次数、不支持次数。
4. 编辑文件大小分布。

## 测试计划

### 后端单元测试

1. `ReadText`：
   - UTF-8 文本成功读取。
   - 二进制文件返回 `unsupported_media_type`。
   - 超过大小上限返回 `file_too_large`。
   - 目录路径返回 `not_a_file`。
   - 越界路径被拦截。
2. `WriteText`：
   - revision 匹配保存成功，并返回新 revision。
   - revision 不匹配返回 `file_modified`。
   - `force=true` 可覆盖当前版本。
   - 保存后内容完整，mtime / size 更新。
   - 并发保存同一路径不会产生交错写。
3. `Space`：
   - 本地路径返回非零 total/free。
   - 驱动不支持返回 `supported=false`。
   - Mux 子挂载路径路由到对应后端。
   - 虚拟根 `/` 不聚合时返回 `supported=false`。

### 后端集成测试

1. 未登录访问 `fs/content` 和 `fs/space` 的认证行为。
2. `PUT /api/fs/content` 受写限流和 PathGuard 保护。
3. 冲突时 HTTP 状态为 `409`，响应 code/message 正确。
4. 保存成功写审计日志。

### 前端手动验证

1. 右键文本文件，点击“编辑”，当前窗口打开编辑器。
2. 右键文本文件，点击“新窗口编辑”，新窗口打开同一文件编辑器。
3. 修改保存成功后，刷新预览 / 下载内容确认已更新。
4. 两个窗口同时打开同一文件，窗口 A 保存后，窗口 B 保存应出现冲突提示。
5. 打开二进制文件时，不展示编辑入口或展示不可编辑提示。
6. 切换不同目录，容量状态栏随路径更新。
7. Docker 多 bind mount 场景下，不同目录展示不同文件系统容量。

## 上线与回滚

1. 不需要数据库迁移。
2. 新增 `fs/content` / `fs/space` 接口不影响旧的文件浏览、预览、下载和写操作；但 `system/info` 的磁盘字段会在本轮移除，前端需同步改用 `fs/space`。
3. 不需要独立的文本编辑功能开关，默认允许文本编辑；回滚或禁用依赖版本回退或调整 `--max-edit-size`。
4. 回滚到旧版本后，已编辑保存的文件就是普通文件内容，不存在数据结构兼容问题。
5. 如果前端先上线但后端接口不存在，应在 `api.fs.content/space` 失败时展示降级提示，不影响基础浏览。

## 建议实施顺序

1. 新增后端模型、错误码和配置项。
2. 在 storage 层增加 `ContentEditor` 可选接口，并由 `local` 驱动实现文本读取、revision、原子保存。
3. 在 service 层新增 `ReadContent`、`SaveContent`、`Space`。
4. 在 handler / routes 中新增 `GET /api/fs/content`、`PUT /api/fs/content`、`GET /api/fs/space`。
5. 补齐后端测试。
6. 前端新增 API 类型和调用。
7. 前端引入 CodeMirror 6 作为轻量级代码编辑器，完成编辑器页面和右键菜单入口。
8. 前端容量状态栏切换到路径级容量，并在写操作后刷新。
9. 端到端验证冲突、新窗口、Docker 多磁盘容量场景。

## 风险与已确认决策

### 风险

1. **大文件编辑体验风险**：即使后端限制大小，浏览器中编辑数 MB 文件仍可能卡顿；本轮默认上限为 `5MiB`。
2. **编码兼容风险**：首期只支持 UTF-8 / UTF-8 BOM，GBK / GB18030 文件会被判定为不可编辑或读取失败。
3. **外部进程并发写风险**：revision 可以发现保存前的变化，但无法阻止保存过程后外部进程再次写入；这是 NAS 场景可接受的乐观锁边界。
4. **弱 revision 风险**：如果某些后端只能提供 mtime/size，可能存在极小概率漏检，需要通过 `weak=true` 显式标记。
5. **容量口径差异**：Linux 的 `free`、`available` 和 root reserved blocks 语义不同，前端展示文案需要避免过度承诺。

### 已确认决策

1. `max-edit-size` 默认值使用 `5MiB`。
2. 首期仅支持 UTF-8 / UTF-8 BOM，暂不支持 GBK / GB18030。
3. 不增加独立文本编辑开关，默认允许编辑。
4. `GET /api/fs/space` 对不存在路径返回 `path_not_found`，不查询最近存在的父目录容量。
5. `GET /api/system/info` 本轮直接改为纯系统信息，不再保留磁盘字段。
6. 编辑器首期直接引入轻量级代码编辑器，优先使用 CodeMirror 6，而不是原生 `textarea` 或 Monaco。
7. 新增错误码从 `2012` 起续号（`2001`–`2011` 已被占用，其中 `2011` 已是 `upload_incomplete`），冲突响应 `code` 使用 `2012`。
8. 保存时服务端对 `content` 字节原样写入，不做行尾转换；`encoding` / `line_ending` 仅用于读取探测与前端展示。
9. 首期不扩展 `Usager` 接口，`space.free` 与 `space.available` 同值（均为 `Bavail` 语义），`used = total - free`。
10. 新增 `Caps.Edit` 与 `ContentEditor` 配对，遵循现有「Caps 标志位 + 可选接口」约定。
11. 路径级写串行化复用现有 `util.PathLocker`（已被 `UploadService` 使用），与上传合并共享同一实例注入 `FileService`，按规范化 API path 加锁，仅用于 `SaveContent`。

# 在线解压功能方案概述

## 背景

flist 已经支持文件浏览、上传、复制/移动/删除、在线预览和“打包下载”，但压缩包仍需先下载到客户端、在本地解压、再上传回服务器。对 NAS、VPS 和移动设备上的大文件，这条链路会重复消耗网络、客户端磁盘和时间。

本次增加服务端在线解压能力：用户在文件右键菜单直接点击“解压”，服务端在后台完成解压，前端沿用现有传输面板展示进度，用户可以继续浏览文件。

## 当前现状

### 文件操作主链路

```text
FileBrowser 右键菜单
  -> fsStore / fileOpStore
  -> POST /api/fs/op/{copy|move|delete}
  -> FileOpHandler
  -> FileOpService 单 worker 队列
  -> storage.Backend / ProgressCopier
  -> SSE /api/fs/op/progress
  -> TransferPanel
```

关键位置：

| 层 | 当前实现 |
| --- | --- |
| 右键菜单 | `frontend/src/components/FileBrowser.tsx` 的 `menuItems()` |
| 异步任务状态 | `frontend/src/fileOpStore.ts`，支持 SSE、取消、localStorage 恢复 |
| 进度 UI | `frontend/src/components/TransferPanel.tsx` 的 `FileOpRow` |
| API | `internal/handler/fileop.go`、`internal/server/routes.go` |
| 后台队列 | `internal/service/fileop.go`，copy/move/delete 共用单 worker |
| 存储抽象 | `internal/storage/backend.go`；写文件使用可选 `StreamWriter` |
| 本地原子写 | `internal/storage/local/content.go` 的 `OpenWrite` |
| 虚拟命名空间 | `internal/storage/mux.go`，将 `/files`、`/drive` 路由到具体后端 |

### 已有可复用能力

1. `FileOpService` 已有排队、任务属主隔离、取消、进度节流、速率计算、终态保留和清理机制。
2. `TransferPanel` 已有 queued/running/done/error/canceled 状态和确定/不确定进度条。
3. `storage.Backend.Open` 可流式读取源压缩包，`StreamWriter.OpenWrite` 可原子写入单个输出文件。
4. `Backend.Mkdir/Move/Remove` 可用于构建目录、成功提交和失败回滚。
5. `Mux` 已能路由绝大多数文件操作，但当前尚未向顶层暴露 `StreamWriter.OpenWrite`。

### 当前不足

1. 没有压缩格式识别、归档条目解析和安全落盘逻辑。
2. `FileOpKind` 仅包含 copy/move/delete，没有 extract。
3. 右键菜单不会识别可解压文件。
4. 顶层 `Mux` 未实现 `StreamWriter`，service 无法通过统一虚拟路径写解压文件。

## 目标

1. 根据文件名大小写不敏感地识别 `.zip`、`.rar`、`.tar.gz`。
2. 单个受支持压缩文件的右键菜单展示“解压”，点击后立即创建后台任务。
3. 默认输出到压缩包所在目录的同名文件夹：
   - `photos.zip` -> `photos/`
   - `backup.rar` -> `backup/`
   - `logs.tar.gz` -> `logs/`
4. 同名目录已存在时不覆盖、不合并，自动使用 `name (2)`、`name (3)`，保持“一次点击即可执行”。
5. 沿用现有传输面板，展示排队、解压进度条、完成、失败和取消状态。
6. 支持取消；失败或取消不保留本次任务的半成品。
7. 防止 Zip Slip/Tar Slip、符号链接逃逸、压缩炸弹占满磁盘等常见风险。
8. 保持单二进制交付，不依赖系统安装 `unrar`、`tar` 或 `unzip`。
9. 对 `/files` 和 `/drive/<id>` 下的可写本地存储保持一致行为。

## 非目标

1. 本期不支持用户自选输出目录或自定义输出文件夹名称。
2. 本期不支持一次批量解压多个压缩包。
3. 本期不支持带密码的 ZIP/RAR。
4. 本期不支持 RAR 分卷、ZIP 分卷或拆分 tar 包。
5. 本期不恢复压缩包中的 owner、group、权限、ACL、xattr 或精确时间戳。
6. 本期不创建归档内的符号链接、硬链接、设备文件、FIFO 等特殊条目。
7. 本期不增加新的视觉体系；严格复用现有右键菜单和传输面板样式。

## 已采用的默认决策

用户已要求“详细计划后直接执行”。下列未明确细节采用安全且符合现有产品习惯的默认值，不阻塞实现：

1. 输出冲突采用自动避让，不覆盖/合并已有目录。
2. 解压失败或取消采用整任务回滚，而不是保留已经成功的部分文件。
3. 加密包、分卷包返回明确失败，不弹出密码或分卷选择 UI。
4. 归档中的链接和特殊文件跳过；普通文件和目录正常解压。
5. 最多处理 100,000 个归档条目，RAR 解码字典上限 128 MiB，防止不受控资源占用。

## 核心设计

### 总体流程

```text
右键“解压”
  -> POST /api/fs/op/extract { path }
  -> 202 task_id
  -> FileOpService 串行 worker
      -> 按文件名选择 ZIP / RAR / TAR.GZ reader
      -> 创建同目录隐藏临时根 .flist-extract-<task-id>
      -> 校验每个归档条目路径与类型
      -> 目录逐层创建，普通文件流式原子写入
      -> SSE 上报进度
      -> 成功：隐藏临时根 Move 为 name[/ name (N)]
      -> 失败/取消：Remove 隐藏临时根
  -> TransferPanel 更新状态
  -> 完成后刷新压缩包所在目录
```

### 为什么复用 FileOpService

| 方案 | 优点 | 缺点 | 结论 |
| --- | --- | --- | --- |
| 扩展现有 FileOpService | 复用队列、SSE、取消、恢复和 UI；NAS I/O 继续串行 | 需让任务模型支持 extract | 采用 |
| 新建 ExtractService + 新进度协议 | 边界独立 | 重复状态机、SSE、恢复和面板逻辑 | 不采用 |
| 同步 HTTP 请求等待解压完成 | 实现短 | 长请求易超时，阻塞交互，无法可靠取消/恢复 | 不采用 |
| 调用系统 `unzip/tar/unrar` | 格式兼容成熟 | 破坏零外部依赖和跨平台单二进制目标 | 不采用 |

### RAR 实现选择

ZIP 与 TAR.GZ 使用 Go 标准库。RAR 使用纯 Go 的 `github.com/nwaples/rardecode/v2`，固定到经测试的版本；该库提供顺序 `Reader`、RAR 4/5 解码、加密/分卷错误识别，并采用 BSD-2-Clause 许可证：

- https://pkg.go.dev/github.com/nwaples/rardecode/v2
- https://github.com/nwaples/rardecode

不使用外部 `unrar` 命令，因此部署模型不变。

### 输出提交策略

不直接写最终目录，而采用“隐藏临时目录 + 成功改名”：

```text
/files/docs/data.zip
/files/docs/.flist-extract-<task-id>/   # 解压期间
/files/docs/data/                        # 全部成功后原子 Move
```

收益：

1. 用户默认列表中看不到逐步出现的半成品。
2. 任一条目失败或任务取消时，只需递归删除本任务唯一临时根。
3. 单个输出文件仍由 `StreamWriter` 临时文件 + rename 原子落盘。
4. 最终目录名发生并发冲突时，可重新探测 `name (N)` 后提交。

### 进度口径

不同格式能获得的信息不同：

| 格式 | 总量与进度口径 | 原因 |
| --- | --- | --- |
| ZIP | 所有普通文件的 uncompressed size；按实际输出字节累计 | 中央目录可在解压前低成本获得完整大小 |
| TAR.GZ | 压缩包文件大小；按 gzip 底层已读取压缩字节累计 | 若先扫描所有 tar header 会完整解压两次，代价过高 |
| RAR | 压缩包文件大小；按 RAR reader 已读取压缩字节累计 | 顺序/solid RAR 预扫描可能需要实际解码，避免双倍成本 |

前端对 extract 只展示“百分比 + 当前阶段/文件名”，不把不同格式的字节口径混写成“已解压大小”。任务完成时统一拉满到 100%。

## 安全设计

### 路径安全

每个归档条目在写入前执行：

1. 将 `\` 归一为 `/`。
2. 拒绝 NUL、绝对路径、Windows 盘符路径。
3. `path.Clean` 后拒绝 `..` 和 `../` 前缀。
4. 对每个路径段执行平台文件名校验。
5. 拼接后再次断言目标位于任务临时根之下。

任何不安全条目都使整个任务失败并回滚，不做“尽力跳过”，避免用户误以为归档已完整可信地解压。

### 链接与特殊文件

1. ZIP 中带 symlink mode 的条目跳过。
2. TAR 的 symlink、hardlink、char/block device、FIFO 等条目跳过。
3. RAR 的所有 link/redirection 条目跳过。
4. 不跟随、不创建链接，因此链接目标无法逃逸输出根。

### 资源限制

1. 条目数量上限 100,000，超过后失败并回滚。
2. 若目标后端提供 `Usager`，累计实际输出字节不得超过任务开始时的可用空间。
3. 底层写入遇到 `ENOSPC` 继续映射为 `disk_full`。
4. RAR `MaxDictionarySize` 设为 128 MiB，避免恶意 header 申请超大解码字典。
5. 所有复制循环检查任务 `context`，允许用户取消 CPU/I/O 工作。
6. 文件按流式方式处理，内存不随压缩包或输出文件大小线性增长。

## 协议设计

### 新增发起接口

```http
POST /api/fs/op/extract
Content-Type: application/json

{
  "path": "/files/archive/photos.zip"
}
```

成功沿用现有 202 任务句柄：

```json
{
  "code": 0,
  "message": "accepted",
  "data": {
    "task_id": "...",
    "op": "extract",
    "total_items": 1,
    "total_bytes": 52428800
  }
}
```

### SSE 与状态机

沿用 `/api/fs/op/progress?id=<task_id>` 和现有事件结构，不新建协议。

```text
queued -> running -> done
                  -> canceled
                  -> error
```

`op` 新增值 `extract`。解压任务只有一个顶层 item（源压缩包）；归档内部文件不展开为详情列表，避免大型归档向前端发送数万条事件。

运行期错误通过现有 `snapshot.error` / `results[0].error` 传递：

| 错误名 | 含义 |
| --- | --- |
| `unsupported_archive` | 扩展名不支持或压缩方法不支持 |
| `archive_encrypted` | 归档需要密码 |
| `archive_multivolume` | 分卷归档 |
| `archive_unsafe_path` | 条目路径可能逃逸 |
| `archive_too_many_entries` | 超过 100,000 个条目 |
| `archive_corrupt` | 格式损坏、校验失败或内容与扩展名不匹配 |
| `disk_full` | 可用空间不足或底层写满 |
| `not_supported` | 目标存储不支持流式写入 |
| `canceled` | 用户取消 |

发起阶段的非法 path、非普通文件和不支持扩展名仍走统一 HTTP 错误信封。

## 前端交互

### 右键菜单

在单个普通、可达文件上：

1. 文件名匹配 `.zip`、`.rar`、`.tar.gz` 时，在“下载”之后展示“解压”。
2. 图标使用现有 `lucide-react` 的 `ArchiveRestore`，尺寸保持 `w-4 h-4`。
3. 多选、目录、不可达符号链接和其他扩展名不展示该入口。
4. 点击后不弹配置框，直接发起任务并打开现有传输面板。

### 传输面板

严格复用 `FileOpRow` 的布局、字号、颜色、暗色模式、按钮和进度条：

1. 标题：`解压 · archive.zip`。
2. queued：原有灰色不确定进度条。
3. running：蓝色确定进度条；总量暂不可用时回退原有不确定动画。
4. done/error/canceled：复用现有状态图标与移除/取消按钮。
5. running 图标使用 `ArchiveRestore`，不新增样式 token。
6. 完成后刷新源压缩包所在目录，以显示新文件夹。

## 兼容与降级

| 场景 | 行为 |
| --- | --- |
| 旧前端/旧客户端 | 原 copy/move/delete 与 SSE 字段不变；新增枚举值只在新接口产生 |
| 后端没有 `StreamWriter` | 任务失败为 `not_supported`，不产生最终目录 |
| 目标同名目录存在 | 自动使用 `name (2)`，不覆盖、不合并 |
| 单条输出写失败 | 整个隐藏临时根回滚 |
| SSE 断线/页面刷新 | 沿用 localStorage task_id 恢复和 EventSource 重连 |
| 服务进程在任务中崩溃 | 可能遗留 `.flist-extract-*` 隐藏临时目录；不会出现伪装成完整结果的最终目录 |
| 加密/分卷包 | 明确失败并回滚；不请求密码或其他卷 |
| 链接/特殊条目 | 跳过，其他普通内容继续 |

## 可观测性

沿用 `fileop finished` 结构化日志，并增加：

1. `op=extract`、`task_id`、`status`、`user`。
2. 成功时记录 `archive_format`、`entries`、`output_path`、`written_bytes`。
3. 失败时记录错误类别；不记录文件内容和归档内数据。
4. SSE 快照可观察 status、percent、speed 和终态错误。

## 上线与回滚

1. 不需要数据库迁移或配置迁移。
2. 新接口和新前端入口为增量能力，不改变旧 API。
3. 回滚代码即可；已成功解压的普通目录不依赖新版本，可继续访问。
4. 回滚前若仍有任务运行，正常优雅关闭不能保证后台任务完成；临时目录保持隐藏且不会被当成完整结果。
5. 新增一个纯 Go 依赖，需要在 Linux/Windows 交叉构建中验证。

## 测试计划

### 单元测试

1. 文件名格式识别：大小写、`.tar.gz` 双扩展、错误扩展。
2. 输出目录名推导及冲突避让。
3. 条目路径校验：`../`、绝对路径、反斜杠穿越、盘符、NUL、合法 Unicode。
4. ZIP：普通文件、嵌套目录、空目录、进度、损坏包、Zip Slip、取消回滚。
5. TAR.GZ：普通文件、隐式目录、空目录、特殊条目跳过、Tar Slip、损坏包。
6. RAR：至少使用测试夹具验证普通文件；加密/分卷或库错误映射可用构造错误单测覆盖。
7. 条目数量限制、可用空间预算、失败清理。
8. `Mux.OpenWrite` 正确路由与不支持后端降级。

### Handler/路由测试

1. 未认证返回 401。
2. 空 path、目录、错误扩展返回 400/对应文件错误。
3. 合法请求返回 202、`op=extract`，SSE 最终完成。
4. task 属主隔离与取消沿用既有覆盖。

### 前端与构建验证

1. `npm run lint` TypeScript 通过。
2. `npm run build` 成功。
3. `go test ./...`、`go vet ./...` 通过。
4. Linux 与 Windows 后端交叉编译至少各验证一次。

### 手动验证

1. 三种格式分别右键解压，输出目录名正确。
2. 同名目录已存在时得到 `(2)`，已有内容不变。
3. 大包进度条持续推进，浏览其他目录不受阻塞。
4. 中途取消后无最终目录、无可见半成品。
5. 刷新页面后任务进度恢复。
6. 暗色/亮色、列表/网格模式的右键入口和面板样式与现有元素一致。

## 建议实施顺序

1. 补充存储层 `Mux.OpenWrite` 路由能力。
2. 新增归档识别、安全路径、流式写入和三种 decoder。
3. 扩展 FileOp model/service/handler/routes 为 extract。
4. 增加 service、handler、storage 测试。
5. 扩展前端类型、API、store 和任务恢复逻辑。
6. 在右键菜单增加入口，在 TransferPanel 增加 extract 文案与图标。
7. 完成 TypeScript、Go、构建和手动行为验证。

## 风险与后续问题

### 已处理风险

1. 路径穿越：严格校验并限制在临时根。
2. 半成品：隐藏临时根 + 成功提交 + 失败回滚。
3. 磁盘耗尽：可用空间预算 + 底层 ENOSPC。
4. RAR 内存：字典上限。
5. 大任务阻塞：后台串行队列 + SSE + cancel。

### 非阻塞后续项

1. 密码输入与加密包解压可作为独立需求设计，避免在 task/localStorage 中泄漏密码。
2. 分卷 RAR/ZIP 需要卷发现、权限和缺卷交互，后续单独设计。
3. 可增加启动时清理陈旧 `.flist-extract-*` 目录的机制。
4. 可增加“解压到…”和覆盖/合并策略，但本期保持一键、安全默认值。

本期没有会改变代码结构或接口方向的待确认阻塞项。

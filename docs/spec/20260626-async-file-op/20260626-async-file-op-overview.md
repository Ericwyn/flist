# 异步文件操作任务方案概述

## 背景

flist 的复制 / 移动 / 删除是同步 HTTP 接口：前端发起 `POST /api/fs/copy`，后端在同一个请求里串行完成所有项，全部成功后才返回结果。对于小文件这没有问题，但当用户在 NAS 上复制多个大文件（例如线上场景一次复制 15 个 multi-GB 的 `.MOV` 原片到另一个目录）时，请求会阻塞数分钟甚至更久，期间：

1. 页面没有任何反馈，用户以为浏览器 / 服务卡死；
2. 用户无法继续浏览其他目录，操作与导航互相阻塞；
3. 任何网络抖动导致连接中断，整个批量操作结果丢失，无法判断完成了几项；
4. 浏览器 / 反向代理对单请求的超时会直接杀掉一个尚未完成的大批量复制。

上传 / 下载已经有成熟的「传输面板」实时进度展示（分片上传 + 流式 zip 下载，前端 EMA 速率）。本次把 copy / move / delete 同样改造为**后台任务 + 实时进度**，复用传输面板，让大文件操作可见、可取消、不阻塞浏览。

## 当前现状

### 文件操作主链路（同步）

```text
前端 fsStore.paste / removePaths
  -> api.fs.copy / move / remove   (POST /api/fs/copy|move, DELETE /api/fs/delete)
  -> handler.FileHandler.Copy / Move / Delete
  -> service.FileService.Copy / Move / Delete
    -> statDir(dst)              探测 dst 是否已存在目录
    -> transferTarget(...)       计算 dst/basename 落点（autoRename 时 avoidConflict 探测 name (n).ext）
    -> checkSpace (仅 copy)      驱动实现 Usager 时预检磁盘空间
    -> backend.Copy / Move / Remove  逐项串行，尽力而为
  -> 返回 OpResult[]（每项 ok/error）
  -> 前端 refresh() 刷新当前目录
```

### 关键文件与数据结构

| 层 | 文件 | 关键结构 / 函数 |
| --- | --- | --- |
| Util | `internal/util/fsop.go` | `CopyPath`、`MovePath`、`RemovePath`、`copyFile`（`io.Copy`） |
| Storage | `internal/storage/backend.go` | `Backend.Copy/Move/Remove` 接口、错误词表 |
| Local | `internal/storage/local/local.go` | `Local.Copy/Move/Remove`，委托 `util.CopyPath/MovePath` |
| Service | `internal/service/file.go` | `FileService.Copy/Move/Delete`、`transferTarget`、`avoidConflict`、`checkSpace`、`treeSize` |
| Handler | `internal/handler/file.go` | `FileHandler.Copy/Move/Delete`，`OpResult` 批量返回 |
| 错误码 | `internal/handler/errors.go` | 2001-2016、4000、9001-9002 |
| 路由 | `internal/server/routes.go` | 写操作组（套写限流 10/s） |
| 前端 API | `frontend/src/lib/api.ts` | `api.fs.copy/move/remove` |
| 前端 Store | `frontend/src/fsStore.ts` | `paste`、`removePaths`，`await` 后 `refresh()` |
| 传输面板 | `frontend/src/components/TransferPanel.tsx` | 仅上传 / 下载分区 |
| 进度 Store | `frontend/src/uploadStore.ts`、`downloadStore.ts` | EMA 速率、任务状态机 |

### 上传 / 下载既有能力（可复用）

- 上传：`UploadService` 内存会话表 + 后台清理 sweep，前端分片并发 + EMA 速率 + 传输面板 `UploadRow`。
- 下载：`handler.Archive` 流式 zip（无 Content-Length），前端 `fetch` + `getReader()` 边读边回调 `onProgress(received)`，面板 `DownloadRow` 不确定态进度条。
- 鉴权：`middleware.ExtractToken` 支持 `Authorization: Bearer` 头**或** `flist_session` Cookie（登录 handler `SetCookie`），下载 / 媒体内联已依赖 Cookie 鉴权。

### 当前问题点

1. copy/move/delete 同步阻塞，大文件操作期间 UI 假死；
2. 无项内字节进度，大文件复制只能看到「请求 pending」，无法判断是否真的在跑；
3. 操作与当前目录强绑定（`await refresh()`），用户必须停留在原地等待；
4. 传输面板只覆盖上传 / 下载，文件操作没有可视化载体。

## 目标

1. copy / move / delete 改为异步任务：请求立即返回 `task_id`（HTTP 202），不阻塞；
2. 通过 SSE 实时推送进度，含**项内字节级进度**（已复制字节 / 总字节 + EMA 速率）和项级 / 总级进度；
3. 复用传输面板展示文件操作任务，与上传 / 下载统一；
4. 任务与当前目录解耦，用户可在任务运行时自由浏览其他目录，完成后按相关性刷新；
5. 支持任务取消（`ctx` 取消，复制中途清理半成品）；
6. 保留旧的同步 `/api/fs/copy|move|delete` 接口不变，前端切换到新异步接口。

## 非目标

1. 不持久化任务状态（内存态，服务重启未完成任务即丢失，与上传会话一致策略）；
2. 不做任务断点续传（复制不同于上传，无文件指纹复用语义；重启需重新发起）；
3. 不做任务历史记录持久化（仅内存保留 10 分钟供断线重连取最终结果）；
4. 不改造删除的字节级进度（递归删除按项级 done 推送即可，单文件删除很快，目录删除项数即粒度）；
5. 不做并发并行（全局串行执行，NAS 机械盘避免磁头抖动）；
6. 不改 `rename`（单文件重命名仍走同步 `move`，瞬时完成无需异步化）。

## 核心设计

### 总体原则

把 copy / move / delete 从「请求内同步执行」改为「请求创建后台任务 + SSE 订阅进度」，与上传 / 下载的传输面板模式对齐。业务规则（落点判定 / 自动避让 / 空间预检）在异步路径上**复用 `FileService` 的导出方法**，保证与同步路径语义完全一致，不重复实现。

### 任务执行模型

```text
POST /api/fs/op/{copy,move,delete}
  -> FileOpService.Start           创建 task，入 jobs 队列，立即返回 task_id（202）
  -> 单一 worker goroutine 串行消费 jobs（FIFO，全局串行）
     -> runTask
        -> execTransfer / execDelete
           -> 逐项：TransferTarget(复用) -> CheckSpace(复用, 仅 copy)
                  -> ProgressCopier.CopyWithProgress / MoveWithProgress（项内字节进度回调）
                  -> 或回退 backend.Copy/Move（驱动不支持 ProgressCopier 时仅项级进度）
           -> 每项推 item_start / item_progress / item_done
     -> finishTask 推 finished（携带 OpResult[]）
     -> 任务保留 10min 后 Sweep 清理

GET /api/fs/op/progress?id=<task_id>   (SSE, 依赖 Cookie 鉴权)
  -> FileOpService.Subscribe          订阅事件流
  -> 先推一条 snapshot（断线重连恢复）
  -> 循环推 item_start/item_progress/item_done
  -> finished 后关闭流
  -> 15s 心跳注释防代理超时

POST /api/fs/op/cancel {task_id}       幂等取消
```

### 方案取舍

| 决策点 | 选择 | 理由 |
| --- | --- | --- |
| 接口兼容 | 新增异步接口，保留旧同步接口 | 用户确认；旧接口无前端调用但保留便于脚本 / 兼容 |
| 进度通道 | SSE（`GET`，`text/event-stream`） | 单向推送天然契合进度流；浏览器原生 `EventSource` 支持断线自动重连；复用既有 Cookie 鉴权（`EventSource` 无法带 `Authorization` 头） |
| 并发模型 | 单一 worker 全局串行 | NAS 机械盘场景避免磁头抖动；FIFO 公平；用户确认 |
| 进度粒度 | 项内字节级 | 多 GB 单文件必须看到字节进度才有意义；通过 `io.Copy` writer 包装实现 |
| 任务状态存储 | 内存 map，随进程生命周期 | 与上传会话一致；不引入持久化复杂度 |
| 已完成任务保留 | 10 分钟 TTL | 覆盖断线重连窗口，超期 Sweep 清理 |
| 业务规则复用 | 导出 `FileService.StatDir/TransferTarget/CheckSpace/TreeSize` | 避免在异步路径重复实现落点 / 避让 / 空间语义，防止与同步路径偏离 |
| 进度回调注入 | 新增 `storage.ProgressCopier` 可选接口 | 驱动可选实现，不支持时回退普通 `Copy/Move`（仅项级进度） |
| 速率计算位置 | 服务端 EMA | SSE 是单向通道，服务端算好速率随快照推送，前端直接展示 |
| 进度节流 | 200ms 最小间隔 | 防高频 `Write` 回调打满 SSE；大文件 200ms 仍足够平滑 |

### 替代方案不选原因

| 方案 | 不选原因 |
| --- | --- |
| 轮询 `GET /api/fs/op/status?id=` | 实时性差、流量浪费；SSE 更契合单向流且原生支持重连 |
| WebSocket | 双向能力对本场景过剩；SSE 更轻、HTTP 友好、自带重连 |
| 把同步接口直接改异步（不保留旧接口） | 破坏既有调用方；用户要求保留 |
| 并发并行执行多任务 | NAS 机械盘场景会加剧寻道抖动，反而更慢 |
| 任务持久化到 SQLite | 引入迁移与状态机复杂度；上传会话也未持久化，保持一致 |

## 数据结构 / 协议设计

### 新增接口

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| POST | `/api/fs/op/copy` | 发起异步复制，202 返回 `task_id` |
| POST | `/api/fs/op/move` | 发起异步移动 |
| POST | `/api/fs/op/delete` | 发起异步删除 |
| GET | `/api/fs/op/progress?id=` | SSE 订阅任务进度 |
| POST | `/api/fs/op/cancel` | 取消任务（幂等） |

发起接口请求体与旧同步接口一致（`{src[], dst, auto_rename}` / `{paths[]}`），便于前端复用。

### 任务句柄（202 响应）

```json
{
  "code": 0,
  "message": "accepted",
  "data": {
    "task_id": "XntQAPsgakAobC8...",
    "op": "copy",
    "total_items": 15,
    "total_bytes": 48318382080
  }
}
```

`total_bytes` 为 best-effort 估算（`TreeSize` 递归 stat 求和），失败置 0，不阻断任务。

### SSE 事件

每条事件为 `data: <json>\n\n`，json 结构：

```json
{
  "type": "item_progress",
  "snapshot": {
    "op": "copy", "status": "running",
    "total_items": 15, "total_bytes": 48318382080,
    "done_items": 3, "done_bytes": 9663676416,
    "cur_index": 3, "cur_name": "P1013675.MOV",
    "cur_size": 3221225472, "cur_copied": 1610612736,
    "speed": 52428800, "started_at": "2026-06-26T23:11:50Z"
  },
  "index": 3, "copied": 1610612736
}
```

事件类型：

| type | 触发时机 | 携带 |
| --- | --- | --- |
| `snapshot` | 订阅建立时立即推一条（断线重连恢复） | 当前快照 |
| `item_start` | 开始处理某项 | index、name、size |
| `item_progress` | 项内字节推进（节流 200ms） | index、copied |
| `item_done` | 某项完成（成功 / 失败） | index、ok、error |
| `finished` | 任务终态（done / canceled / error） | 快照含 status、results[]、error |

### 新增错误码

| 常量 | 值 | 含义 |
| --- | --- | --- |
| `CodeFileOpNotFound` | 2017 | 任务不存在（已过期或 ID 非法） |
| `CodeFileOpBusy` | 2018 | 操作队列已满（排队任务超过 32） |

### 任务状态机

```text
queued（已入队，等待串行槽）
  -> running（worker 开始执行）
     -> done（全部项处理完，可能有单项失败，看 results）
     -> canceled（用户取消，ctx 取消中断复制；已完成项保留，未触达项标 skipped）
     -> error（整体失败，如目标非法）
```

> `results[]` 始终覆盖全部 `totalItems`：成功项 `ok:true`，失败项带 `error` 码名，取消的中断项 `error:"canceled"`，未触达项 `error:"skipped"`。前端详情 Modal 据此展示完整明细。

## 兼容与降级

| 场景 | 处理方式 |
| --- | --- |
| 旧同步接口 `/api/fs/copy|move|delete` | 完全保留不变，不受影响 |
| 驱动不支持 `ProgressCopier` | `FileOpService` 用类型断言探测，回退 `backend.Copy/Move`，仅项级进度无字节进度 |
| 队列满（>32 排队） | `Start` 返回 `ErrFileOpBusy`（2018），前端展示「操作队列已满，请稍后再试」 |
| SSE 客户端断线 | 浏览器 `EventSource` 自动重连；任务在 10min TTL 内可恢复快照，超期返回 2017 |
| 任务运行中服务重启 | 内存态丢失，未完成任务无最终结果（与上传会话一致；复制中途的半成品由驱动在 `ctx` 取消 / 错误时清理） |
| 取消时复制中途 | `progressWriter` 检测 `ctx.Err()` 中止 `io.Copy`，`copyFile` 清理半成品 dst，跨盘 move 的 `movePath` 回退分支清理半成品。**已完成项保留**（与 OS 习惯一致），仅停止后续工作；未触达项标记 `skipped`，`results[]` 覆盖全部 `totalItems` 供详情面板展示 |
| 同盘 move（rename 瞬时） | 不产生 `item_progress`，仅 `item_start` -> `item_done`，符合预期 |
| 反向代理缓冲 | SSE 响应设 `X-Acc-Buffering: no` + 15s 心跳注释，关闭 nginx 缓冲 |

## 可观测性

1. 关键日志（结构化 slog）：
   - 任务完成：`logger.Info("fileop finished", task_id, op, status, items, user)`
2. 调试日志：发起任务时记录 op / srcs 数 / totalBytes
3. 不记录文件内容、不记录敏感路径之外的信息
4. 任务状态可通过 `Get(taskID)` 查询快照（便于排查）

## 测试计划

### 单元 / 集成测试（`internal/service/fileop_test.go`）

1. `TestFileOpCopyProgress` — 复制两文件，验证 `finished` 携带 `OpResult[]`、`DoneItems` 正确、落盘成功
2. `TestFileOpDelete` — 删除任务项级 `item_done` 与 `finished` 正确
3. `TestFileOpCancel` — 取消请求使任务进入 `canceled` 终态
4. `TestFileOpCopyProgressSlow` — 关闭节流后验证 `item_progress` 确实推送（证明字节回调链路通畅）
5. `TestFileOpGetMissing` — 不存在任务 `Get`/`Subscribe` 返回 false/nil
6. `TestFileOpOwnerIsolation` — 非属主无法订阅 / 取消他人任务（not-found 语义）
7. `TestFileOpQueueBusy` — `ErrFileOpBusy`/`ErrFileOpNotFound` 是独立 sentinel
8. `TestFileOpCancelBeforeRun` — 入队后被取消：所有项补 `skipped`、`results` 覆盖全部 `totalItems`、文件未删除
9. `TestFileOpCancelResultsComplete` — 复制中途取消：`results` 覆盖全部项、半成品被清理

### 回归测试

- `go test ./...` 全量通过，util / local / service 既有测试无回归（`CopyPath`/`MovePath` 签名改为接收 `context` 但旧调用方 `context.Background()` 透明兼容）

### 手动验证

1. 复制多个大文件 → 请求瞬间返回 → 传输面板出现「复制 · 文件名 · 字节进度 · 总进度」
2. 任务运行中切换到其他目录浏览 → 不阻塞
3. 任务完成 → 若停留在目标目录则自动刷新
4. 点击取消 → 任务进入「已取消」，半成品被清理；已完成项保留，详情面板显示「成功 N · 未完成 M」
5. 点击任务行 → 弹出详情 Modal，展示逐项状态（成功 / 失败 / 取消 / 未执行）
6. 关闭浏览器再打开 → 10 分钟内 SSE 重连可恢复最终结果
7. 新开标签页 / 刷新页面 → 进行中的任务通过 localStorage 恢复，传输面板继续展示进度

## 上线与回滚

### 上线步骤

1. 后端：util → storage 接口 → local → FileService 导出 → FileOpService → handler → 路由 → main 装配
2. 前端：types → api → fileOpStore → TransferPanel → fsStore 委托
3. `make build` 重新打包前端进二进制
4. 部署；确认反向代理对 `text/event-stream` 不缓冲（`X-Accel-Buffering: no` 一般够用，必要时 `proxy_buffering off` + 调大 `proxy_read_timeout`）

### 回滚

1. 回滚代码到上一版本
2. 旧同步接口本就保留，前端回滚后继续用旧接口，无数据兼容问题
3. 内存任务随进程退出即消失，无脏数据

## 建议实施顺序

1. 后端底层：`util.CopyPathWithProgress`/`MovePathWithProgress` + `progressWriter`
2. storage：`ProgressCopier` 可选接口 + local 实现
3. FileService：导出 `StatDir`/`TransferTarget`/`CheckSpace`/`TreeSize`
4. FileOpService：任务注册表 + 单 worker + SSE 事件
5. handler + 路由 + main 装配 + 错误码
6. 前端：types → api → fileOpStore → TransferPanel → fsStore 委托
7. 测试 + 完整构建验证

## 风险与待确认问题

### 已确认决策

1. 新增异步接口、保留旧同步接口 — 用户确认
2. SSE 推送进度 — 用户确认
3. 全局串行执行（单 worker）— 用户确认
4. 项内字节级进度 — 用户确认

### 风险

1. **SSE 经反向代理缓冲**：线上 nginx 可能缓冲响应导致进度延迟，已设 `X-Accel-Buffering: no` + 心跳，必要时需运维侧 `proxy_buffering off`
2. **内存任务不跨重启**：服务重启未完成任务无最终结果，用户需重新发起（与上传会话一致，可接受）
3. **`total_bytes` 估算对超大目录可能偏慢**：`TreeSize` 递归 stat 求和，百万级文件目录会拖慢 `Start` 响应；当前未设上限，必要时可加超时降级为 0
4. **`ProgressCopier` 仅 local 实现**：未来 WebDAV / 网盘驱动若不实现该接口，异步路径退化为仅项级进度（功能可用但体验降级）

### 非阻塞待确认

1. 任务保留 TTL 是否需要可配置（当前硬编码 10min）— 后续可加配置项
2. 是否需要任务历史记录持久化 — 当前非目标，后续视需求评估

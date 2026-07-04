// 后端 list/stat 返回的文件类型，仅区分 file/dir。
export type EntryType = 'file' | 'dir';

// 供前端图标与预览决策的大类（按扩展名推导）。
export type FileKind = 'folder' | 'text' | 'image' | 'video' | 'audio' | 'pdf' | 'unknown';

// 单个文件/目录条目，对应后端 model.FileInfo。
export interface FileEntry {
  name: string;
  type: EntryType;
  size: number;
  mode: string;
  modTime: string; // ISO 时间字符串（来自 mod_time）
  isSymlink: boolean;
  symlinkTarget?: string;
  unreachable?: boolean;
}

// 目录列表返回，对应后端 model.ListResult。
export interface ListResult {
  path: string;
  total: number;
  page: number;
  pageSize: number;
  items: FileEntry[];
}

// 预览返回，对应后端 model.PreviewResult。
export interface PreviewResult {
  type: 'text' | 'binary' | 'image' | 'video' | 'audio';
  content: string;
  truncated: boolean;
  size: number;
  previewBytes: number;
}

export interface ListOptions {
  sort?: 'name' | 'size' | 'mtime';
  order?: 'asc' | 'desc';
  showHidden?: boolean;
  page?: number;
  pageSize?: number;
}

// 批量写操作（move / delete）的单条结果，对应后端 model.OpResult。
export interface OpResult {
  src: string;
  ok: boolean;
  error?: string; // 失败时的错误码名
}

// 单条搜索命中，对应后端 model.SearchHit。
export interface SearchHit {
  path: string;
  name: string;
  type: EntryType;
  size: number;
  mode: string;
  modTime: string;
}

// 搜索返回，对应后端 model.SearchResult。
export interface SearchResult {
  query: string;
  base: string;
  items: SearchHit[];
  truncated: boolean;
  timedOut: boolean;
}

export interface SearchOptions {
  recursive?: boolean;
  showHidden?: boolean;
  limit?: number;
}

// 收藏夹条目，对应后端 model.Bookmark。
export interface Bookmark {
  id: number;
  name: string;
  path: string;
  sortOrder: number;
  createdAt: string;
  valid: boolean; // 目标仍存在且为目录
}

export interface RecentAccessItem {
  path: string;
  name: string;
  type: EntryType;
  visitedAt: number;
}

// 文本保存乐观锁的不透明版本 token，对应后端 model.FileRevision。
export interface FileRevision {
  token: string;
  weak: boolean;
}

// 可编辑文本的完整内容，对应后端 model.FileContentResult。
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

// 保存成功返回，对应后端 model.SaveContentResult。
export interface SaveContentResult {
  path: string;
  size: number;
  modTime: string;
  revision: FileRevision;
}

// 保存冲突（409）返回的 data 体，对应后端 model.SaveConflict。
export interface SaveConflict {
  path: string;
  currentModTime: string;
  currentRevision: FileRevision;
}

// 路径级容量，对应后端 model.SpaceResult。
export interface SpaceInfo {
  path: string;
  mount: { name: string; prefix: string };
  space: {
    supported: boolean;
    total?: number;
    used?: number;
    free?: number;
    available?: number;
    usedPercent?: number;
  };
  readonly: boolean;
}

// 剪贴板状态：复制 / 剪切两态，承载待粘贴的路径集合。
export interface Clipboard {
  mode: 'copy' | 'cut';
  paths: string[];
}

// 系统信息，对应后端 model.SystemInfo。
export interface SystemInfo {
  os: string;
  arch: string;
  serverTime: string;
  deviceManagement: boolean; // 设备管理是否可用（Linux + lsblk/udisksctl）
}

// 块设备 / 分区，对应后端 model.Device。
export interface Device {
  device: string; // /dev/sdc1
  id: string; // 挂载点名（/drive/<id>）
  name: string;
  label: string;
  fstype: string;
  size: number;
  mounted: boolean;
  mountpoint: string;
  drivePath: string; // /drive/<id>，前端「进入」用
  removable: boolean; // 是否可移动设备（USB / 热插拔）
  readonly: boolean;
  system: boolean; // 是否为系统关键挂载（根 / 引导分区），前端禁用卸载
}

// 上传初始化返回，对应后端 model.UploadInitResult。
export interface UploadInitResult {
  uploadId: string;
  chunkSize: number;
  totalChunks: number;
  received: number[]; // 已收分片索引（断点续传时非空）
}

// 上传任务状态机：
//   pending    等待开始（已入队）
//   conflict   目标已存在，等待用户选择覆盖/改名/取消
//   uploading  分片上传中
//   done       完成
//   error      失败
//   canceled   用户取消
export type UploadStatus = 'pending' | 'conflict' | 'uploading' | 'done' | 'error' | 'canceled';

// 单个上传任务（前端内存态，不持久化）。
export interface UploadTask {
  id: string; // 前端生成的本地任务 id
  file: File;
  dir: string; // 目标目录 API 路径
  name: string; // 目标文件名（改名后可不同于 file.name）
  status: UploadStatus;
  loaded: number; // 已上传字节（进度展示）
  total: number; // 文件总字节
  speed: number; // 当前瞬时速率（bytes/s），EMA 平滑，仅 uploading 态有效
  lastLoaded: number; // 上次进度快照字节（内部，速率计算用）
  lastTs: number; // 上次进度快照时间戳 ms（内部）
  error?: string; // 失败时的可读信息
}

// 异步文件操作任务状态机：
//   queued   已入队，等待全局串行槽
//   running  执行中
//   done     全部完成（可能有单项失败，看 results）
//   canceled 用户取消
//   error    整体失败
export type FileOpStatus = 'queued' | 'running' | 'done' | 'canceled' | 'error';

// 异步文件操作类型。
export type FileOpKind = 'copy' | 'move' | 'delete';

// 后端 SSE 推送的任务快照（对应 model.FileOpSnapshot）。
export interface FileOpSnapshot {
  op: FileOpKind;
  status: FileOpStatus;
  total_items: number;
  total_bytes: number;
  done_items: number;
  done_bytes: number;
  cur_index: number;
  cur_name: string;
  cur_size: number;
  cur_copied: number;
  speed: number;
  results?: OpResult[];
  error?: string;
  started_at: string;
}

// 后端 SSE 事件（对应 service.FileOpEvent）。
export interface FileOpEvent {
  type: 'snapshot' | 'item_start' | 'item_progress' | 'item_done' | 'finished';
  snapshot: FileOpSnapshot;
  index?: number;
  name?: string;
  size?: number;
  copied?: number;
  ok?: boolean;
  error?: string;
}

// POST /api/fs/op/* 返回的任务句柄。
export interface FileOpStartResult {
  taskId: string;
  op: FileOpKind;
  totalItems: number;
  totalBytes: number;
}

// 单项执行状态（详情面板用）。运行中由 item_start/item_done 事件重建，
// 完成后由 finished 携带的 results[] 覆盖（authoritative）。
export type FileOpItemStatus = 'pending' | 'running' | 'done' | 'failed' | 'canceled' | 'skipped';

export interface FileOpItem {
  index: number;
  name: string; // basename
  size: number;
  status: FileOpItemStatus;
  error?: string; // 失败 / 取消 / 跳过时的错误码名
}

// 前端文件操作任务（内存态，不持久化；task_id 持久化到 localStorage 供跨标签页恢复）。
export interface FileOpTask {
  id: string; // 后端 task_id
  op: FileOpKind;
  dst?: string; // copy/move 的目标目录（用于完成时刷新判断）
  srcs: string[]; // 原始路径（用于完成时刷新判断 + 详情面板 fallback 名称）
  status: FileOpStatus;
  totalItems: number;
  totalBytes: number;
  doneItems: number;
  doneBytes: number;
  curIndex: number;
  curName: string;
  curSize: number;
  curCopied: number;
  speed: number;
  results?: OpResult[];
  items?: FileOpItem[]; // 详情面板用：逐项状态
  error?: string;
  // 内部：SSE 连接句柄，用于关闭
  _es?: EventSource;
}

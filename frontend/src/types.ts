// 后端 list/stat 返回的文件类型，仅区分 file/dir。
export type EntryType = 'file' | 'dir';

// 供前端图标与预览决策的大类（按扩展名推导）。
export type FileKind = 'folder' | 'text' | 'image' | 'video' | 'audio' | 'unknown';

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

// 剪贴板状态：复制 / 剪切两态，承载待粘贴的路径集合。
export interface Clipboard {
  mode: 'copy' | 'cut';
  paths: string[];
}

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

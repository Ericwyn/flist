// 统一响应信封，与后端 handler.Envelope 对齐。
import {
  FileEntry, ListResult, PreviewResult, ListOptions,
  OpResult, SearchResult, SearchHit, SearchOptions, Bookmark,
  UploadInitResult,
} from '../types';
import { parentPath, joinPath } from './path';

export interface Envelope<T = unknown> {
  code: number;
  message: string;
  data: T;
}

export interface LoginData {
  token: string;
  expires_at: number;
  username: string;
}

export interface MeData {
  id: number;
  username: string;
}

const TOKEN_KEY = 'flist_token';

export function getToken(): string | null {
  return localStorage.getItem(TOKEN_KEY);
}

export function setToken(token: string): void {
  localStorage.setItem(TOKEN_KEY, token);
}

export function clearToken(): void {
  localStorage.removeItem(TOKEN_KEY);
}

// ApiError 携带业务错误码，便于上层按 code 区分处理。
export class ApiError extends Error {
  code: number;
  constructor(code: number, message: string) {
    super(message);
    this.code = code;
    this.name = 'ApiError';
  }
}

interface RequestOptions {
  method?: string;
  body?: unknown;
}

async function request<T>(path: string, opts: RequestOptions = {}): Promise<T> {
  const headers: Record<string, string> = {};
  const token = getToken();
  if (token) {
    headers['Authorization'] = `Bearer ${token}`;
  }
  let body: string | undefined;
  if (opts.body !== undefined) {
    headers['Content-Type'] = 'application/json';
    body = JSON.stringify(opts.body);
  }

  const resp = await fetch(path, {
    method: opts.method ?? 'GET',
    headers,
    body,
    credentials: 'same-origin',
  });

  let env: Envelope<T>;
  try {
    env = (await resp.json()) as Envelope<T>;
  } catch {
    throw new ApiError(resp.status, `请求失败 (HTTP ${resp.status})`);
  }

  if (env.code !== 0) {
    throw new ApiError(env.code, env.message || '请求失败');
  }
  return env.data;
}

export const api = {
  login(username: string, password: string): Promise<LoginData> {
    return request<LoginData>('/api/auth/login', {
      method: 'POST',
      body: { username, password },
    });
  },

  logout(): Promise<null> {
    return request<null>('/api/auth/logout', { method: 'POST' });
  },

  me(): Promise<MeData> {
    return request<MeData>('/api/auth/me');
  },

  changePassword(oldPassword: string, newPassword: string): Promise<null> {
    return request<null>('/api/auth/password', {
      method: 'PUT',
      body: { old_password: oldPassword, new_password: newPassword },
    });
  },

  changeUsername(username: string): Promise<MeData> {
    return request<MeData>('/api/auth/username', {
      method: 'PUT',
      body: { username },
    });
  },

  fs: {
    async list(path: string, opts: ListOptions = {}): Promise<ListResult> {
      const params = new URLSearchParams({ path: path || '/' });
      if (opts.sort) params.set('sort', opts.sort);
      if (opts.order) params.set('order', opts.order);
      if (opts.showHidden) params.set('show_hidden', 'true');
      if (opts.page) params.set('page', String(opts.page));
      if (opts.pageSize) params.set('page_size', String(opts.pageSize));
      const raw = await request<RawListResult>(`/api/fs/list?${params.toString()}`);
      return {
        path: raw.path,
        total: raw.total,
        page: raw.page,
        pageSize: raw.page_size,
        items: (raw.items || []).map(mapEntry),
      };
    },

    async stat(path: string): Promise<FileEntry> {
      const raw = await request<RawEntry>(
        `/api/fs/stat?path=${encodeURIComponent(path)}`,
      );
      return mapEntry(raw);
    },

    async preview(path: string): Promise<PreviewResult> {
      const raw = await request<RawPreview>(
        `/api/fs/preview?path=${encodeURIComponent(path)}`,
      );
      return {
        type: raw.type,
        content: raw.content,
        truncated: raw.truncated,
        size: raw.size,
        previewBytes: raw.preview_bytes,
      };
    },

    // downloadUrl 构造下载/内联 URL。媒体内联依赖同源 HttpOnly Cookie 鉴权。
    downloadUrl(path: string, opts: { download?: boolean } = {}): string {
      const params = new URLSearchParams({ path });
      if (opts.download) params.set('download', '1');
      return `/api/fs/download?${params.toString()}`;
    },

    // mkdir 创建单层目录，返回规范化后的路径。
    async mkdir(path: string): Promise<string> {
      const raw = await request<RawPathResult>('/api/fs/mkdir', {
        method: 'POST',
        body: { path },
      });
      return raw.path;
    },

    // touch 创建空文件，返回规范化后的路径。
    async touch(path: string): Promise<string> {
      const raw = await request<RawPathResult>('/api/fs/touch', {
        method: 'POST',
        body: { path },
      });
      return raw.path;
    },

    // move 批量移动 / 重命名，逐项返回结果（尽力而为）。
    // autoRename：仅「移入已存在目录」分支生效，落点同名时后端自动避让。
    async move(src: string[], dst: string, autoRename = false): Promise<OpResult[]> {
      const raw = await request<RawOpResults>('/api/fs/move', {
        method: 'POST',
        body: { src, dst, auto_rename: autoRename },
      });
      return raw.results || [];
    },

    // copy 批量复制，逐项返回结果（尽力而为）。autoRename 同 move。
    async copy(src: string[], dst: string, autoRename = false): Promise<OpResult[]> {
      const raw = await request<RawOpResults>('/api/fs/copy', {
        method: 'POST',
        body: { src, dst, auto_rename: autoRename },
      });
      return raw.results || [];
    },

    // rename 是 move 的便捷封装：把单个条目重命名为同目录下的新名（严格冲突，不避让）。
    async rename(fromPath: string, newName: string): Promise<OpResult> {
      const dst = joinPath(parentPath(fromPath), newName);
      const results = await this.move([fromPath], dst);
      return results[0];
    },

    // remove 批量递归删除，逐项返回结果（尽力而为）。
    async remove(paths: string[]): Promise<OpResult[]> {
      const raw = await request<RawOpResults>('/api/fs/delete', {
        method: 'DELETE',
        body: { paths },
      });
      return raw.results || [];
    },

    // search 按文件名匹配搜索。
    async search(base: string, q: string, opts: SearchOptions = {}): Promise<SearchResult> {
      const params = new URLSearchParams({ path: base || '/', q });
      if (opts.recursive === false) params.set('recursive', 'false');
      if (opts.showHidden) params.set('show_hidden', 'true');
      if (opts.limit) params.set('limit', String(opts.limit));
      const raw = await request<RawSearchResult>(`/api/fs/search?${params.toString()}`);
      return {
        query: raw.query,
        base: raw.base,
        items: (raw.items || []).map(mapHit),
        truncated: raw.truncated,
        timedOut: raw.timed_out,
      };
    },

    // uploadInit 初始化（或按指纹复用）分片上传会话。
    async uploadInit(
      dir: string,
      name: string,
      totalSize: number,
      chunkSize: number,
      fingerprint: string,
    ): Promise<UploadInitResult> {
      const raw = await request<RawUploadInit>('/api/fs/upload/init', {
        method: 'POST',
        body: {
          dir,
          name,
          total_size: totalSize,
          chunk_size: chunkSize,
          fingerprint,
        },
      });
      return {
        uploadId: raw.upload_id,
        chunkSize: raw.chunk_size,
        totalChunks: raw.total_chunks,
        received: raw.received || [],
      };
    },

    // uploadChunk 上传单个分片（二进制 body，不走 JSON 信封）。
    // 用裸 fetch + Authorization 头，body 为分片 Blob。
    async uploadChunk(uploadId: string, index: number, blob: Blob): Promise<void> {
      const headers: Record<string, string> = {
        'Content-Type': 'application/octet-stream',
      };
      const token = getToken();
      if (token) headers['Authorization'] = `Bearer ${token}`;

      const params = new URLSearchParams({ upload_id: uploadId, index: String(index) });
      const resp = await fetch(`/api/fs/upload/chunk?${params.toString()}`, {
        method: 'POST',
        headers,
        body: blob,
        credentials: 'same-origin',
      });
      let env: Envelope<unknown>;
      try {
        env = (await resp.json()) as Envelope<unknown>;
      } catch {
        throw new ApiError(resp.status, `分片上传失败 (HTTP ${resp.status})`);
      }
      if (env.code !== 0) {
        throw new ApiError(env.code, env.message || '分片上传失败');
      }
    },

    // uploadComplete 校验分片齐全后合并落盘。返回落盘路径。
    async uploadComplete(uploadId: string, overwrite: boolean): Promise<string> {
      const raw = await request<RawPathResult>('/api/fs/upload/complete', {
        method: 'POST',
        body: { upload_id: uploadId, overwrite },
      });
      return raw.path;
    },
  },

  bookmarks: {
    // list 获取当前用户的收藏列表（含 valid 失效标记）。
    async list(): Promise<Bookmark[]> {
      const raw = await request<{ bookmarks: RawBookmark[] }>('/api/bookmarks');
      return (raw.bookmarks || []).map(mapBookmark);
    },

    // create 收藏一个目录。name 可省略（后端回落为 basename）。
    async create(path: string, name?: string): Promise<Bookmark> {
      const raw = await request<RawBookmark>('/api/bookmarks', {
        method: 'POST',
        body: { path, name: name ?? '' },
      });
      return mapBookmark(raw);
    },

    // update 重命名收藏。
    async update(id: number, name: string): Promise<void> {
      await request<null>(`/api/bookmarks/${id}`, {
        method: 'PUT',
        body: { name },
      });
    },

    // remove 删除收藏。
    async remove(id: number): Promise<void> {
      await request<null>(`/api/bookmarks/${id}`, { method: 'DELETE' });
    },

    // reorder 批量调整排序。
    async reorder(orders: { id: number; sortOrder: number }[]): Promise<void> {
      await request<null>('/api/bookmarks/reorder', {
        method: 'PUT',
        body: { orders: orders.map((o) => ({ id: o.id, sort_order: o.sortOrder })) },
      });
    },
  },
};

// 后端原始字段（snake_case）。
interface RawEntry {
  name: string;
  type: 'file' | 'dir';
  size: number;
  mode: string;
  mod_time: string;
  is_symlink: boolean;
  symlink_target?: string;
  unreachable?: boolean;
}

interface RawListResult {
  path: string;
  total: number;
  page: number;
  page_size: number;
  items: RawEntry[];
}

interface RawPreview {
  type: 'text' | 'binary' | 'image' | 'video' | 'audio';
  content: string;
  truncated: boolean;
  size: number;
  preview_bytes: number;
}

interface RawPathResult {
  path: string;
}

interface RawUploadInit {
  upload_id: string;
  chunk_size: number;
  total_chunks: number;
  received: number[];
}

interface RawOpResults {
  results: OpResult[];
}

interface RawSearchHit {
  path: string;
  name: string;
  type: 'file' | 'dir';
  size: number;
  mode: string;
  mod_time: string;
}

interface RawSearchResult {
  query: string;
  base: string;
  items: RawSearchHit[];
  truncated: boolean;
  timed_out: boolean;
}

function mapEntry(r: RawEntry): FileEntry {
  return {
    name: r.name,
    type: r.type,
    size: r.size,
    mode: r.mode,
    modTime: r.mod_time,
    isSymlink: r.is_symlink,
    symlinkTarget: r.symlink_target,
    unreachable: r.unreachable,
  };
}

function mapHit(r: RawSearchHit): SearchHit {
  return {
    path: r.path,
    name: r.name,
    type: r.type,
    size: r.size,
    mode: r.mode,
    modTime: r.mod_time,
  };
}

interface RawBookmark {
  id: number;
  name: string;
  path: string;
  sort_order: number;
  created_at: string;
  valid: boolean;
}

function mapBookmark(r: RawBookmark): Bookmark {
  return {
    id: r.id,
    name: r.name,
    path: r.path,
    sortOrder: r.sort_order,
    createdAt: r.created_at,
    valid: r.valid,
  };
}

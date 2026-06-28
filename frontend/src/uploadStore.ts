import { create } from 'zustand';
import { api, ApiError } from './lib/api';
import { UploadTask } from './types';
import { joinPath } from './lib/path';
import { useAuthStore } from './authStore';
import { useFsStore } from './fsStore';

// CHUNK_SIZE 为前端切片大小（与后端归一区间一致，8MiB 是默认值）。
const CHUNK_SIZE = 8 << 20;
// CHUNK_CONCURRENCY 为单个文件内并发上传的分片数（NAS 顺序写为主，3 个够用且不过载）。
const CHUNK_CONCURRENCY = 3;

interface UploadState {
  tasks: UploadTask[];
  panelOpen: boolean;

  // enqueue 把一批文件加入上传队列，目标为 dir；自动开始（冲突项转 conflict 等待用户）。
  enqueue: (files: File[], dir: string) => Promise<void>;
  // resolveConflict 处理冲突任务：overwrite 覆盖、rename 改名重传、cancel 取消。
  resolveOverwrite: (id: string) => void;
  resolveRename: (id: string, newName: string) => void;
  cancelTask: (id: string) => void;
  // removeTask 从列表移除一条（完成/失败后清理 UI）。
  removeTask: (id: string) => void;
  clearFinished: () => void;
  closePanel: () => void;
}

// fingerprint 由文件名 + 大小 + 最后修改时间成，用于断点续传识别「同一文件」。
function fingerprint(file: File): string {
  return `${file.name}:${file.size}:${file.lastModified}`;
}

function genId(): string {
  return `${Date.now()}-${Math.random().toString(36).slice(2, 9)}`;
}

export const useUploadStore = create<UploadState>((set, get) => ({
  tasks: [],
  panelOpen: false,

  enqueue: async (files, dir) => {
    if (files.length === 0) return;
    const newTasks: UploadTask[] = files.map((file) => ({
      id: genId(),
      file,
      dir,
      name: file.name,
      status: 'pending',
      loaded: 0,
      total: file.size,
    }));
    set((s) => ({ tasks: [...s.tasks, ...newTasks], panelOpen: true }));

    // 逐个文件：先预检冲突，无冲突则开始上传。
    for (const task of newTasks) {
      const exists = await targetExists(task.dir, task.name);
      if (exists) {
        patch(set, task.id, { status: 'conflict' });
      } else {
        void runUpload(set, get, task.id, false);
      }
    }
  },

  resolveOverwrite: (id) => {
    const task = get().tasks.find((t) => t.id === id);
    if (!task) return;
    void runUpload(set, get, id, true);
  },

  resolveRename: (id, newName) => {
    const name = newName.trim();
    if (!name) return;
    patch(set, id, { name, status: 'pending' });
    // 改名后重新预检：新名可能仍冲突。
    void (async () => {
      const task = get().tasks.find((t) => t.id === id);
      if (!task) return;
      const exists = await targetExists(task.dir, name);
      if (exists) {
        patch(set, id, { status: 'conflict' });
      } else {
        void runUpload(set, get, id, false);
      }
    })();
  },

  cancelTask: (id) => {
    patch(set, id, { status: 'canceled' });
  },

  removeTask: (id) => {
    set((s) => ({ tasks: s.tasks.filter((t) => t.id !== id) }));
  },

  clearFinished: () => {
    set((s) => ({
      tasks: s.tasks.filter(
        (t) => t.status !== 'done' && t.status !== 'canceled' && t.status !== 'error',
      ),
    }));
  },

  closePanel: () => set({ panelOpen: false }),
}));

// targetExists 预检目标文件是否已存在（用于冲突判定）。出错（如不存在）视为不存在。
async function targetExists(dir: string, name: string): Promise<boolean> {
  try {
    await api.fs.stat(joinPath(dir, name));
    return true;
  } catch {
    return false;
  }
}

// runUpload 执行单个任务的完整上传：init → 并发分片 → complete。
// overwrite 透传给 complete；冲突预检由调用方在此之前完成。
async function runUpload(
  set: (fn: (s: UploadState) => Partial<UploadState>) => void,
  get: () => UploadState,
  id: string,
  overwrite: boolean,
) {
  const task = get().tasks.find((t) => t.id === id);
  if (!task) return;
  patch(set, id, { status: 'uploading', loaded: 0, error: undefined });

  try {
    const init = await api.fs.uploadInit(
      task.dir,
      task.name,
      task.file.size,
      CHUNK_SIZE,
      fingerprint(task.file),
    );
    const cs = init.chunkSize;
    const received = new Set(init.received);

    // 已收分片直接计入进度（断点续传）。
    let uploaded = received.size;
    const totalChunks = init.totalChunks;
    syncProgress(set, id, uploaded, totalChunks, task.file.size);

    // 待传分片索引队列。
    const pending: number[] = [];
    for (let i = 0; i < totalChunks; i++) {
      if (!received.has(i)) pending.push(i);
    }

    // 以固定并发消费 pending 队列。
    let cursor = 0;
    let failed: unknown = null;
    const worker = async () => {
      while (cursor < pending.length && !failed) {
        // 任务被取消则停止。
        if (get().tasks.find((t) => t.id === id)?.status === 'canceled') return;
        const idx = pending[cursor++];
        const start = idx * cs;
        const end = Math.min(start + cs, task.file.size);
        const blob = task.file.slice(start, end);
        try {
          await api.fs.uploadChunk(init.uploadId, idx, blob);
          uploaded++;
          syncProgress(set, id, uploaded, totalChunks, task.file.size);
        } catch (e) {
          failed = e;
          return;
        }
      }
    };
    await Promise.all(
      Array.from({ length: Math.min(CHUNK_CONCURRENCY, pending.length || 1) }, worker),
    );

    if (get().tasks.find((t) => t.id === id)?.status === 'canceled') {
      return; // 取消则不 complete。
    }
    if (failed) throw failed;

    await api.fs.uploadComplete(init.uploadId, overwrite);
    patch(set, id, { status: 'done', loaded: task.file.size });
    maybeRefresh(task.dir);
  } catch (e) {
    handleAuth(e);
    patch(set, id, { status: 'error', error: uploadErrMessage(e) });
  }
}

// syncProgress 按已传分片数估算字节进度。
function syncProgress(
  set: (fn: (s: UploadState) => Partial<UploadState>) => void,
  id: string,
  uploadedChunks: number,
  totalChunks: number,
  totalBytes: number,
) {
  const ratio = totalChunks > 0 ? uploadedChunks / totalChunks : 0;
  patch(set, id, { loaded: Math.round(ratio * totalBytes) });
}

// patch 更新指定任务的部分字段。
function patch(
  set: (fn: (s: UploadState) => Partial<UploadState>) => void,
  id: string,
  fields: Partial<UploadTask>,
) {
  set((s) => ({
    tasks: s.tasks.map((t) => (t.id === id ? { ...t, ...fields } : t)),
  }));
}

// maybeRefresh 当上传目标为当前浏览目录时刷新列表；搜索态下重跑当前搜索以纳入新文件。
function maybeRefresh(dir: string) {
  const fs = useFsStore.getState();
  if (fs.currentPath !== dir) return;
  if (fs.searchOpen && fs.searchQuery) {
    void fs.runSearch(fs.searchQuery);
    return;
  }
  void fs.refresh();
}

// uploadErrMessage 将上传错误码翻译为中文。
function uploadErrMessage(e: unknown): string {
  if (e instanceof ApiError) {
    switch (e.code) {
      case 2004:
        return '目标已存在';
      case 2005:
        return '磁盘空间不足';
      case 2006:
        return '文件名非法';
      case 2009:
        return '文件超过上传大小上限';
      case 2010:
        return '上传会话已失效，请重试';
      case 2011:
        return '分片不完整';
      default:
        return e.message;
    }
  }
  return e instanceof Error ? e.message : '上传失败';
}

function handleAuth(e: unknown) {
  if (e instanceof ApiError && e.code === 1001) {
    useAuthStore.getState().logout();
  }
}

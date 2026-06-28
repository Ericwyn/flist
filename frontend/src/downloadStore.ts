import { create } from 'zustand';
import { api, ApiError } from './lib/api';
import { useAuthStore } from './authStore';

// 打包下载任务状态：
//   downloading 正在流式接收 zip
//   done        接收完成（浏览器已触发保存）
//   error       失败
//   canceled    用户取消
export type DownloadStatus = 'downloading' | 'done' | 'error' | 'canceled';

// 单个打包下载任务（前端内存态，不持久化）。流式 zip 无 Content-Length，
// 故只有已接收字节 loaded、没有总大小，进度条为不确定态。
export interface DownloadTask {
  id: string;
  name: string; // 下载文件名（含 .zip），仅用于 UI 展示
  status: DownloadStatus;
  loaded: number; // 已接收字节
  error?: string;
}

interface DownloadState {
  tasks: DownloadTask[];
  panelOpen: boolean;

  // start 发起一个打包下载任务（多文件 / 目录），自动加入面板并开始；支持同时多个。
  start: (paths: string[], name?: string) => void;
  cancelTask: (id: string) => void;
  removeTask: (id: string) => void;
  clearFinished: () => void;
  closePanel: () => void;
}

// 每个任务对应的 AbortController，置于 store 外的模块级 map（不进 React 状态）。
const controllers = new Map<string, AbortController>();

function genId(): string {
  return `dl-${Date.now()}-${Math.random().toString(36).slice(2, 9)}`;
}

export const useDownloadStore = create<DownloadState>((set) => ({
  tasks: [],
  panelOpen: false,

  start: (paths, name) => {
    if (paths.length === 0) return;
    const id = genId();
    const displayName = `${name && name.trim() ? name.trim() : 'flist-download'}.zip`;
    const task: DownloadTask = { id, name: displayName, status: 'downloading', loaded: 0 };
    set((s) => ({ tasks: [...s.tasks, task], panelOpen: true }));

    const controller = new AbortController();
    controllers.set(id, controller);

    void (async () => {
      try {
        await api.fs.archive(
          paths,
          name,
          (received) => patch(set, id, { loaded: received }),
          controller.signal,
        );
        patch(set, id, { status: 'done' });
      } catch (e) {
        if (controller.signal.aborted || (e instanceof DOMException && e.name === 'AbortError')) {
          patch(set, id, { status: 'canceled' });
        } else {
          handleAuth(e);
          patch(set, id, { status: 'error', error: downloadErrMessage(e) });
        }
      } finally {
        controllers.delete(id);
      }
    })();
  },

  cancelTask: (id) => {
    controllers.get(id)?.abort();
    patch(set, id, { status: 'canceled' });
  },

  removeTask: (id) => {
    controllers.get(id)?.abort(); // 移除未完成任务时一并中断
    set((s) => ({ tasks: s.tasks.filter((t) => t.id !== id) }));
  },

  clearFinished: () => {
    set((s) => ({ tasks: s.tasks.filter((t) => t.status === 'downloading') }));
  },

  closePanel: () => set({ panelOpen: false }),
}));

// patch 更新指定任务的部分字段。
function patch(
  set: (fn: (s: DownloadState) => Partial<DownloadState>) => void,
  id: string,
  fields: Partial<DownloadTask>,
) {
  set((s) => ({
    tasks: s.tasks.map((t) => (t.id === id ? { ...t, ...fields } : t)),
  }));
}

// downloadErrMessage 将打包下载错误码翻译为中文（复用 archive 的错误词表）。
function downloadErrMessage(e: unknown): string {
  if (e instanceof ApiError) {
    switch (e.code) {
      case 2001:
        return '文件或目录不存在';
      case 2002:
        return '路径越界';
      case 4000:
        return '请求无效';
      default:
        return e.message;
    }
  }
  return e instanceof Error ? e.message : '打包下载失败';
}

function handleAuth(e: unknown) {
  if (e instanceof ApiError && e.code === 1001) {
    useAuthStore.getState().logout();
  }
}

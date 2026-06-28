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
// speed 为前端按字节增量与时间差计算的瞬时速率（EMA 平滑，bytes/s），
// lastLoaded / lastTs 为内部快照，仅供速率计算，不用于 UI。
export interface DownloadTask {
  id: string;
  name: string; // 下载文件名（含 .zip），仅用于 UI 展示
  status: DownloadStatus;
  loaded: number; // 已接收字节
  speed: number; // 当前瞬时速率（bytes/s），进行中实时更新
  lastLoaded: number; // 上次进度快照字节（内部）
  lastTs: number; // 上次进度快照时间戳 ms（内部）
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

export const useDownloadStore = create<DownloadState>((set, get) => ({
  tasks: [],
  panelOpen: false,

  start: (paths, name) => {
    if (paths.length === 0) return;
    const id = genId();
    const displayName = `${name && name.trim() ? name.trim() : 'flist-download'}.zip`;
    const now = Date.now();
    const task: DownloadTask = {
      id,
      name: displayName,
      status: 'downloading',
      loaded: 0,
      speed: 0,
      lastLoaded: 0,
      lastTs: now,
    };
    set((s) => ({ tasks: [...s.tasks, task], panelOpen: true }));

    const controller = new AbortController();
    controllers.set(id, controller);

    void (async () => {
      try {
        await api.fs.archive(
          paths,
          name,
          (received) => patch(set, id, computeSpeed(id, received, get)),
          controller.signal,
        );
        patch(set, id, { status: 'done', speed: 0, lastLoaded: 0, lastTs: 0 });
      } catch (e) {
        if (controller.signal.aborted || (e instanceof DOMException && e.name === 'AbortError')) {
          patch(set, id, { status: 'canceled', speed: 0 });
        } else {
          handleAuth(e);
          patch(set, id, { status: 'error', error: downloadErrMessage(e), speed: 0 });
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

// computeSpeed 基于新接收字节 received 与上次快照计算 EMA 平滑速率，返回需 patch 的字段。
// 间隔过短（<50ms）时仅更新 loaded、保留旧基线让 dt 累积，避免高频小包永远算不到速率；
// 无增量时同样仅更新 loaded。首次（lastTs=0）仅记录基线。
const SPEED_EMA_ALPHA = 0.5;
const SPEED_MIN_DT = 50;

function computeSpeed(
  id: string,
  received: number,
  get: () => DownloadState,
): Partial<DownloadTask> {
  const task = get().tasks.find((t) => t.id === id);
  if (!task) return { loaded: received };
  const now = Date.now();
  // 首次：仅记录基线，不计算速率。
  if (task.lastTs === 0) {
    return { loaded: received, lastLoaded: received, lastTs: now };
  }
  const dt = now - task.lastTs;
  // 间隔过短：仅更新 loaded，保留旧 lastTs/lastLoaded 让 dt 累积到阈值再算，避免抖动且保证速率可算出。
  if (dt < SPEED_MIN_DT) {
    return { loaded: received };
  }
  const delta = received - task.lastLoaded;
  if (delta <= 0) {
    return { loaded: received, lastTs: now };
  }
  const inst = (delta * 1000) / dt; // bytes/s
  const speed = task.speed === 0 ? inst : task.speed * SPEED_EMA_ALPHA + inst * (1 - SPEED_EMA_ALPHA);
  return { loaded: received, speed, lastLoaded: received, lastTs: now };
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

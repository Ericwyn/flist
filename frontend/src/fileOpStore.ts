import { create } from 'zustand';
import { api, ApiError } from './lib/api';
import {
  FileOpTask, FileOpStatus, FileOpKind, FileOpEvent, FileOpItem, FileOpItemStatus,
} from './types';
import { parentPath, baseName } from './lib/path';
import { useAuthStore } from './authStore';
import { useFsStore } from './fsStore';

// 页面卸载标记：刷新 / 关闭时浏览器会关闭 EventSource 并可能触发 onerror，
// 此时不应清理 localStorage 或标记任务失败，否则新页面无法恢复进行中的任务。
let isUnloading = false;
if (typeof window !== 'undefined') {
  window.addEventListener('beforeunload', () => { isUnloading = true; });
  window.addEventListener('pagehide', () => { isUnloading = true; });
}

interface FileOpState {
  tasks: FileOpTask[];
  panelOpen: boolean;

  // startCopy/Move/Delete 发起异步任务，立即返回（不阻塞）；进度通过 SSE 推送。
  startCopy: (srcs: string[], dst: string, autoRename?: boolean) => Promise<void>;
  startMove: (srcs: string[], dst: string, autoRename?: boolean) => Promise<void>;
  startDelete: (paths: string[]) => Promise<void>;
  cancelTask: (id: string) => void;
  removeTask: (id: string) => void;
  clearFinished: () => void;
  closePanel: () => void;
}

export const useFileOpStore = create<FileOpState>((set, get) => ({
  tasks: [],
  panelOpen: false,

  startCopy: (srcs, dst, autoRename = false) =>
    startOp(set, get, 'copy', srcs, dst, autoRename),
  startMove: (srcs, dst, autoRename = false) =>
    startOp(set, get, 'move', srcs, dst, autoRename),
  startDelete: (paths) => startOp(set, get, 'delete', paths, '', false),

  cancelTask: (id) => {
    void api.fs.op.cancel(id).catch(() => {});
    // 乐观标记为取消（终态由 SSE 确认；若 SSE 已断则保持 canceled）。
    patch(set, id, { status: 'canceled' });
  },

  removeTask: (id) => {
    closeStream(get().tasks.find((t) => t.id === id));
    unpersistTask(id);
    set((s) => ({ tasks: s.tasks.filter((t) => t.id !== id) }));
  },

  clearFinished: () => {
    const remaining = get().tasks.filter(
      (t) => t.status === 'queued' || t.status === 'running',
    );
    // 清理被移除任务的 localStorage 条目。
    for (const t of get().tasks) {
      if (t.status !== 'queued' && t.status !== 'running') {
        unpersistTask(t.id);
      }
    }
    set({ tasks: remaining });
  },

  closePanel: () => set({ panelOpen: false }),
}));

// startOp 发起任务并订阅 SSE。任何发起失败（如队列满）直接置 error 任务后不订阅。
async function startOp(
  set: (fn: (s: FileOpState) => Partial<FileOpState>) => void,
  get: () => FileOpState,
  op: FileOpKind,
  srcs: string[],
  dst: string,
  autoRename: boolean,
): Promise<void> {
  if (srcs.length === 0) return;
  let res;
  try {
    if (op === 'copy') res = await api.fs.op.copy(srcs, dst, autoRename);
    else if (op === 'move') res = await api.fs.op.move(srcs, dst, autoRename);
    else res = await api.fs.op.delete(srcs);
  } catch (e) {
    handleAuth(e);
    // 发起失败：仍入面板展示一条 error 任务，便于用户看到原因。
    const failTask: FileOpTask = {
      id: `fail-${Date.now()}`,
      op,
      dst: dst || undefined,
      srcs,
      status: 'error',
      totalItems: srcs.length,
      totalBytes: 0,
      doneItems: 0,
      doneBytes: 0,
      curIndex: -1,
      curName: '',
      curSize: 0,
      curCopied: 0,
      speed: 0,
      error: opErrMessage(e),
    };
    set((s) => ({ tasks: [...s.tasks, failTask], panelOpen: true }));
    return;
  }

  const task: FileOpTask = {
    id: res.taskId,
    op,
    dst: dst || undefined,
    srcs,
    status: 'queued',
    totalItems: res.totalItems,
    totalBytes: res.totalBytes,
    doneItems: 0,
    doneBytes: 0,
    curIndex: -1,
    curName: '',
    curSize: 0,
    curCopied: 0,
    speed: 0,
    // 初始化完整 pending 列表（避免稀疏数组空洞导致 filter/map 访问 undefined）。
    items: srcs.map((s, i) => ({ index: i, name: baseName(s), size: 0, status: 'pending' as const })),
  };
  set((s) => ({ tasks: [...s.tasks, task], panelOpen: true }));
  persistTask(task);
  subscribe(set, get, task.id);
}

// subscribe 打开 SSE 并按事件更新任务状态；finished 后刷新相关目录。
function subscribe(
  set: (fn: (s: FileOpState) => Partial<FileOpState>) => void,
  get: () => FileOpState,
  taskId: string,
) {
  const es = api.fs.op.progress(taskId);
  // 标记连接句柄，便于取消/移除时关闭。
  patch(set, taskId, { _es: es });

  es.onmessage = (ev) => {
    let evt: FileOpEvent;
    try {
      evt = JSON.parse(ev.data) as FileOpEvent;
    } catch {
      return;
    }
    applyEvent(set, get, taskId, evt);
  };

  es.onerror = () => {
    // 页面卸载期间（刷新 / 关闭）浏览器关闭 EventSource，会触发 onerror，
    // 但此时不应清理 localStorage 或标记失败——否则新页面无法恢复任务。
    if (isUnloading) return;
    const t = get().tasks.find((x) => x.id === taskId);
    if (!t) {
      es.close();
      return;
    }
    // 任务已结束：关闭避免无限重连。
    if (t.status === 'done' || t.status === 'error' || t.status === 'canceled') {
      es.close();
      return;
    }
    // readyState===CLOSED 表示服务端返回 404（任务过期 / 不存在 / 非属主），
    // EventSource 不会自动重连 → 标记失败。localStorage 不在此清理：
    // 任务仍留在 localStorage，下次刷新 restorePersistedTasks 会重试订阅，
    // 若仍 404 则再次标记失败，用户可手动移除（removeTask 会清 localStorage）。
    if (es.readyState === EventSource.CLOSED) {
      es.close();
      patch(set, taskId, { status: 'error', error: '任务已过期或不存在' });
    }
    // 否则（CONNECTING）允许浏览器自动重连恢复进度。
  };
}

// applyEvent 将一条 SSE 事件应用到任务状态，并维护逐项 items[] 供详情面板展示。
function applyEvent(
  set: (fn: (s: FileOpState) => Partial<FileOpState>) => void,
  get: () => FileOpState,
  taskId: string,
  evt: FileOpEvent,
) {
  const snap = evt.snapshot;
  if (!snap) return;

  const base: Partial<FileOpTask> = {
    status: snap.status as FileOpStatus,
    totalItems: snap.total_items,
    totalBytes: snap.total_bytes,
    doneItems: snap.done_items,
    doneBytes: snap.done_bytes,
    curIndex: snap.cur_index,
    curName: snap.cur_name,
    curSize: snap.cur_size,
    curCopied: snap.cur_copied,
    speed: snap.speed,
    results: snap.results,
    error: snap.error,
  };

  // 逐项 items[] 维护：运行中由 item_start/item_done 事件更新，
  // 完成后由 finished 携带的 results[] 覆盖（authoritative）。
  // items 在任务创建时已初始化为完整 pending 列表，此处拷贝后更新对应槽位。
  const t = get().tasks.find((x) => x.id === taskId);
  let items: FileOpItem[] | undefined = t?.items ? t.items.map((i) => ({ ...i })) : undefined;

  if (evt.type === 'item_start' && typeof evt.index === 'number') {
    if (!items) items = [];
    items[evt.index] = {
      index: evt.index,
      name: evt.name || (t ? baseName(t.srcs[evt.index] || '') : ''),
      size: evt.size || 0,
      status: 'running',
    };
  } else if (evt.type === 'item_done' && typeof evt.index === 'number') {
    if (!items) items = [];
    const prev = items[evt.index];
    items[evt.index] = {
      index: evt.index,
      name: prev?.name || (t ? baseName(t.srcs[evt.index] || '') : ''),
      size: prev?.size || 0,
      status: itemStatus(evt.ok, evt.error),
      error: evt.ok ? undefined : evt.error,
    };
  } else if (evt.type === 'finished' && snap.results) {
    // results[] 是权威项列表（含 skipped / canceled 项），覆盖运行时重建的 items。
    items = snap.results.map((r, i) => ({
      index: i,
      name: baseName(r.src),
      size: 0,
      status: itemStatus(r.ok, r.error),
      error: r.ok ? undefined : r.error,
    }));
  }
  if (items) base.items = items;

  patch(set, taskId, base);

  if (evt.type === 'finished') {
    const cur = get().tasks.find((x) => x.id === taskId);
    closeStream(cur);
    unpersistTask(taskId);
    if (cur) maybeRefresh(cur);
  }
}

// itemStatus 根据事件 ok/error 推导单项状态。
function itemStatus(ok?: boolean, error?: string): FileOpItemStatus {
  if (ok) return 'done';
  if (error === 'skipped') return 'skipped';
  if (error === 'canceled') return 'canceled';
  return 'failed';
}

// maybeRefresh 任务完成后按相关性刷新当前目录：
//   copy   → 当前目录为目标 dst 时刷新
//   move   → 当前目录为 dst 或任一 src 的父目录、或当前目录本身是某 src 时刷新
//   delete → 当前目录为任一 src 的父目录、或当前目录本身是某 src 时刷新
function maybeRefresh(t: FileOpTask) {
  const fs = useFsStore.getState();
  const cur = fs.currentPath;
  const rel =
    t.op === 'delete'
      ? t.srcs.some((p) => p === cur || parentPath(p) === cur)
      : t.op === 'move'
        ? cur === t.dst || t.srcs.some((p) => p === cur || parentPath(p) === cur)
        : cur === t.dst; // copy
  if (!rel) return;
  if (fs.searchOpen && fs.searchQuery) {
    void fs.runSearch(fs.searchQuery);
    return;
  }
  void fs.refresh();
}

function closeStream(t?: FileOpTask) {
  if (!t?._es) return;
  try {
    t._es.close();
  } catch {
    // ignore
  }
  t._es = undefined;
}

// patch 更新指定任务的部分字段。
function patch(
  set: (fn: (s: FileOpState) => Partial<FileOpState>) => void,
  id: string,
  fields: Partial<FileOpTask>,
) {
  set((s) => ({
    tasks: s.tasks.map((t) => (t.id === id ? { ...t, ...fields } : t)),
  }));
}

// ===== localStorage 持久化（仅 task_id + 元数据，供跨标签页 / 刷新恢复 SSE 订阅）=====

const STORAGE_KEY = 'flist:fileop:tasks';

interface PersistedTask {
  id: string;
  op: FileOpKind;
  srcs: string[];
  dst: string;
}

function persistTask(t: FileOpTask) {
  const all = loadPersistedTasks();
  all.push({ id: t.id, op: t.op, srcs: t.srcs, dst: t.dst || '' });
  savePersistedTasks(all);
}

function unpersistTask(id: string) {
  const all = loadPersistedTasks().filter((t) => t.id !== id);
  savePersistedTasks(all);
}

function loadPersistedTasks(): PersistedTask[] {
  try {
    const raw = localStorage.getItem(STORAGE_KEY);
    if (!raw) return [];
    const arr = JSON.parse(raw);
    return Array.isArray(arr) ? arr : [];
  } catch {
    return [];
  }
}

function savePersistedTasks(tasks: PersistedTask[]) {
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(tasks));
  } catch {
    // localStorage 不可用（隐私模式 / 满）→ 静默降级，不影响功能。
  }
}

// 模块加载时恢复：为每个持久化的 task_id 创建占位任务并订阅 SSE，
// 服务端 snapshot 会立即填充真实状态；任务已过期则 onerror 清理。
function restorePersistedTasks() {
  const persisted = loadPersistedTasks();
  if (persisted.length === 0) return;
  const state = useFileOpStore.getState();
  const existing = new Set(state.tasks.map((t) => t.id));
  for (const p of persisted) {
    if (existing.has(p.id)) continue; // 避免重复
    const placeholder: FileOpTask = {
      id: p.id,
      op: p.op,
      dst: p.dst || undefined,
      srcs: p.srcs,
      status: 'queued',
      totalItems: p.srcs.length,
      totalBytes: 0,
      doneItems: 0,
      doneBytes: 0,
      curIndex: -1,
      curName: '',
      curSize: 0,
      curCopied: 0,
      speed: 0,
      items: p.srcs.map((s, i) => ({ index: i, name: baseName(s), size: 0, status: 'pending' as const })),
    };
    useFileOpStore.setState((s) => ({
      tasks: [...s.tasks, placeholder],
      panelOpen: true,
    }));
    subscribe(useFileOpStore.setState, useFileOpStore.getState, p.id);
  }
}

// 在模块加载后恢复持久化任务（仅浏览器环境）。
if (typeof window !== 'undefined' && typeof localStorage !== 'undefined') {
  restorePersistedTasks();
}

// opErrMessage 将发起失败错误翻译为中文。
function opErrMessage(e: unknown): string {
  if (e instanceof ApiError) {
    switch (e.code) {
      case 2018:
        return '操作队列已满，请稍后再试';
      case 2002:
        return '路径越界';
      case 2001:
        return '路径不存在';
      default:
        return e.message;
    }
  }
  return e instanceof Error ? e.message : '操作失败';
}

function handleAuth(e: unknown) {
  if (e instanceof ApiError && e.code === 1001) {
    useAuthStore.getState().logout();
  }
}

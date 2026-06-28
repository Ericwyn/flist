import React, { useState } from 'react';
import { useUploadStore } from '../uploadStore';
import { useDownloadStore, DownloadTask, DownloadStatus } from '../downloadStore';
import { useFileOpStore } from '../fileOpStore';
import { UploadTask, FileOpTask, FileOpItem } from '../types';
import { Modal } from './Modal';
import {
  X, ChevronDown, ChevronUp, CheckCircle2, AlertCircle, Loader2,
  ArrowLeftRight, Ban, Pencil, Download, Copy, FolderInput, Trash2,
  MinusCircle, Clock,
} from 'lucide-react';

const formatBytes = (bytes: number, decimals = 1) => {
  if (!+bytes) return '0 B';
  const k = 1024;
  const sizes = ['B', 'KB', 'MB', 'GB', 'TB'];
  const i = Math.floor(Math.log(bytes) / Math.log(k));
  return `${parseFloat((bytes / Math.pow(k, i)).toFixed(decimals))} ${sizes[i]}`;
};

// TransferPanel 固定在右下角，统一展示上传、下载与文件操作任务的进度、状态与操作入口。
export function TransferPanel() {
  const upload = useUploadStore();
  const download = useDownloadStore();
  const fileOp = useFileOpStore();
  const [collapsed, setCollapsed] = useState(false);
  const [detailTaskId, setDetailTaskId] = useState<string | null>(null);

  const open =
    (upload.panelOpen && upload.tasks.length > 0) ||
    (download.panelOpen && download.tasks.length > 0) ||
    (fileOp.panelOpen && fileOp.tasks.length > 0);
  if (!open) return null;

  // 进行中 / 总数统计（上传 + 下载 + 文件操作合并）。
  const uploadActive = upload.tasks.filter((t) => t.status === 'uploading' || t.status === 'pending').length;
  const downloadActive = download.tasks.filter((t) => t.status === 'downloading').length;
  const fileOpActive = fileOp.tasks.filter((t) => t.status === 'queued' || t.status === 'running').length;
  const active = uploadActive + downloadActive + fileOpActive;
  const totalCount = upload.tasks.length + download.tasks.length + fileOp.tasks.length;
  const doneCount =
    upload.tasks.filter((t) => t.status === 'done').length +
    download.tasks.filter((t) => t.status === 'done').length +
    fileOp.tasks.filter((t) => t.status === 'done').length;

  const closeAll = () => {
    upload.clearFinished();
    download.clearFinished();
    fileOp.clearFinished();
    if (uploadActive === 0) upload.closePanel();
    if (downloadActive === 0) download.closePanel();
    if (fileOpActive === 0) fileOp.closePanel();
  };

  return (
    <div className="fixed bottom-12 right-4 z-[160] w-80 max-w-[calc(100vw-2rem)] rounded-xl bg-white dark:bg-slate-900 shadow-2xl border border-slate-200 dark:border-slate-800 overflow-hidden">
      {/* 头部 */}
      <div className="flex items-center justify-between px-3.5 py-2.5 border-b border-slate-100 dark:border-slate-800 bg-slate-50 dark:bg-slate-800/50">
        <div className="flex items-center gap-2 text-sm font-medium text-slate-700 dark:text-slate-200">
          <ArrowLeftRight className="w-4 h-4" />
          <span>
            传输 {active > 0 ? `（${active} 进行中）` : `（${doneCount}/${totalCount} 完成）`}
          </span>
        </div>
        <div className="flex items-center gap-0.5">
          <button
            onClick={() => setCollapsed((c) => !c)}
            className="p-1 text-slate-400 hover:text-slate-600 dark:hover:text-slate-200 rounded"
            title={collapsed ? '展开' : '收起'}
          >
            {collapsed ? <ChevronUp className="w-4 h-4" /> : <ChevronDown className="w-4 h-4" />}
          </button>
          <button
            onClick={closeAll}
            className="p-1 text-slate-400 hover:text-slate-600 dark:hover:text-slate-200 rounded"
            title="关闭"
          >
            <X className="w-4 h-4" />
          </button>
        </div>
      </div>

      {/* 任务列表：下载在上、文件操作居中、上传在下。 */}
      {!collapsed && (
        <div className="max-h-80 overflow-y-auto divide-y divide-slate-50 dark:divide-slate-800/50">
          {download.tasks.map((task) => (
            <div key={task.id}>
              <DownloadRow task={task} />
            </div>
          ))}
          {fileOp.tasks.map((task) => (
            <div key={task.id}>
              <FileOpRow task={task} onOpenDetail={() => setDetailTaskId(task.id)} />
            </div>
          ))}
          {upload.tasks.map((task) => (
            <div key={task.id}>
              <UploadRow task={task} />
            </div>
          ))}
        </div>
      )}

      <FileOpDetailModal
        taskId={detailTaskId}
        onClose={() => setDetailTaskId(null)}
      />
    </div>
  );
}

// DownloadRow 渲染一个打包下载任务。流式 zip 无总大小，进度为不确定态（仅显示已接收字节）。
function DownloadRow({ task }: { task: DownloadTask }) {
  const { cancelTask, removeTask } = useDownloadStore();
  return (
    <div className="px-3.5 py-2.5">
      <div className="flex items-center gap-2">
        <DownloadStatusIcon status={task.status} />
        <div className="flex-1 min-w-0">
          <div className="text-xs text-slate-700 dark:text-slate-200 truncate" title={task.name}>
            {task.name}
          </div>
          <div className="text-[10px] text-slate-400 tabular-nums">
            {downloadStatusLabel(task)}
          </div>
        </div>
        {task.status === 'downloading' ? (
          <button onClick={() => cancelTask(task.id)} className="p-1 text-slate-400 hover:text-rose-500 rounded shrink-0" title="取消">
            <Ban className="w-3.5 h-3.5" />
          </button>
        ) : (
          <button onClick={() => removeTask(task.id)} className="p-1 text-slate-400 hover:text-slate-600 dark:hover:text-slate-200 rounded shrink-0" title="移除">
            <X className="w-3.5 h-3.5" />
          </button>
        )}
      </div>

      {/* 不确定态进度条（无总大小，用滑动动画表示进行中） */}
      {task.status === 'downloading' && (
        <div className="mt-1.5 h-1 rounded-full bg-slate-100 dark:bg-slate-800 overflow-hidden">
          <div className="h-full w-1/3 bg-blue-500 animate-indeterminate rounded-full" />
        </div>
      )}

      {task.status === 'error' && task.error && (
        <div className="mt-1 text-[10px] text-rose-500">{task.error}</div>
      )}
    </div>
  );
}

function downloadStatusLabel(task: DownloadTask): string {
  switch (task.status) {
    case 'downloading':
      return `打包下载中 · 已接收 ${formatBytes(task.loaded)} · ${formatBytes(task.speed)}/s`;
    case 'done':
      return `完成 · ${formatBytes(task.loaded)}`;
    case 'error':
      return '失败';
    case 'canceled':
      return '已取消';
    default:
      return '';
  }
}

function DownloadStatusIcon({ status }: { status: DownloadStatus }) {
  switch (status) {
    case 'done':
      return <CheckCircle2 className="w-4 h-4 text-emerald-500 shrink-0" />;
    case 'error':
      return <AlertCircle className="w-4 h-4 text-rose-500 shrink-0" />;
    case 'canceled':
      return <Ban className="w-4 h-4 text-slate-400 shrink-0" />;
    default:
      return <Download className="w-4 h-4 text-blue-500 shrink-0" />;
  }
}

function UploadRow({ task }: { task: UploadTask }) {
  const { resolveOverwrite, resolveRename, cancelTask, removeTask } = useUploadStore();
  const [renaming, setRenaming] = useState(false);
  const [newName, setNewName] = useState(task.name);

  const pct = task.total > 0 ? Math.round((task.loaded / task.total) * 100) : 0;

  return (
    <div className="px-3.5 py-2.5">
      <div className="flex items-center gap-2">
        <UploadStatusIcon status={task.status} />
        <div className="flex-1 min-w-0">
          <div className="text-xs text-slate-700 dark:text-slate-200 truncate" title={task.name}>
            {task.name}
          </div>
          <div className="text-[10px] text-slate-400 tabular-nums">
            {uploadStatusLabel(task, pct)}
          </div>
        </div>
        <UploadRowActions
          task={task}
          onCancel={() => cancelTask(task.id)}
          onRemove={() => removeTask(task.id)}
        />
      </div>

      {/* 进度条（上传中显示） */}
      {task.status === 'uploading' && (
        <div className="mt-1.5 h-1 rounded-full bg-slate-100 dark:bg-slate-800 overflow-hidden">
          <div
            className="h-full bg-blue-500 transition-all duration-200"
            style={{ width: `${pct}%` }}
          />
        </div>
      )}

      {/* 冲突处理 */}
      {task.status === 'conflict' && (
        <div className="mt-2">
          {renaming ? (
            <div className="flex items-center gap-1.5">
              <input
                value={newName}
                autoFocus
                onChange={(e) => setNewName(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === 'Enter') {
                    resolveRename(task.id, newName);
                    setRenaming(false);
                  } else if (e.key === 'Escape') {
                    setRenaming(false);
                  }
                }}
                className="flex-1 min-w-0 px-2 py-1 text-xs bg-slate-50 dark:bg-slate-800 border border-slate-200 dark:border-slate-700 rounded outline-none focus:border-blue-400"
              />
              <button
                onClick={() => {
                  resolveRename(task.id, newName);
                  setRenaming(false);
                }}
                className="px-2 py-1 text-[11px] font-medium bg-blue-600 text-white rounded hover:bg-blue-700"
              >
                确定
              </button>
            </div>
          ) : (
            <div className="flex items-center gap-1.5 flex-wrap">
              <span className="text-[10px] text-amber-600 dark:text-amber-400 mr-1">同名文件已存在</span>
              <button
                onClick={() => resolveOverwrite(task.id)}
                className="px-2 py-0.5 text-[11px] font-medium text-rose-600 dark:text-rose-400 hover:bg-rose-50 dark:hover:bg-rose-900/20 rounded border border-rose-200 dark:border-rose-800"
              >
                覆盖
              </button>
              <button
                onClick={() => {
                  setNewName(task.name);
                  setRenaming(true);
                }}
                className="flex items-center gap-1 px-2 py-0.5 text-[11px] font-medium text-slate-600 dark:text-slate-300 hover:bg-slate-100 dark:hover:bg-slate-800 rounded border border-slate-200 dark:border-slate-700"
              >
                <Pencil className="w-3 h-3" /> 改名
              </button>
              <button
                onClick={() => cancelTask(task.id)}
                className="px-2 py-0.5 text-[11px] font-medium text-slate-500 hover:bg-slate-100 dark:hover:bg-slate-800 rounded"
              >
                取消
              </button>
            </div>
          )}
        </div>
      )}

      {/* 失败信息 */}
      {task.status === 'error' && task.error && (
        <div className="mt-1 text-[10px] text-rose-500">{task.error}</div>
      )}
    </div>
  );
}

function UploadStatusIcon({ status }: { status: UploadTask['status'] }) {
  switch (status) {
    case 'done':
      return <CheckCircle2 className="w-4 h-4 text-emerald-500 shrink-0" />;
    case 'error':
      return <AlertCircle className="w-4 h-4 text-rose-500 shrink-0" />;
    case 'conflict':
      return <AlertCircle className="w-4 h-4 text-amber-500 shrink-0" />;
    case 'canceled':
      return <Ban className="w-4 h-4 text-slate-400 shrink-0" />;
    case 'uploading':
      return <Loader2 className="w-4 h-4 text-blue-500 animate-spin shrink-0" />;
    default:
      return <Loader2 className="w-4 h-4 text-slate-300 shrink-0" />;
  }
}

function uploadStatusLabel(task: UploadTask, pct: number): string {
  switch (task.status) {
    case 'pending':
      return '等待中…';
    case 'uploading':
      return `${pct}% · ${formatBytes(task.loaded)} / ${formatBytes(task.total)} · ${formatBytes(task.speed)}/s`;
    case 'done':
      return `完成 · ${formatBytes(task.total)}`;
    case 'conflict':
      return '需要确认';
    case 'error':
      return '失败';
    case 'canceled':
      return '已取消';
    default:
      return '';
  }
}

function UploadRowActions({
  task,
  onCancel,
  onRemove,
}: {
  task: UploadTask;
  onCancel: () => void;
  onRemove: () => void;
}) {
  if (task.status === 'uploading' || task.status === 'pending') {
    return (
      <button
        onClick={onCancel}
        className="p-1 text-slate-400 hover:text-rose-500 rounded shrink-0"
        title="取消"
      >
        <Ban className="w-3.5 h-3.5" />
      </button>
    );
  }
  if (task.status === 'done' || task.status === 'error' || task.status === 'canceled') {
    return (
      <button
        onClick={onRemove}
        className="p-1 text-slate-400 hover:text-slate-600 dark:hover:text-slate-200 rounded shrink-0"
        title="移除"
      >
        <X className="w-3.5 h-3.5" />
      </button>
    );
  }
  return null;
}

// FileOpRow 渲染一个异步文件操作任务（copy/move/delete）。
// 进度：项内字节进度（curCopied/curSize）+ 总进度（doneItems/totalItems）。
// 点击主体区域打开详情 Modal（逐项状态）。
function FileOpRow({ task, onOpenDetail }: { task: FileOpTask; onOpenDetail: () => void }) {
  const { cancelTask, removeTask } = useFileOpStore();
  const curPct = task.curSize > 0 ? Math.min(100, Math.round((task.curCopied / task.curSize) * 100)) : 0;
  const totPct = task.totalItems > 0 ? Math.round((task.doneItems / task.totalItems) * 100) : 0;

  return (
    <div className="px-3.5 py-2.5">
      <div className="flex items-center gap-2">
        <FileOpStatusIcon task={task} />
        <div
          className="flex-1 min-w-0 cursor-pointer hover:text-slate-900 dark:hover:text-white transition-colors"
          onClick={onOpenDetail}
          title="点击查看详情"
        >
          <div className="text-xs text-slate-700 dark:text-slate-200 truncate" title={fileOpTitle(task)}>
            {fileOpTitle(task)}
          </div>
          <div className="text-[10px] text-slate-400 tabular-nums">
            {fileOpStatusLabel(task, totPct, curPct)}
          </div>
        </div>
        {task.status === 'queued' || task.status === 'running' ? (
          <button
            onClick={() => cancelTask(task.id)}
            className="p-1 text-slate-400 hover:text-rose-500 rounded shrink-0"
            title="取消"
          >
            <Ban className="w-3.5 h-3.5" />
          </button>
        ) : (
          <button
            onClick={() => removeTask(task.id)}
            className="p-1 text-slate-400 hover:text-slate-600 dark:hover:text-slate-200 rounded shrink-0"
            title="移除"
          >
            <X className="w-3.5 h-3.5" />
          </button>
        )}
      </div>

      {/* 进度条：运行中且当前项有大小时显示项内字节进度；否则显示总进度。 */}
      {task.status === 'running' && (
        <div className="mt-1.5 h-1 rounded-full bg-slate-100 dark:bg-slate-800 overflow-hidden">
          <div
            className="h-full bg-blue-500 transition-all duration-200"
            style={{ width: `${task.curSize > 0 ? curPct : totPct}%` }}
          />
        </div>
      )}
      {task.status === 'queued' && (
        <div className="mt-1.5 h-1 rounded-full bg-slate-100 dark:bg-slate-800 overflow-hidden">
          <div className="h-full w-1/3 bg-slate-300 dark:bg-slate-600 animate-indeterminate rounded-full" />
        </div>
      )}

      {task.status === 'error' && task.error && (
        <div className="mt-1 text-[10px] text-rose-500">{task.error}</div>
      )}
      {task.status === 'done' && task.results && task.results.some((r) => !r.ok) && (
        <div className="mt-1 text-[10px] text-amber-500">
          {task.results.filter((r) => !r.ok).length} 项失败
        </div>
      )}
    </div>
  );
}

function fileOpTitle(task: FileOpTask): string {
  const label = task.op === 'copy' ? '复制' : task.op === 'move' ? '移动' : '删除';
  if (task.curName) return `${label} · ${task.curName}`;
  const cnt = task.totalItems;
  return `${label} · ${cnt} 项`;
}

function fileOpStatusLabel(task: FileOpTask, totPct: number, curPct: number): string {
  switch (task.status) {
    case 'queued':
      return `排队中 · ${task.doneItems}/${task.totalItems} 项`;
    case 'running':
      if (task.curSize > 0) {
        return `${curPct}% · ${formatBytes(task.curCopied)} / ${formatBytes(task.curSize)} · 总 ${totPct}% (${task.doneItems}/${task.totalItems}) · ${formatBytes(task.speed)}/s`;
      }
      return `${task.doneItems}/${task.totalItems} 项 · ${totPct}%`;
    case 'done':
      return `完成 · ${task.totalItems} 项`;
    case 'canceled': {
      // 取消时可能部分项已完成——展示明细让用户知道哪些成功。
      const ok = task.items?.filter((i) => i && i.status === 'done').length ?? 0;
      const skip = task.items?.filter((i) => i && (i.status === 'skipped' || i.status === 'canceled')).length ?? 0;
      if (ok > 0) return `已取消 · 成功 ${ok} · 未完成 ${skip}`;
      return '已取消';
    }
    case 'error':
      return '失败';
    default:
      return '';
  }
}

function FileOpStatusIcon({ task }: { task: FileOpTask }) {
  if (task.status === 'done') return <CheckCircle2 className="w-4 h-4 text-emerald-500 shrink-0" />;
  if (task.status === 'error') return <AlertCircle className="w-4 h-4 text-rose-500 shrink-0" />;
  if (task.status === 'canceled') return <Ban className="w-4 h-4 text-slate-400 shrink-0" />;
  if (task.status === 'queued') return <Loader2 className="w-4 h-4 text-slate-300 shrink-0" />;
  // running：按操作类型区分图标（彩色），与上传的旋转 Loader 区分。
  if (task.op === 'delete') return <Trash2 className="w-4 h-4 text-amber-500 shrink-0" />;
  if (task.op === 'move') return <FolderInput className="w-4 h-4 text-violet-500 shrink-0" />;
  return <Copy className="w-4 h-4 text-blue-500 shrink-0" />;
}

// FileOpDetailModal 展示任务的逐项明细：每项的名称、状态（成功/失败/取消/跳过/进行中）、错误。
// 运行中由 item_start/item_done 事件重建 items[]；完成后由 finished 携带的 results[] 覆盖。
function FileOpDetailModal({ taskId, onClose }: { taskId: string | null; onClose: () => void }) {
  const tasks = useFileOpStore((s) => s.tasks);
  const task = tasks.find((t) => t.id === taskId) ?? null;

  const opLabel = (op: string) => (op === 'copy' ? '复制' : op === 'move' ? '移动' : '删除');
  const statusLabel = (s: string) =>
    s === 'queued' ? '排队中' : s === 'running' ? '进行中' : s === 'done' ? '完成' : s === 'canceled' ? '已取消' : s === 'error' ? '失败' : s;

  const items = task?.items;
  // 汇总：成功 / 失败 / 未完成（guard i 防御稀疏 / 未初始化）。
  const okCnt = items?.filter((i) => i && i.status === 'done').length ?? 0;
  const failCnt = items?.filter((i) => i && i.status === 'failed').length ?? 0;
  const pendCnt = items?.filter((i) => i && (i.status === 'skipped' || i.status === 'canceled' || i.status === 'pending' || i.status === 'running')).length ?? 0;

  return (
    <Modal
      isOpen={!!task}
      onClose={onClose}
      title={task ? `${opLabel(task.op)}任务详情` : ''}
      maxWidth="lg"
    >
      {task && (
        <div className="space-y-3">
          {/* 汇总栏 */}
          <div className="flex items-center gap-3 text-xs">
            <span className="flex items-center gap-1 text-slate-600 dark:text-slate-300">
              <CheckCircle2 className="w-3.5 h-3.5 text-emerald-500" />
              成功 {okCnt}
            </span>
            {failCnt > 0 && (
              <span className="flex items-center gap-1 text-slate-600 dark:text-slate-300">
                <AlertCircle className="w-3.5 h-3.5 text-rose-500" />
                失败 {failCnt}
              </span>
            )}
            {pendCnt > 0 && (
              <span className="flex items-center gap-1 text-slate-600 dark:text-slate-300">
                <MinusCircle className="w-3.5 h-3.5 text-slate-400" />
                未完成 {pendCnt}
              </span>
            )}
            <span className="ml-auto text-slate-400">{statusLabel(task.status)}</span>
          </div>

          {/* 逐项列表 */}
          {items && items.length > 0 ? (
            <ul className="divide-y divide-slate-100 dark:divide-slate-800/50">
              {items.filter((it) => it).map((it) => (
                <li key={it.index} className="flex items-center gap-2 py-1.5">
                  <FileOpItemIcon status={it.status} />
                  <span className="flex-1 min-w-0 text-xs text-slate-700 dark:text-slate-200 truncate" title={it.name}>
                    {it.name}
                  </span>
                  {it.size > 0 && (
                    <span className="text-[10px] text-slate-400 tabular-nums shrink-0">
                      {formatBytes(it.size)}
                    </span>
                  )}
                  {it.error && (
                    <span className="text-[10px] text-rose-400 shrink-0">{itemErrorLabel(it.error)}</span>
                  )}
                </li>
              ))}
            </ul>
          ) : (
            <div className="text-xs text-slate-400 text-center py-4">暂无项明细</div>
          )}

          {/* 整体错误（如目标非法） */}
          {task.error && (
            <div className="text-xs text-rose-500 bg-rose-50 dark:bg-rose-900/20 rounded px-2 py-1.5">
              {task.error}
            </div>
          )}
        </div>
      )}
    </Modal>
  );
}

// FileOpItemIcon 单项状态图标。
function FileOpItemIcon({ status }: { status: FileOpItem['status'] }) {
  switch (status) {
    case 'done':
      return <CheckCircle2 className="w-3.5 h-3.5 text-emerald-500 shrink-0" />;
    case 'failed':
      return <AlertCircle className="w-3.5 h-3.5 text-rose-500 shrink-0" />;
    case 'canceled':
    case 'skipped':
      return <MinusCircle className="w-3.5 h-3.5 text-slate-400 shrink-0" />;
    case 'running':
      return <Loader2 className="w-3.5 h-3.5 text-blue-500 shrink-0 animate-spin" />;
    case 'pending':
    default:
      return <Clock className="w-3.5 h-3.5 text-slate-300 shrink-0" />;
  }
}

// itemErrorLabel 将单项错误码名翻译为中文。
function itemErrorLabel(err: string): string {
  switch (err) {
    case 'canceled':
      return '已取消';
    case 'skipped':
      return '未执行';
    case 'path_not_found':
      return '路径不存在';
    case 'path_traversal':
      return '路径越界';
    case 'file_exists':
      return '已存在';
    case 'disk_full':
      return '磁盘空间不足';
    case 'permission_denied':
      return '无权限';
    case 'bad_request':
    case 'bad_op':
      return '操作非法';
    default:
      return err;
  }
}

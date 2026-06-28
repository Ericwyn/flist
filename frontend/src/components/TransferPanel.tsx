import React, { useState } from 'react';
import { useUploadStore } from '../uploadStore';
import { useDownloadStore, DownloadTask, DownloadStatus } from '../downloadStore';
import { UploadTask } from '../types';
import {
  X, ChevronDown, ChevronUp, CheckCircle2, AlertCircle, Loader2,
  ArrowLeftRight, Ban, Pencil, Download,
} from 'lucide-react';

const formatBytes = (bytes: number, decimals = 1) => {
  if (!+bytes) return '0 B';
  const k = 1024;
  const sizes = ['B', 'KB', 'MB', 'GB', 'TB'];
  const i = Math.floor(Math.log(bytes) / Math.log(k));
  return `${parseFloat((bytes / Math.pow(k, i)).toFixed(decimals))} ${sizes[i]}`;
};

// TransferPanel 固定在右下角，统一展示上传与打包下载任务的进度、状态与操作入口。
export function TransferPanel() {
  const upload = useUploadStore();
  const download = useDownloadStore();
  const [collapsed, setCollapsed] = useState(false);

  const open = (upload.panelOpen && upload.tasks.length > 0) || (download.panelOpen && download.tasks.length > 0);
  if (!open) return null;

  // 进行中 / 总数统计（上传 + 下载合并）。
  const uploadActive = upload.tasks.filter((t) => t.status === 'uploading' || t.status === 'pending').length;
  const downloadActive = download.tasks.filter((t) => t.status === 'downloading').length;
  const active = uploadActive + downloadActive;
  const totalCount = upload.tasks.length + download.tasks.length;
  const doneCount =
    upload.tasks.filter((t) => t.status === 'done').length +
    download.tasks.filter((t) => t.status === 'done').length;

  const closeAll = () => {
    upload.clearFinished();
    download.clearFinished();
    if (uploadActive === 0) upload.closePanel();
    if (downloadActive === 0) download.closePanel();
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

      {/* 任务列表：下载在上、上传在下（最近发起的下载更受关注）。 */}
      {!collapsed && (
        <div className="max-h-80 overflow-y-auto divide-y divide-slate-50 dark:divide-slate-800/50">
          {download.tasks.map((task) => (
            <div key={task.id}>
              <DownloadRow task={task} />
            </div>
          ))}
          {upload.tasks.map((task) => (
            <div key={task.id}>
              <UploadRow task={task} />
            </div>
          ))}
        </div>
      )}
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

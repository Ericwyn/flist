import React, { useState } from 'react';
import { useUploadStore } from '../uploadStore';
import { UploadTask } from '../types';
import {
  X, ChevronDown, ChevronUp, CheckCircle2, AlertCircle, Loader2,
  FileUp, Ban, Pencil,
} from 'lucide-react';

const formatBytes = (bytes: number, decimals = 1) => {
  if (!+bytes) return '0 B';
  const k = 1024;
  const sizes = ['B', 'KB', 'MB', 'GB', 'TB'];
  const i = Math.floor(Math.log(bytes) / Math.log(k));
  return `${parseFloat((bytes / Math.pow(k, i)).toFixed(decimals))} ${sizes[i]}`;
};

// UploadPanel 固定在右下角，展示上传任务进度、状态与冲突处理入口。
export function UploadPanel() {
  const { tasks, panelOpen, closePanel, clearFinished } = useUploadStore();
  const [collapsed, setCollapsed] = useState(false);

  if (!panelOpen || tasks.length === 0) return null;

  const active = tasks.filter((t) => t.status === 'uploading' || t.status === 'pending').length;
  const done = tasks.filter((t) => t.status === 'done').length;

  return (
    <div className="fixed bottom-4 right-4 z-[160] w-80 max-w-[calc(100vw-2rem)] rounded-xl bg-white dark:bg-slate-900 shadow-2xl border border-slate-200 dark:border-slate-800 overflow-hidden">
      {/* 头部 */}
      <div className="flex items-center justify-between px-3.5 py-2.5 border-b border-slate-100 dark:border-slate-800 bg-slate-50 dark:bg-slate-800/50">
        <div className="flex items-center gap-2 text-sm font-medium text-slate-700 dark:text-slate-200">
          <FileUp className="w-4 h-4" />
          <span>
            上传 {active > 0 ? `（${active} 进行中）` : `（${done}/${tasks.length} 完成）`}
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
            onClick={() => {
              clearFinished();
              if (active === 0) closePanel();
            }}
            className="p-1 text-slate-400 hover:text-slate-600 dark:hover:text-slate-200 rounded"
            title="关闭"
          >
            <X className="w-4 h-4" />
          </button>
        </div>
      </div>

      {/* 任务列表 */}
      {!collapsed && (
        <div className="max-h-80 overflow-y-auto divide-y divide-slate-50 dark:divide-slate-800/50">
          {tasks.map((task) => (
            <div key={task.id}>
              <TaskRow task={task} />
            </div>
          ))}
        </div>
      )}
    </div>
  );
}

function TaskRow({ task }: { task: UploadTask }) {
  const { resolveOverwrite, resolveRename, cancelTask, removeTask } = useUploadStore();
  const [renaming, setRenaming] = useState(false);
  const [newName, setNewName] = useState(task.name);

  const pct = task.total > 0 ? Math.round((task.loaded / task.total) * 100) : 0;

  return (
    <div className="px-3.5 py-2.5">
      <div className="flex items-center gap-2">
        <StatusIcon status={task.status} />
        <div className="flex-1 min-w-0">
          <div className="text-xs text-slate-700 dark:text-slate-200 truncate" title={task.name}>
            {task.name}
          </div>
          <div className="text-[10px] text-slate-400 tabular-nums">
            {statusLabel(task, pct)}
          </div>
        </div>
        <RowActions
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

function StatusIcon({ status }: { status: UploadTask['status'] }) {
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

function statusLabel(task: UploadTask, pct: number): string {
  switch (task.status) {
    case 'pending':
      return '等待中…';
    case 'uploading':
      return `${pct}% · ${formatBytes(task.loaded)} / ${formatBytes(task.total)}`;
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

function RowActions({
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

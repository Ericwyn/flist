import React from 'react';
import { AlertTriangle } from 'lucide-react';

interface ConflictDialogProps {
  saving: boolean;
  onForceSave: () => void;
  onReload: () => void;
  onCopyAndCancel: () => void;
}

// ConflictDialog 是保存冲突（文件被外部修改）时的处理弹窗：
// 强制覆盖 / 丢弃本地改动重新加载 / 复制本地内容到剪贴板后取消。
// 整页编辑器与预览模态框共用同一交互。
export function ConflictDialog({ saving, onForceSave, onReload, onCopyAndCancel }: ConflictDialogProps) {
  return (
    <div className="fixed inset-0 z-[300] flex items-center justify-center bg-black/40">
      <div className="w-full max-w-md bg-white dark:bg-slate-900 rounded-xl shadow-xl border border-slate-200 dark:border-slate-700 p-5">
        <div className="flex items-center gap-2 mb-3 text-amber-600 dark:text-amber-400">
          <AlertTriangle className="w-5 h-5" />
          <h3 className="font-medium">文件已被外部修改</h3>
        </div>
        <p className="text-sm text-slate-600 dark:text-slate-300 mb-4">
          此文件自你打开后已被其他窗口或进程修改。请选择如何处理你的改动。
        </p>
        <div className="flex flex-col gap-2">
          <button
            onClick={onForceSave}
            disabled={saving}
            className="px-3 py-2 text-sm font-medium bg-rose-600 text-white rounded-lg hover:bg-rose-700 disabled:opacity-40 transition-colors"
          >
            强制覆盖（用我的内容保存）
          </button>
          <button
            onClick={onReload}
            className="px-3 py-2 text-sm font-medium bg-slate-100 dark:bg-slate-800 text-slate-700 dark:text-slate-200 rounded-lg hover:bg-slate-200 dark:hover:bg-slate-700 transition-colors"
          >
            丢弃我的改动，重新加载远端
          </button>
          <button
            onClick={onCopyAndCancel}
            className="px-3 py-2 text-sm text-slate-500 dark:text-slate-400 rounded-lg hover:bg-slate-100 dark:hover:bg-slate-800 transition-colors"
          >
            复制我的内容到剪贴板并取消
          </button>
        </div>
      </div>
    </div>
  );
}

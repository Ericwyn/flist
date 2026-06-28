import React, { useEffect, useRef, useState } from 'react';
import { Loader2 } from 'lucide-react';

interface SaveAsDialogProps {
  defaultPath: string;
  saving: boolean;
  // exists 为 true 时进入「目标已存在，确认覆盖」态。
  exists: boolean;
  error?: string | null;
  // onSubmit 提交目标路径与是否覆盖。覆盖确认由父组件根据 exists 调用 onSubmit(path, true)。
  onSubmit: (path: string, overwrite: boolean) => void;
  onCancel: () => void;
}

// SaveAsDialog 是「另存为」输入弹窗：用户填写目标完整路径，提交后由调用方走 touch + force 保存。
// 目标已存在时（exists）切换为覆盖确认。整页编辑器与预览模态框共用。
export function SaveAsDialog({ defaultPath, saving, exists, error, onSubmit, onCancel }: SaveAsDialogProps) {
  const [path, setPath] = useState(defaultPath);
  const inputRef = useRef<HTMLInputElement>(null);

  useEffect(() => {
    const el = inputRef.current;
    if (!el) return;
    el.focus();
    // 选中文件名部分（最后一个 / 之后到扩展名前），便于直接改名。
    const slash = path.lastIndexOf('/');
    const dot = path.lastIndexOf('.');
    const start = slash + 1;
    const end = dot > slash ? dot : path.length;
    el.setSelectionRange(start, end);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const submit = () => {
    const p = path.trim();
    if (!p || saving) return;
    onSubmit(p, exists);
  };

  return (
    <div className="fixed inset-0 z-[300] flex items-center justify-center bg-black/40">
      <div className="w-full max-w-md bg-white dark:bg-slate-900 rounded-xl shadow-xl border border-slate-200 dark:border-slate-700 p-5">
        <h3 className="font-medium text-slate-900 dark:text-white mb-3">另存为</h3>
        <input
          ref={inputRef}
          value={path}
          onChange={(e) => setPath(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === 'Enter') submit();
            if (e.key === 'Escape') onCancel();
          }}
          spellCheck={false}
          className="w-full px-3 py-2 text-sm font-mono rounded-lg border border-slate-200 dark:border-slate-700 bg-white dark:bg-slate-800 text-slate-800 dark:text-slate-100 focus:outline-none focus:ring-2 focus:ring-blue-500/40"
          placeholder="/目标/路径/文件名"
        />
        {exists && (
          <p className="mt-2 text-[12px] text-amber-600 dark:text-amber-400">
            目标已存在，继续将覆盖该文件。
          </p>
        )}
        {error && <p className="mt-2 text-[12px] text-rose-600 dark:text-rose-400">{error}</p>}
        <div className="mt-4 flex justify-end gap-2">
          <button
            onClick={onCancel}
            className="px-3 py-1.5 text-xs font-medium text-slate-600 dark:text-slate-300 rounded-lg hover:bg-slate-100 dark:hover:bg-slate-800 transition-colors"
          >
            取消
          </button>
          <button
            onClick={submit}
            disabled={saving || !path.trim()}
            className={
              'flex items-center gap-1.5 px-3 py-1.5 text-xs font-medium text-white rounded-lg disabled:opacity-40 disabled:cursor-not-allowed transition-colors ' +
              (exists ? 'bg-rose-600 hover:bg-rose-700' : 'bg-blue-600 hover:bg-blue-700')
            }
          >
            {saving && <Loader2 className="w-3.5 h-3.5 animate-spin" />}
            <span>{exists ? '覆盖保存' : '保存'}</span>
          </button>
        </div>
      </div>
    </div>
  );
}

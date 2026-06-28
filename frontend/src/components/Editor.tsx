import React, { useEffect, useState } from 'react';
import { Loader2, Save, FileWarning, FileDown, Undo2 } from 'lucide-react';
import { useFileEditor } from '../lib/useFileEditor';
import { formatBytes } from '../lib/utils';
import { ConflictDialog } from './ConflictDialog';
import { SaveAsDialog } from './SaveAsDialog';

// editorPathFromUrl 从 /editor?path=... 解析目标文件 API 路径。
function editorPathFromUrl(): string {
  const params = new URLSearchParams(window.location.search);
  return params.get('path') || '';
}

// Editor 是独立的文本编辑器页面（通过 /editor?path=... 打开，支持新窗口）。
export function Editor() {
  const [path] = useState(editorPathFromUrl);
  const ed = useFileEditor(path);
  const { meta, loading, loadError, dirty, saving, saveError, conflict, savedAt, notEditable } = ed;

  // 另存为弹窗态。
  const [saveAsOpen, setSaveAsOpen] = useState(false);
  const [saveAsExists, setSaveAsExists] = useState(false);
  const [saveAsError, setSaveAsError] = useState<string | null>(null);
  const [saveAsBusy, setSaveAsBusy] = useState(false);

  useEffect(() => {
    if (meta) document.title = `${meta.name} - 编辑 - Flist`;
  }, [meta]);

  // 未保存离开提醒。
  useEffect(() => {
    const onBeforeUnload = (e: BeforeUnloadEvent) => {
      if (dirty) {
        e.preventDefault();
        e.returnValue = '';
      }
    };
    window.addEventListener('beforeunload', onBeforeUnload);
    return () => window.removeEventListener('beforeunload', onBeforeUnload);
  }, [dirty]);

  const submitSaveAs = async (target: string, overwrite: boolean) => {
    setSaveAsBusy(true);
    setSaveAsError(null);
    const res = await ed.saveAs(target, overwrite);
    setSaveAsBusy(false);
    if (res.ok) {
      setSaveAsOpen(false);
      setSaveAsExists(false);
      return;
    }
    if (res.exists) {
      setSaveAsExists(true);
      return;
    }
    setSaveAsError(res.error ?? '另存为失败');
  };

  if (loading) {
    return (
      <div className="flex h-screen w-full items-center justify-center bg-[#f8fafc] dark:bg-slate-900">
        <Loader2 className="w-6 h-6 text-blue-500 animate-spin" />
      </div>
    );
  }

  if (loadError) {
    return (
      <div className="flex h-screen w-full flex-col items-center justify-center gap-3 bg-[#f8fafc] dark:bg-slate-900 text-slate-600 dark:text-slate-300">
        <FileWarning className="w-10 h-10 text-amber-500" />
        <p className="text-sm">{loadError}</p>
        <button onClick={() => window.close()} className="text-xs text-blue-600 hover:underline">
          关闭
        </button>
      </div>
    );
  }

  const downloadUrl = meta ? `/api/fs/download?path=${encodeURIComponent(meta.path)}&download=1` : '#';

  return (
    <div className="flex h-screen w-full flex-col bg-white dark:bg-slate-950 text-slate-800 dark:text-slate-100">
      {/* 顶部信息栏 */}
      <div className="h-12 shrink-0 flex items-center gap-3 px-4 border-b border-slate-200 dark:border-slate-800">
        <div className="min-w-0 flex-1">
          <div className="flex items-center gap-2 text-sm font-medium truncate">
            <span className="truncate">{meta?.name}</span>
            {dirty && <span className="w-1.5 h-1.5 rounded-full bg-amber-500 shrink-0" title="有未保存的修改" />}
          </div>
          <div className="text-[11px] text-slate-400 dark:text-slate-500 truncate">
            {meta?.path} · {formatBytes(meta?.size ?? 0)} · {meta?.encoding}
            {meta?.readonly && <span className="text-amber-500 ml-1">· 只读</span>}
          </div>
        </div>
        {savedAt && !dirty && (
          <span className="text-[11px] text-emerald-600 dark:text-emerald-400 shrink-0">已保存</span>
        )}
        <a
          href={downloadUrl}
          download={meta?.name}
          className="flex items-center gap-1.5 px-3 py-1.5 text-xs font-medium bg-slate-100 dark:bg-slate-800 text-slate-700 dark:text-slate-200 rounded-lg hover:bg-slate-200 dark:hover:bg-slate-700 transition-colors"
        >
          <FileDown className="w-3.5 h-3.5" />
          <span>下载</span>
        </a>
        <button
          onClick={ed.restore}
          disabled={!dirty}
          className="flex items-center gap-1.5 px-3 py-1.5 text-xs font-medium bg-slate-100 dark:bg-slate-800 text-slate-700 dark:text-slate-200 rounded-lg hover:bg-slate-200 dark:hover:bg-slate-700 disabled:opacity-40 disabled:cursor-not-allowed transition-colors"
          title="恢复为打开时的内容"
        >
          <Undo2 className="w-3.5 h-3.5" />
          <span>恢复</span>
        </button>
        <button
          onClick={() => {
            setSaveAsExists(false);
            setSaveAsError(null);
            setSaveAsOpen(true);
          }}
          disabled={!!notEditable}
          className="flex items-center gap-1.5 px-3 py-1.5 text-xs font-medium bg-slate-100 dark:bg-slate-800 text-slate-700 dark:text-slate-200 rounded-lg hover:bg-slate-200 dark:hover:bg-slate-700 disabled:opacity-40 disabled:cursor-not-allowed transition-colors"
        >
          <span>另存为</span>
        </button>
        <button
          onClick={() => void ed.doSave(false)}
          disabled={saving || !dirty || !!notEditable}
          className="flex items-center gap-1.5 px-3 py-1.5 text-xs font-medium bg-blue-600 text-white rounded-lg hover:bg-blue-700 disabled:opacity-40 disabled:cursor-not-allowed transition-colors"
          title="保存（Ctrl/Cmd+S）"
        >
          {saving ? <Loader2 className="w-3.5 h-3.5 animate-spin" /> : <Save className="w-3.5 h-3.5" />}
          <span>保存</span>
        </button>
      </div>

      {saveError && (
        <div className="px-4 py-1.5 text-[11px] text-rose-600 dark:text-rose-400 bg-rose-50 dark:bg-rose-900/20 shrink-0">
          {saveError}
        </div>
      )}

      {/* 编辑区 / 不可编辑提示 */}
      {notEditable ? (
        <div className="flex-1 flex flex-col items-center justify-center gap-3 text-slate-500">
          <FileWarning className="w-10 h-10 text-amber-500" />
          <p className="text-sm">{meta?.readonly ? '该文件为只读，无法编辑' : '该文件不可作为文本编辑'}</p>
        </div>
      ) : (
        <div ref={ed.hostRef} className="flex-1 overflow-auto text-sm" />
      )}

      {conflict && (
        <ConflictDialog
          saving={saving}
          onForceSave={() => void ed.doSave(true)}
          onReload={() => void ed.reloadRemote()}
          onCopyAndCancel={async () => {
            await ed.copyContentToClipboard();
            ed.dismissConflict();
          }}
        />
      )}

      {saveAsOpen && meta && (
        <SaveAsDialog
          defaultPath={meta.path}
          saving={saveAsBusy}
          exists={saveAsExists}
          error={saveAsError}
          onSubmit={(p, overwrite) => void submitSaveAs(p, overwrite)}
          onCancel={() => {
            setSaveAsOpen(false);
            setSaveAsExists(false);
            setSaveAsError(null);
          }}
        />
      )}
    </div>
  );
}

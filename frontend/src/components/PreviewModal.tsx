import React, { useCallback, useEffect, useState } from 'react';
import { useFsStore } from '../fsStore';
import { api, getToken } from '../lib/api';
import { kindOf } from '../lib/path';
import { PreviewResult } from '../types';
import { Modal } from './Modal';
import { ConflictDialog } from './ConflictDialog';
import { SaveAsDialog } from './SaveAsDialog';
import { useFileEditor } from '../lib/useFileEditor';
import { formatBytes } from '../lib/utils';
import { Download, File, Loader2, Music, ExternalLink, Save, Undo2, FileWarning } from 'lucide-react';

export function PreviewModal() {
  const { previewEntry, previewPath, closePreview } = useFsStore();

  const kind = previewEntry ? kindOf(previewEntry) : 'unknown';
  const isTextKind = kind === 'text' || kind === 'unknown';
  const isPdf = kind === 'pdf';
  // 仅文本/未知类型走可编辑内容接口；媒体类型传空 path 让 hook 短路（不发请求、不挂载编辑器）。
  const ed = useFileEditor(isTextKind && previewPath ? previewPath : '');

  // 关闭拦截：有未保存改动时先二次确认。
  const [confirmClose, setConfirmClose] = useState(false);
  const requestClose = useCallback(() => {
    if (ed.dirty) {
      setConfirmClose(true);
      return;
    }
    closePreview();
  }, [ed.dirty, closePreview]);

  // 另存为弹窗态。
  const [saveAsOpen, setSaveAsOpen] = useState(false);
  const [saveAsExists, setSaveAsExists] = useState(false);
  const [saveAsError, setSaveAsError] = useState<string | null>(null);
  const [saveAsBusy, setSaveAsBusy] = useState(false);

  const submitSaveAs = useCallback(
    async (target: string, overwrite: boolean) => {
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
    },
    [ed],
  );

  if (!previewEntry || !previewPath) return null;

  const inlineUrl = api.fs.downloadUrl(previewPath);
  const downloadUrl = api.fs.downloadUrl(previewPath, { download: true });
  // 编辑器页面 URL（同源复用登录态，不在 URL 携带 token）。
  const editorUrl = `/editor?path=${encodeURIComponent(previewPath)}`;

  const meta = ed.meta;
  // 可编辑：文本内容已加载且后端判定可写。
  const editable = isTextKind && !!meta && meta.editable && !meta.readonly;
  // 过大文件回落到截断只读预览（编辑接口 2014）。
  const tooLarge = isTextKind && ed.loadErrorCode === 2014;

  const footer = (
    <div className="flex items-center gap-2">
      <a
        href={downloadUrl}
        download={previewEntry.name}
        className="flex items-center gap-1.5 px-3 py-1.5 text-xs font-medium bg-slate-100 dark:bg-slate-800 text-slate-700 dark:text-slate-200 rounded-lg hover:bg-slate-200 dark:hover:bg-slate-700 transition-colors"
      >
        <Download className="w-3.5 h-3.5" />
        <span>下载</span>
      </a>
      {(editable || tooLarge) && (
        <button
          onClick={() => window.open(editorUrl, '_blank', 'noopener')}
          className="flex items-center gap-1.5 px-3 py-1.5 text-xs font-medium bg-slate-100 dark:bg-slate-800 text-slate-700 dark:text-slate-200 rounded-lg hover:bg-slate-200 dark:hover:bg-slate-700 transition-colors"
        >
          <ExternalLink className="w-3.5 h-3.5" />
          <span>新窗口编辑</span>
        </button>
      )}
      {editable && (
        <>
          <span className="w-px h-5 bg-slate-200 dark:bg-slate-700" />
          <button
            onClick={ed.restore}
            disabled={!ed.dirty}
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
            className="px-3 py-1.5 text-xs font-medium bg-slate-100 dark:bg-slate-800 text-slate-700 dark:text-slate-200 rounded-lg hover:bg-slate-200 dark:hover:bg-slate-700 transition-colors"
          >
            另存为
          </button>
          <button
            onClick={() => void ed.doSave(false)}
            disabled={ed.saving || !ed.dirty}
            className="flex items-center gap-1.5 px-3 py-1.5 text-xs font-medium bg-blue-600 text-white rounded-lg hover:bg-blue-700 disabled:opacity-40 disabled:cursor-not-allowed transition-colors shadow-sm"
            title="保存（Ctrl/Cmd+S）"
          >
            {ed.saving ? <Loader2 className="w-3.5 h-3.5 animate-spin" /> : <Save className="w-3.5 h-3.5" />}
            <span>保存</span>
          </button>
        </>
      )}
    </div>
  );

  return (
    <Modal
      isOpen={true}
      onClose={requestClose}
      title={previewEntry.name}
      maxWidth={isPdf ? '6xl' : '4xl'}
      contentClassName="bg-slate-50 dark:bg-slate-950/50 p-0"
      footer={footer}
    >
      <div className="min-h-[40vh] max-h-[72vh]">
        {kind === 'image' && (
          <div className="flex items-center justify-center min-h-[40vh] max-h-[72vh]">
            <img src={inlineUrl} alt={previewEntry.name} className="max-w-full max-h-[70vh] object-contain" />
          </div>
        )}

        {kind === 'video' && (
          <div className="flex items-center justify-center min-h-[40vh] max-h-[72vh]">
            {/* 同源 HttpOnly Cookie 鉴权，支持 Range 拖拽。 */}
            <video src={inlineUrl} controls autoPlay className="max-w-full max-h-[70vh] outline-none" />
          </div>
        )}

        {kind === 'audio' && (
          <div className="flex items-center justify-center min-h-[40vh]">
            <div className="w-full max-w-md bg-white dark:bg-slate-900 p-8 rounded-2xl shadow-sm border border-slate-100 dark:border-slate-800 m-8">
              <div className="text-center mb-6">
                <div className="w-20 h-20 bg-purple-50 dark:bg-purple-900/30 rounded-2xl flex items-center justify-center mx-auto mb-4 border border-purple-100 dark:border-purple-800/50">
                  <Music className="w-10 h-10 text-purple-500 dark:text-purple-400" />
                </div>
                <h4 className="font-medium text-slate-900 dark:text-slate-100">{previewEntry.name}</h4>
              </div>
              <audio src={inlineUrl} controls autoPlay className="w-full outline-none" />
            </div>
          </div>
        )}

        {kind === 'pdf' && (
          <PdfPreview url={inlineUrl} title={previewEntry.name} downloadUrl={downloadUrl} />
        )}

        {isTextKind && (
          <div className="w-full">
            {ed.loading && (
              <div className="flex items-center justify-center min-h-[40vh] text-slate-400">
                <Loader2 className="w-6 h-6 animate-spin" />
              </div>
            )}

            {/* 过大：回落到截断只读预览。 */}
            {!ed.loading && tooLarge && <TextPreviewFallback path={previewPath} downloadUrl={downloadUrl} name={previewEntry.name} />}

            {/* 其他加载失败（如二进制 2013）：提示 + 下载。 */}
            {!ed.loading && ed.loadError && !tooLarge && (
              <div className="text-slate-500 flex flex-col items-center py-12">
                <File className="w-12 h-12 mb-3 opacity-50" />
                <p className="text-sm">{ed.loadError}</p>
                <a href={downloadUrl} download={previewEntry.name} className="mt-3 text-xs text-blue-600 hover:underline">
                  下载查看
                </a>
              </div>
            )}

            {/* 可编辑：内联 CodeMirror。 */}
            {!ed.loading && !ed.loadError && meta && (
              <div className="flex flex-col h-[60vh]">
                <div className="flex items-center gap-2 px-4 py-1.5 shrink-0 text-[11px] text-slate-400 dark:text-slate-500 border-b border-slate-100 dark:border-slate-800/50">
                  <span className="truncate">{formatBytes(meta.size)} · {meta.encoding}</span>
                  {meta.readonly && <span className="text-amber-500">· 只读</span>}
                  {ed.dirty && <span className="text-amber-500">· 未保存</span>}
                  {ed.savedAt && !ed.dirty && <span className="text-emerald-500">· 已保存</span>}
                </div>
                {ed.saveError && (
                  <div className="px-4 py-1.5 text-[11px] text-rose-600 dark:text-rose-400 bg-rose-50 dark:bg-rose-900/20 shrink-0">
                    {ed.saveError}
                  </div>
                )}
                {meta.editable && !meta.readonly ? (
                  <div ref={ed.hostRef} className="flex-1 overflow-auto text-sm bg-white dark:bg-slate-900" />
                ) : (
                  // 只读文本：展示内容但不挂载编辑器。
                  <div className="flex-1 flex flex-col">
                    <div className="flex items-center gap-2 px-4 py-2 text-[11px] text-amber-600 dark:text-amber-400 bg-amber-50 dark:bg-amber-900/20 shrink-0">
                      <FileWarning className="w-3.5 h-3.5" />
                      <span>该文件为只读，仅供查看</span>
                    </div>
                    <pre className="flex-1 overflow-auto p-4 bg-white dark:bg-slate-900 text-slate-800 dark:text-slate-200 font-mono text-sm whitespace-pre-wrap break-words">
                      {meta.content}
                    </pre>
                  </div>
                )}
              </div>
            )}
          </div>
        )}
      </div>

      {ed.conflict && (
        <ConflictDialog
          saving={ed.saving}
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

      {confirmClose && (
        <div className="fixed inset-0 z-[300] flex items-center justify-center bg-black/40">
          <div className="w-full max-w-sm bg-white dark:bg-slate-900 rounded-xl shadow-xl border border-slate-200 dark:border-slate-700 p-5">
            <div className="flex items-center gap-2 mb-3 text-amber-600 dark:text-amber-400">
              <FileWarning className="w-5 h-5" />
              <h3 className="font-medium">有未保存的修改</h3>
            </div>
            <p className="text-sm text-slate-600 dark:text-slate-300 mb-4">关闭后未保存的修改将丢失，确定要关闭吗？</p>
            <div className="flex justify-end gap-2">
              <button
                onClick={() => setConfirmClose(false)}
                className="px-3 py-1.5 text-xs font-medium text-slate-600 dark:text-slate-300 rounded-lg hover:bg-slate-100 dark:hover:bg-slate-800 transition-colors"
              >
                继续编辑
              </button>
              <button
                onClick={() => {
                  setConfirmClose(false);
                  closePreview();
                }}
                className="px-3 py-1.5 text-xs font-medium bg-rose-600 text-white rounded-lg hover:bg-rose-700 transition-colors"
              >
                放弃修改并关闭
              </button>
            </div>
          </div>
        </div>
      )}
    </Modal>
  );
}

// PdfPreview 先用 fetch 拉取 PDF，再用 blob: URL 嵌入浏览器 PDF viewer，避免后端 X-Frame-Options: deny 拦截 iframe。
function PdfPreview({ url, title, downloadUrl }: { url: string; title: string; downloadUrl: string }) {
  const [blobUrl, setBlobUrl] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    const ac = new AbortController();
    let objectUrl: string | null = null;

    const load = async () => {
      setLoading(true);
      setError(null);
      setBlobUrl(null);
      try {
        const headers: Record<string, string> = {};
        const token = getToken();
        if (token) headers.Authorization = `Bearer ${token}`;
        const resp = await fetch(url, {
          headers,
          credentials: 'same-origin',
          signal: ac.signal,
        });
        if (!resp.ok) throw new Error(`预览失败 (HTTP ${resp.status})`);
        const blob = await resp.blob();
        objectUrl = URL.createObjectURL(blob.type === 'application/pdf' ? blob : new Blob([blob], { type: 'application/pdf' }));
        setBlobUrl(objectUrl);
      } catch (e) {
        if (ac.signal.aborted) return;
        setError(e instanceof Error ? e.message : 'PDF 预览失败');
      } finally {
        if (!ac.signal.aborted) setLoading(false);
      }
    };

    void load();
    return () => {
      ac.abort();
      if (objectUrl) URL.revokeObjectURL(objectUrl);
    };
  }, [url]);

  return (
    <div className="h-[72vh] bg-slate-100 dark:bg-slate-950">
      {loading && (
        <div className="flex h-full items-center justify-center text-slate-400">
          <Loader2 className="w-6 h-6 animate-spin" />
        </div>
      )}
      {!loading && error && (
        <div className="h-full flex flex-col items-center justify-center text-slate-500 px-6 text-center">
          <File className="w-12 h-12 mb-3 opacity-50" />
          <p className="text-sm">{error}</p>
          <a href={downloadUrl} download={title} className="mt-3 text-xs text-blue-600 hover:underline">
            下载查看
          </a>
        </div>
      )}
      {!loading && blobUrl && (
        <iframe
          src={blobUrl}
          title={title}
          className="w-full h-full border-0 bg-white dark:bg-slate-900"
        />
      )}
    </div>
  );
}

// TextPreviewFallback 在文本过大无法编辑时回落到截断只读预览（沿用 fs/preview）。
function TextPreviewFallback({ path, downloadUrl, name }: { path: string; downloadUrl: string; name: string }) {
  const [data, setData] = useState<PreviewResult | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    setError(null);
    api.fs
      .preview(path)
      .then((res) => {
        if (!cancelled) setData(res);
      })
      .catch((e) => {
        if (!cancelled) setError(e instanceof Error ? e.message : '预览失败');
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [path]);

  if (loading) {
    return (
      <div className="flex items-center justify-center min-h-[40vh] text-slate-400">
        <Loader2 className="w-6 h-6 animate-spin" />
      </div>
    );
  }
  if (error || !data || data.type !== 'text') {
    return (
      <div className="text-slate-500 flex flex-col items-center py-12">
        <File className="w-12 h-12 mb-3 opacity-50" />
        <p className="text-sm">{error ?? '此文件类型暂不支持预览'}</p>
        <a href={downloadUrl} download={name} className="mt-3 text-xs text-blue-600 hover:underline">
          下载查看
        </a>
      </div>
    );
  }

  return (
    <div className="flex flex-col h-[60vh]">
      <div className="text-[11px] text-amber-600 dark:text-amber-400 bg-amber-50 dark:bg-amber-900/20 px-4 py-1.5 shrink-0">
        文件较大，仅显示前 {Math.round(data.previewBytes / 1024)} KB，无法在线编辑（如需编辑请下载）
      </div>
      <pre className="flex-1 overflow-auto p-4 bg-white dark:bg-slate-900 text-slate-800 dark:text-slate-200 font-mono text-sm whitespace-pre-wrap break-words">
        {data.content}
      </pre>
    </div>
  );
}

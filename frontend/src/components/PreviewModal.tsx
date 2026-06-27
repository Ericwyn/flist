import React, { useEffect, useState } from 'react';
import { useFsStore } from '../fsStore';
import { api } from '../lib/api';
import { kindOf } from '../lib/path';
import { PreviewResult } from '../types';
import { Modal } from './Modal';
import { Download, File, Loader2, Music } from 'lucide-react';

export function PreviewModal() {
  const { previewEntry, previewPath, closePreview } = useFsStore();
  const [textData, setTextData] = useState<PreviewResult | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const kind = previewEntry ? kindOf(previewEntry) : 'unknown';

  useEffect(() => {
    if (!previewEntry || !previewPath) return;
    setTextData(null);
    setError(null);

    // 仅文本/未知类型走 preview 接口；媒体类型直接用 download 内联。
    if (kind === 'text' || kind === 'unknown') {
      setLoading(true);
      api.fs
        .preview(previewPath)
        .then(setTextData)
        .catch((e) => setError(e instanceof Error ? e.message : '预览失败'))
        .finally(() => setLoading(false));
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [previewPath]);

  if (!previewEntry || !previewPath) return null;

  const inlineUrl = api.fs.downloadUrl(previewPath);
  const downloadUrl = api.fs.downloadUrl(previewPath, { download: true });

  const footer = (
    <a
      href={downloadUrl}
      download={previewEntry.name}
      className="flex items-center space-x-1.5 px-3 py-1.5 text-xs font-medium bg-blue-600 text-white rounded-lg hover:bg-blue-700 transition-colors shadow-sm"
    >
      <Download className="w-3.5 h-3.5" />
      <span>下载</span>
    </a>
  );

  return (
    <Modal
      isOpen={true}
      onClose={closePreview}
      title={previewEntry.name}
      maxWidth="4xl"
      contentClassName="bg-slate-50 dark:bg-slate-950/50 p-0"
      footer={footer}
    >
      <div className="flex items-center justify-center min-h-[40vh] max-h-[70vh]">
        {kind === 'image' && (
          <img src={inlineUrl} alt={previewEntry.name} className="max-w-full max-h-[70vh] object-contain" />
        )}

        {kind === 'video' && (
          // 同源 HttpOnly Cookie 鉴权，支持 Range 拖拽。
          <video src={inlineUrl} controls autoPlay className="max-w-full max-h-[70vh] outline-none" />
        )}

        {kind === 'audio' && (
          <div className="w-full max-w-md bg-white dark:bg-slate-900 p-8 rounded-2xl shadow-sm border border-slate-100 dark:border-slate-800 m-8">
            <div className="text-center mb-6">
              <div className="w-20 h-20 bg-purple-50 dark:bg-purple-900/30 rounded-2xl flex items-center justify-center mx-auto mb-4 border border-purple-100 dark:border-purple-800/50">
                <Music className="w-10 h-10 text-purple-500 dark:text-purple-400" />
              </div>
              <h4 className="font-medium text-slate-900 dark:text-slate-100">{previewEntry.name}</h4>
            </div>
            <audio src={inlineUrl} controls autoPlay className="w-full outline-none" />
          </div>
        )}

        {(kind === 'text' || kind === 'unknown') && (
          <div className="w-full">
            {loading && (
              <div className="flex items-center justify-center min-h-[40vh] text-slate-400">
                <Loader2 className="w-6 h-6 animate-spin" />
              </div>
            )}
            {error && (
              <div className="flex items-center justify-center min-h-[40vh] text-rose-500 text-sm">{error}</div>
            )}
            {!loading && !error && textData && textData.type === 'text' && (
              <div className="flex flex-col h-[60vh]">
                {textData.truncated && (
                  <div className="text-[11px] text-amber-600 dark:text-amber-400 bg-amber-50 dark:bg-amber-900/20 px-4 py-1.5 shrink-0">
                    内容较大，仅显示前 {Math.round(textData.previewBytes / 1024)} KB
                  </div>
                )}
                <pre className="flex-1 overflow-auto p-4 bg-white dark:bg-slate-900 text-slate-800 dark:text-slate-200 font-mono text-sm whitespace-pre-wrap break-words">
                  {textData.content}
                </pre>
              </div>
            )}
            {!loading && !error && textData && textData.type !== 'text' && (
              <div className="text-slate-500 flex flex-col items-center py-12">
                <File className="w-12 h-12 mb-3 opacity-50" />
                <p className="text-sm">此文件类型暂不支持预览</p>
                <a href={downloadUrl} download={previewEntry.name}
                  className="mt-3 text-xs text-blue-600 hover:underline">下载查看</a>
              </div>
            )}
          </div>
        )}
      </div>
    </Modal>
  );
}

import React, { useState, useEffect } from 'react';
import { useStore } from '../store';
import { Save, Music, File } from 'lucide-react';
import { Modal } from './Modal';

export function PreviewModal() {
  const { files, previewFileId, closePreview, saveFileContent } = useStore();
  const file = files.find(f => f.id === previewFileId);
  const [content, setContent] = useState('');

  useEffect(() => {
    if (file?.type === 'text') {
      setContent(file.content || '');
    }
  }, [file]);

  if (!file) return null;

  const handleSave = () => {
    saveFileContent(file.id, content);
    closePreview();
  };

  const footer = file.type === 'text' ? (
    <>
      <button
        onClick={closePreview}
        className="px-3 py-1.5 text-xs font-medium text-slate-600 dark:text-slate-400 hover:bg-slate-100 dark:hover:bg-slate-800 rounded-lg transition-colors"
      >
        取消
      </button>
      <button
        onClick={handleSave}
        className="flex items-center space-x-1.5 px-3 py-1.5 text-xs font-medium bg-blue-600 text-white rounded-lg hover:bg-blue-700 transition-colors shadow-sm"
      >
        <Save className="w-3.5 h-3.5" />
        <span>保存</span>
      </button>
    </>
  ) : undefined;

  return (
    <Modal
      isOpen={true}
      onClose={closePreview}
      title={file.name}
      maxWidth="4xl"
      contentClassName="bg-slate-50 dark:bg-slate-950/50 p-0"
      footer={footer}
    >
      <div className="flex items-center justify-center min-h-[40vh] max-h-[70vh]">
        {file.type === 'image' && (
          <img src={file.url} alt={file.name} className="max-w-full max-h-[70vh] object-contain" />
        )}
        
        {file.type === 'video' && (
          <video src={file.url} controls autoPlay className="max-w-full max-h-[70vh] outline-none" />
        )}
        
        {file.type === 'audio' && (
          <div className="w-full max-w-md bg-white dark:bg-slate-900 p-8 rounded-2xl shadow-sm border border-slate-100 dark:border-slate-800 m-8">
            <div className="text-center mb-6">
              <div className="w-20 h-20 bg-purple-50 dark:bg-purple-900/30 rounded-2xl flex items-center justify-center mx-auto mb-4 border border-purple-100 dark:border-purple-800/50">
                <Music className="w-10 h-10 text-purple-500 dark:text-purple-400" />
              </div>
              <h4 className="font-medium text-slate-900 dark:text-slate-100">{file.name}</h4>
            </div>
            <audio src={file.url} controls autoPlay className="w-full outline-none" />
          </div>
        )}
        
        {file.type === 'text' && (
          <textarea
            value={content}
            onChange={(e) => setContent(e.target.value)}
            className="w-full h-[60vh] p-4 bg-white dark:bg-slate-900 text-slate-800 dark:text-slate-200 font-mono text-sm outline-none resize-none"
            spellCheck={false}
          />
        )}

        {file.type === 'unknown' && (
          <div className="text-slate-500 flex flex-col items-center py-12">
            <File className="w-12 h-12 mb-3 opacity-50" />
            <p>此文件类型暂不支持预览</p>
          </div>
        )}
      </div>
    </Modal>
  );
}

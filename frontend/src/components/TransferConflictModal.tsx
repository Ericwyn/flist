import React, { useState } from 'react';
import { FolderSync, Files, X } from 'lucide-react';
import { Modal } from './Modal';
import { ConflictPolicy, PasteConflict } from '../types';

interface TransferConflictModalProps {
  conflict: PasteConflict;
  onChoose: (policy: ConflictPolicy) => void | Promise<void>;
  onClose: () => void;
}

// 只在存在同名目录时出现；同名文件仍沿用自动加后缀，避免每次粘贴都打断用户。
export function TransferConflictModal({ conflict, onChoose, onClose }: TransferConflictModalProps) {
  const [submitting, setSubmitting] = useState(false);
  const count = conflict.inspection.directory_conflicts;
  const verb = conflict.mode === 'cut' ? '移动' : '复制';

  const choose = async (policy: ConflictPolicy) => {
    if (submitting) return;
    setSubmitting(true);
    await onChoose(policy);
    setSubmitting(false);
  };

  const footer = (
    <>
      <button
        onClick={onClose}
        className="flex items-center gap-1.5 px-3 py-1.5 text-xs font-medium text-slate-600 dark:text-slate-300 hover:bg-slate-100 dark:hover:bg-slate-800 rounded-lg"
      >
        <X className="w-3.5 h-3.5" />
        取消
      </button>
      <button
        onClick={() => { void choose('rename'); }}
        disabled={submitting}
        className="flex items-center gap-1.5 px-3 py-1.5 text-xs font-medium text-slate-700 dark:text-slate-200 bg-slate-100 dark:bg-slate-800 hover:bg-slate-200 dark:hover:bg-slate-700 rounded-lg"
      >
        <Files className="w-3.5 h-3.5" />
        保留两份并改名
      </button>
      <button
        onClick={() => { void choose('merge_dirs'); }}
        disabled={submitting}
        className="flex items-center gap-1.5 px-3 py-1.5 text-xs font-medium text-white bg-blue-600 hover:bg-blue-700 rounded-lg shadow-sm"
      >
        <FolderSync className="w-3.5 h-3.5" />
        合并文件夹
      </button>
    </>
  );

  return (
    <Modal isOpen={true} onClose={onClose} title="发现同名文件夹" maxWidth="md" footer={footer}>
      <div className="space-y-3 text-sm text-slate-700 dark:text-slate-200">
        <p>
          目标目录中已有 <span className="font-semibold">{count}</span> 个同名文件夹，是否合并？
        </p>
        <div className="rounded-lg bg-blue-50 dark:bg-blue-900/20 border border-blue-100 dark:border-blue-900/40 p-3 text-xs leading-5 text-blue-700 dark:text-blue-300">
          选择“合并文件夹”时，同名文件夹会递归合并；同名文件会保留原文件，并为移入的文件追加数字后缀。整个过程不会覆盖已有内容。
        </div>
        <p className="text-xs text-slate-400">
          当前操作：{verb} {conflict.paths.length} 项到 {conflict.dst}
        </p>
      </div>
    </Modal>
  );
}

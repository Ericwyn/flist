import React, { useState } from 'react';
import { Modal } from './Modal';
import { AlertTriangle, Loader2 } from 'lucide-react';

interface ConfirmModalProps {
  title: string;
  message: React.ReactNode;
  confirmText?: string;
  danger?: boolean;
  // onConfirm 返回错误信息字符串则展示并保持打开，返回 null 表示成功。
  onConfirm: () => Promise<string | null>;
  onClose: () => void;
}

// ConfirmModal 通用二次确认弹窗，用于删除等不可逆操作。
export function ConfirmModal({ title, message, confirmText = '确定', danger = true, onConfirm, onClose }: ConfirmModalProps) {
  const [error, setError] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);

  const confirm = async () => {
    setSubmitting(true);
    setError(null);
    const result = await onConfirm();
    setSubmitting(false);
    if (result) {
      setError(result);
      return;
    }
    onClose();
  };

  const footer = (
    <>
      <button
        onClick={onClose}
        className="px-3 py-1.5 text-xs font-medium text-slate-600 dark:text-slate-300 hover:bg-slate-100 dark:hover:bg-slate-800 rounded-lg transition-colors"
      >
        取消
      </button>
      <button
        onClick={confirm}
        disabled={submitting}
        className={
          'flex items-center gap-1.5 px-3 py-1.5 text-xs font-medium text-white rounded-lg transition-colors shadow-sm disabled:opacity-50 ' +
          (danger ? 'bg-rose-600 hover:bg-rose-700' : 'bg-blue-600 hover:bg-blue-700')
        }
      >
        {submitting && <Loader2 className="w-3.5 h-3.5 animate-spin" />}
        {confirmText}
      </button>
    </>
  );

  return (
    <Modal isOpen={true} onClose={onClose} title={title} maxWidth="md" footer={footer}>
      <div className="flex gap-3">
        {danger && (
          <div className="w-9 h-9 rounded-full bg-rose-50 dark:bg-rose-900/30 flex items-center justify-center shrink-0">
            <AlertTriangle className="w-4.5 h-4.5 text-rose-500" />
          </div>
        )}
        <div className="text-sm text-slate-700 dark:text-slate-200 min-w-0 break-words">{message}</div>
      </div>
      {error && <p className="mt-3 text-xs text-rose-500">{error}</p>}
    </Modal>
  );
}

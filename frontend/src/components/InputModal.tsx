import React, { useEffect, useRef, useState } from 'react';
import { Modal } from './Modal';
import { Loader2 } from 'lucide-react';

interface InputModalProps {
  title: string;
  label: string;
  initialValue?: string;
  confirmText?: string;
  // onSubmit 返回错误信息字符串则展示并保持打开，返回 null 表示成功。
  onSubmit: (value: string) => Promise<string | null>;
  onClose: () => void;
}

// 前端基础非法字符即时提示（与后端公共规则对齐：/ 与 NUL，以及 . / ..）。
function quickValidate(name: string): string | null {
  const trimmed = name.trim();
  if (!trimmed) return '名称不能为空';
  if (trimmed === '.' || trimmed === '..') return '名称非法';
  if (trimmed.includes('/')) return '名称不能包含 /';
  if (trimmed.length > 255) return '名称过长';
  return null;
}

// InputModal 通用名称输入弹窗，用于新建文件夹 / 新建文件 / 重命名。
export function InputModal({ title, label, initialValue = '', confirmText = '确定', onSubmit, onClose }: InputModalProps) {
  const [value, setValue] = useState(initialValue);
  const [error, setError] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);
  const inputRef = useRef<HTMLInputElement>(null);

  useEffect(() => {
    // 自动聚焦并选中文件名主体（重命名时便于直接改名，保留扩展名）。
    const el = inputRef.current;
    if (!el) return;
    el.focus();
    const dot = initialValue.lastIndexOf('.');
    if (dot > 0) {
      el.setSelectionRange(0, dot);
    } else {
      el.select();
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const submit = async () => {
    const v = value.trim();
    const localErr = quickValidate(v);
    if (localErr) {
      setError(localErr);
      return;
    }
    setSubmitting(true);
    setError(null);
    const result = await onSubmit(v);
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
        onClick={submit}
        disabled={submitting}
        className="flex items-center gap-1.5 px-3 py-1.5 text-xs font-medium bg-blue-600 text-white rounded-lg hover:bg-blue-700 transition-colors shadow-sm disabled:opacity-50"
      >
        {submitting && <Loader2 className="w-3.5 h-3.5 animate-spin" />}
        {confirmText}
      </button>
    </>
  );

  return (
    <Modal isOpen={true} onClose={onClose} title={title} maxWidth="md" footer={footer}>
      <label className="block text-xs text-slate-500 dark:text-slate-400 mb-1.5">{label}</label>
      <input
        ref={inputRef}
        value={value}
        onChange={(e) => {
          setValue(e.target.value);
          setError(null);
        }}
        onKeyDown={(e) => {
          if (e.key === 'Enter') submit();
        }}
        className="w-full px-3 py-2 text-sm bg-slate-50 dark:bg-slate-800 border border-slate-200 dark:border-slate-700 rounded-lg outline-none focus:border-blue-400 dark:focus:border-blue-500 text-slate-800 dark:text-slate-100"
      />
      {error && <p className="mt-2 text-xs text-rose-500">{error}</p>}
    </Modal>
  );
}

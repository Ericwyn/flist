import React, { useEffect, useState } from 'react';
import { Modal } from './Modal';
import { api } from '../lib/api';
import { kindOf } from '../lib/path';
import { FileEntry } from '../types';
import { FileIcon } from './FileIcon';
import { Loader2 } from 'lucide-react';

const formatBytes = (bytes: number, decimals = 1) => {
  if (!+bytes) return '0 B';
  const k = 1024;
  const sizes = ['B', 'KB', 'MB', 'GB', 'TB', 'PB'];
  const i = Math.floor(Math.log(bytes) / Math.log(k));
  return `${parseFloat((bytes / Math.pow(k, i)).toFixed(decimals))} ${sizes[i]}`;
};

const formatTime = (iso: string) => {
  const d = new Date(iso);
  if (isNaN(d.getTime())) return '-';
  return d.toLocaleString();
};

const kindLabel: Record<string, string> = {
  folder: '目录',
  text: '文本文件',
  image: '图片',
  video: '视频',
  audio: '音频',
  unknown: '文件',
};

interface PropertiesModalProps {
  path: string; // 完整 API 路径
  fallback: FileEntry; // 列表里已有的条目，作为加载前的占位
  onClose: () => void;
}

// PropertiesModal 展示文件/目录属性，挂载时拉取最新 stat 信息。
export function PropertiesModal({ path, fallback, onClose }: PropertiesModalProps) {
  const [entry, setEntry] = useState<FileEntry>(fallback);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    let alive = true;
    setLoading(true);
    api.fs
      .stat(path)
      .then((info) => {
        if (alive) setEntry(info);
      })
      .catch(() => {
        // 拉取失败时沿用 fallback。
      })
      .finally(() => {
        if (alive) setLoading(false);
      });
    return () => {
      alive = false;
    };
  }, [path]);

  const kind = kindOf(entry);

  const rows: { label: string; value: React.ReactNode }[] = [
    { label: '名称', value: entry.name },
    { label: '类型', value: kindLabel[kind] ?? '文件' },
    { label: '位置', value: path },
    { label: '大小', value: entry.type === 'dir' ? '-' : formatBytes(entry.size) },
    { label: '权限', value: <span className="font-mono">{entry.mode}</span> },
    { label: '修改时间', value: formatTime(entry.modTime) },
  ];
  if (entry.isSymlink) {
    rows.push({
      label: '符号链接',
      value: entry.unreachable ? '目标不可达（越界或缺失）' : entry.symlinkTarget ?? '-',
    });
  }

  return (
    <Modal isOpen={true} onClose={onClose} title="属性" maxWidth="md">
      <div className="flex items-center gap-3 mb-4 pb-4 border-b border-slate-100 dark:border-slate-800">
        <FileIcon kind={kind} className="w-10 h-10 shrink-0" />
        <div className="min-w-0">
          <div className="text-sm font-medium text-slate-900 dark:text-slate-100 truncate">{entry.name}</div>
          <div className="text-xs text-slate-400">{kindLabel[kind] ?? '文件'}</div>
        </div>
        {loading && <Loader2 className="w-4 h-4 animate-spin text-slate-300 ml-auto" />}
      </div>
      <dl className="space-y-2.5">
        {rows.map((r) => (
          <div key={r.label} className="flex text-sm gap-3">
            <dt className="w-20 shrink-0 text-slate-400 dark:text-slate-500">{r.label}</dt>
            <dd className="flex-1 min-w-0 text-slate-700 dark:text-slate-200 break-all">{r.value}</dd>
          </div>
        ))}
      </dl>
    </Modal>
  );
}

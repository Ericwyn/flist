import { FileEntry } from '../types';
import { formatBytes } from './utils';

export interface SelectionStats {
  fileCount: number;
  folderCount: number;
  totalFileSize: number;
}

// summarizeSelection 只汇总当前列表传入的条目；目录大小不参与计算，避免递归扫描。
export function summarizeSelection(entries: readonly FileEntry[]): SelectionStats {
  return entries.reduce<SelectionStats>((summary, entry) => {
    if (entry.type === 'dir') {
      summary.folderCount += 1;
      return summary;
    }

    summary.fileCount += 1;
    if (Number.isFinite(entry.size) && entry.size > 0) {
      summary.totalFileSize += entry.size;
    }
    return summary;
  }, { fileCount: 0, folderCount: 0, totalFileSize: 0 });
}

// formatSelectionSummary 生成底部状态栏的多选摘要。
export function formatSelectionSummary(entries: readonly FileEntry[]): string {
  if (entries.length === 0) return '';

  const { fileCount, folderCount, totalFileSize } = summarizeSelection(entries);
  if (fileCount === 0) return `已选 ${folderCount} 个文件夹`;

  const parts = [
    `已选 ${fileCount} 个文件`,
    `共约 ${formatBytes(totalFileSize)}`,
  ];
  if (folderCount > 0) {
    parts.push(`含 ${folderCount} 个文件夹`);
  }

  return parts.join(' · ');
}

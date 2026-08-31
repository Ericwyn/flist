import { FileEntry, SearchHit } from '../types';
import { joinPath, kindOf } from './path';

export interface MediaPreviewItem {
  entry: FileEntry;
  path: string;
}

export interface MediaNavigation {
  currentIndex: number;
  total: number;
  previous: MediaPreviewItem | null;
  next: MediaPreviewItem | null;
}

// 媒体切换只覆盖浏览器可以直接展示的图片、视频和音频。
export function isSwitchableMedia(entry: Pick<FileEntry, 'name' | 'type'>): boolean {
  const kind = kindOf(entry);
  return kind === 'image' || kind === 'video' || kind === 'audio';
}

// 输入 entries 已由后端按用户选择的 sort/order 排好；这里只过滤，不再排序，
// 从而完整保留当前文件列表中的相对顺序。
export function buildDirectoryMediaItems(entries: FileEntry[], currentPath: string): MediaPreviewItem[] {
  return entries
    .filter((entry) => !entry.unreachable && isSwitchableMedia(entry))
    .map((entry) => ({
      entry,
      path: joinPath(currentPath, entry.name),
    }));
}

// 搜索页没有独立排序控件，因此沿用屏幕上 searchResults 的展示顺序。
export function buildSearchMediaItems(results: SearchHit[]): MediaPreviewItem[] {
  return results
    .filter((hit) => isSwitchableMedia(hit))
    .map((hit) => ({
      entry: {
        name: hit.name,
        type: hit.type,
        size: hit.size,
        mode: hit.mode,
        modTime: hit.modTime,
        isSymlink: false,
      },
      path: hit.path,
    }));
}

export function getMediaNavigation(items: MediaPreviewItem[], currentPath: string): MediaNavigation {
  const currentIndex = items.findIndex((item) => item.path === currentPath);
  return {
    currentIndex,
    total: items.length,
    previous: currentIndex > 0 ? items[currentIndex - 1] : null,
    next: currentIndex >= 0 && currentIndex < items.length - 1 ? items[currentIndex + 1] : null,
  };
}

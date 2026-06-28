import { FileEntry, FileKind } from '../types';

const imageExts = new Set([
  'jpg', 'jpeg', 'png', 'gif', 'webp', 'bmp', 'svg', 'ico', 'avif',
]);
const videoExts = new Set([
  'mp4', 'webm', 'mkv', 'mov', 'avi', 'm4v', 'mpeg', 'mpg', 'flv', 'wmv',
]);
const audioExts = new Set([
  'mp3', 'wav', 'ogg', 'flac', 'aac', 'm4a', 'opus', 'wma',
]);
const textExts = new Set([
  'txt', 'md', 'markdown', 'log', 'csv', 'json', 'yaml', 'yml', 'toml',
  'ini', 'conf', 'cfg', 'xml', 'html', 'htm', 'css', 'scss', 'less',
  'js', 'jsx', 'ts', 'tsx', 'go', 'py', 'rb', 'rs', 'java', 'c', 'h',
  'cpp', 'hpp', 'cc', 'sh', 'bash', 'zsh', 'sql', 'env', 'kt', 'swift',
  'php', 'pl', 'lua', 'r', 'tex', 'rst', 'vue', 'svelte', 'properties',
]);

// kindOf 由条目类型与扩展名推导大类，供图标与预览决策使用。
export function kindOf(entry: Pick<FileEntry, 'name' | 'type'>): FileKind {
  if (entry.type === 'dir') return 'folder';
  const idx = entry.name.lastIndexOf('.');
  const ext = idx >= 0 ? entry.name.slice(idx + 1).toLowerCase() : '';
  if (imageExts.has(ext)) return 'image';
  if (videoExts.has(ext)) return 'video';
  if (audioExts.has(ext)) return 'audio';
  if (ext === 'pdf') return 'pdf';
  if (textExts.has(ext)) return 'text';
  return 'unknown';
}

// joinPath 在当前目录路径下拼接子项名，返回规范化的 API 路径。
export function joinPath(dir: string, name: string): string {
  if (dir === '/' || dir === '') return `/${name}`;
  return `${dir.replace(/\/$/, '')}/${name}`;
}

// parentPath 返回上一级目录路径。
export function parentPath(p: string): string {
  if (p === '/' || p === '') return '/';
  const trimmed = p.replace(/\/$/, '');
  const idx = trimmed.lastIndexOf('/');
  return idx <= 0 ? '/' : trimmed.slice(0, idx);
}

// breadcrumbs 将路径切分为面包屑片段（含根）。
export function breadcrumbs(p: string): { name: string; path: string }[] {
  const crumbs = [{ name: '我的文件', path: '/' }];
  if (p === '/' || p === '') return crumbs;
  const parts = p.replace(/^\//, '').split('/');
  let acc = '';
  for (const part of parts) {
    acc += `/${part}`;
    crumbs.push({ name: part, path: acc });
  }
  return crumbs;
}

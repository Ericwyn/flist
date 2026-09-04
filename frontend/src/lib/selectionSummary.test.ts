import assert from 'node:assert/strict';
import test from 'node:test';
import { FileEntry } from '../types';
import { formatSelectionSummary, summarizeSelection } from './selectionSummary';

function entry(name: string, type: FileEntry['type'], size: number): FileEntry {
  return {
    name,
    type,
    size,
    mode: type === 'dir' ? 'drwxr-xr-x' : '-rw-r--r--',
    modTime: '2026-09-04T00:00:00Z',
    isSymlink: false,
  };
}

test('多选文件时汇总文件大小', () => {
  const entries = [
    entry('a.bin', 'file', 1024),
    entry('b.bin', 'file', 512),
  ];

  assert.deepEqual(summarizeSelection(entries), {
    fileCount: 2,
    folderCount: 0,
    totalFileSize: 1536,
  });
  assert.equal(formatSelectionSummary(entries), '已选 2 个文件 · 共约 1.5 KB');
});

test('混合选择时忽略文件夹大小并单独展示文件夹数量', () => {
  const entries = [
    entry('movie.mp4', 'file', 2 * 1024 * 1024),
    entry('photos', 'dir', 8 * 1024 * 1024),
    entry('backup', 'dir', 16 * 1024 * 1024),
  ];

  assert.deepEqual(summarizeSelection(entries), {
    fileCount: 1,
    folderCount: 2,
    totalFileSize: 2 * 1024 * 1024,
  });
  assert.equal(formatSelectionSummary(entries), '已选 1 个文件 · 共约 2 MB · 含 2 个文件夹');
});

test('只选择文件夹时不展示大小', () => {
  const entries = [
    entry('photos', 'dir', 4096),
    entry('backup', 'dir', 8192),
  ];

  assert.equal(formatSelectionSummary(entries), '已选 2 个文件夹');
});

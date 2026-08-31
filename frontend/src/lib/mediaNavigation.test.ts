import assert from 'node:assert/strict';
import test from 'node:test';
import { FileEntry, SearchHit } from '../types';
import {
  buildDirectoryMediaItems,
  buildSearchMediaItems,
  getMediaNavigation,
} from './mediaNavigation';

function file(name: string, overrides: Partial<FileEntry> = {}): FileEntry {
  return {
    name,
    type: 'file',
    size: 1,
    mode: '-rw-r--r--',
    modTime: '2026-08-31T00:00:00Z',
    isSymlink: false,
    ...overrides,
  };
}

test('目录媒体队列保留当前列表顺序并跳过不可预览项', () => {
  const entries: FileEntry[] = [
    file('folder', { type: 'dir' }),
    file('03-cover.webp'),
    file('notes.txt'),
    file('02-trailer.mp4'),
    file('hidden.mp3', { unreachable: true }),
    file('01-theme.flac'),
  ];

  const items = buildDirectoryMediaItems(entries, '/files/demo');

  assert.deepEqual(items.map((item) => item.entry.name), [
    '03-cover.webp',
    '02-trailer.mp4',
    '01-theme.flac',
  ]);
  assert.deepEqual(items.map((item) => item.path), [
    '/files/demo/03-cover.webp',
    '/files/demo/02-trailer.mp4',
    '/files/demo/01-theme.flac',
  ]);
});

test('搜索媒体队列沿用搜索结果的展示顺序', () => {
  const results: SearchHit[] = [
    { path: '/files/b.mp3', name: 'b.mp3', type: 'file', size: 2, mode: '-rw-r--r--', modTime: '2026-08-31T00:00:00Z' },
    { path: '/files/readme.md', name: 'readme.md', type: 'file', size: 3, mode: '-rw-r--r--', modTime: '2026-08-31T00:00:00Z' },
    { path: '/drive/a.jpg', name: 'a.jpg', type: 'file', size: 4, mode: '-rw-r--r--', modTime: '2026-08-31T00:00:00Z' },
  ];

  const items = buildSearchMediaItems(results);

  assert.deepEqual(items.map((item) => item.path), ['/files/b.mp3', '/drive/a.jpg']);
});

test('上一项和下一项遵循队列边界且不循环', () => {
  const items = buildDirectoryMediaItems(
    [file('c.jpg'), file('b.mp4'), file('a.ogg')],
    '/files/demo',
  );

  const first = getMediaNavigation(items, '/files/demo/c.jpg');
  assert.equal(first.currentIndex, 0);
  assert.equal(first.previous, null);
  assert.equal(first.next?.entry.name, 'b.mp4');

  const middle = getMediaNavigation(items, '/files/demo/b.mp4');
  assert.equal(middle.previous?.entry.name, 'c.jpg');
  assert.equal(middle.next?.entry.name, 'a.ogg');

  const last = getMediaNavigation(items, '/files/demo/a.ogg');
  assert.equal(last.previous?.entry.name, 'b.mp4');
  assert.equal(last.next, null);
});

import React, { useEffect, useRef, useState } from 'react';
import { HardDrive, Home, Settings, StarOff, Pencil, Trash2, Plus, AlertTriangle, ChevronDown, ChevronUp } from 'lucide-react';
import { useFsStore } from '../fsStore';
import { useAuthStore } from '../authStore';
import { useBookmarkStore } from '../bookmarkStore';
import { useStore } from '../store';
import { cn, formatBytes } from '../lib/utils';
import { api } from '../lib/api';
import { SettingsModal } from './SettingsModal';
import { InputModal } from './InputModal';
import { ConfirmModal } from './ConfirmModal';
import { ContextMenu, MenuItem } from './ContextMenu';
import { Bookmark, RecentAccessItem, SpaceInfo } from '../types';
import { FileIcon } from './FileIcon';
import { kindOf } from '../lib/path';

export function Sidebar() {
  const { currentPath, navigate, spaceVersion, openPreview } = useFsStore();
  const { user } = useAuthStore();
  const { items, load, add, rename, remove, reorder } = useBookmarkStore();
  const { recentAccess, clearRecentAccess, recordRecentAccess, recentEnabled } = useStore();
  const [settingsOpen, setSettingsOpen] = useState(false);

  // 路径级容量：随当前目录与写操作（spaceVersion）刷新。
  const [space, setSpace] = useState<SpaceInfo | null>(null);
  // 容量短缓存：记录上次成功查询的时间与对应 spaceVersion，避免快速切目录频繁请求。
  const spaceCacheRef = useRef<{ at: number; version: number }>({ at: 0, version: -1 });

  // 收藏夹相关弹窗 / 菜单状态。
  const [renameTarget, setRenameTarget] = useState<Bookmark | null>(null);
  const [deleteTarget, setDeleteTarget] = useState<Bookmark | null>(null);
  const [menu, setMenu] = useState<{ x: number; y: number; bm: Bookmark } | null>(null);
  const [dragIndex, setDragIndex] = useState<number | null>(null);
  const [overIndex, setOverIndex] = useState<number | null>(null);

  // 收藏夹折叠：超过上限时默认仅展示前 N 项，点击「展开更多」显示全部，避免挤占最近访问。
  const BOOKMARK_PREVIEW_LIMIT = 10;
  const [bookmarksExpanded, setBookmarksExpanded] = useState(false);

  const atRoot = currentPath === '/';

  // 登录后加载收藏列表。
  useEffect(() => {
    load();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // 路径级容量：随当前目录与写操作（spaceVersion）刷新。
  // 加 5 秒短缓存 + 请求合并，避免快速切目录时频繁查询；驱动不支持或失败时静默隐藏。
  useEffect(() => {
    let cancelled = false;
    const now = Date.now();
    if (
      space &&
      space.path === currentPath &&
      now - spaceCacheRef.current.at < 5000 &&
      spaceCacheRef.current.version === spaceVersion
    ) {
      return; // 命中短缓存，跳过查询
    }
    api.fs
      .space(currentPath)
      .then((res) => {
        if (cancelled) return;
        spaceCacheRef.current = { at: Date.now(), version: spaceVersion };
        setSpace(res);
      })
      .catch(() => {
        if (!cancelled) setSpace(null);
      });
    return () => {
      cancelled = true;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [currentPath, spaceVersion]);

  // 当前目录是否已收藏（用于「收藏当前目录」按钮状态）。
  const currentBookmarked = items.some((b) => b.path === currentPath);

  const onClickBookmark = (bm: Bookmark) => {
    if (!bm.valid) return; // 失效项不可跳转
    navigate(bm.path);
  };

  const onAddCurrent = async () => {
    if (atRoot || currentBookmarked) return;
    await add(currentPath);
  };

  // 拖拽排序：drop 时把 dragIndex 处的项移动到 overIndex 位置并持久化。
  const onDrop = () => {
    if (dragIndex === null || overIndex === null || dragIndex === overIndex) {
      setDragIndex(null);
      setOverIndex(null);
      return;
    }
    const next = [...items];
    const [moved] = next.splice(dragIndex, 1);
    next.splice(overIndex, 0, moved);
    reorder(next);
    setDragIndex(null);
    setOverIndex(null);
  };

  const menuItems = (): MenuItem[] => {
    if (!menu) return [];
    return [
      { label: '重命名', icon: <Pencil className="w-4 h-4" />, onClick: () => setRenameTarget(menu.bm) },
      { label: '删除收藏', icon: <Trash2 className="w-4 h-4" />, danger: true, onClick: () => setDeleteTarget(menu.bm) },
    ];
  };

  const onClickRecent = (item: RecentAccessItem) => {
    if (item.type === 'dir') {
      navigate(item.path);
      return;
    }
    recordRecentAccess({ path: item.path, name: item.name, type: item.type });
    openPreview(
      {
        name: item.name,
        type: 'file',
        size: 0,
        mode: '',
        modTime: '',
        isSymlink: false,
      },
      item.path,
    );
  };

  return (
    <div className="w-52 bg-[#f1f5f9] dark:bg-slate-900/50 border-r border-slate-200 dark:border-slate-800 flex flex-col h-full shrink-0 transition-colors duration-200">
      <div className="p-3 flex items-center space-x-2 text-slate-800 dark:text-slate-200 mb-1">
        <HardDrive className="w-5 h-5 text-blue-600 dark:text-blue-400" />
        <span className="text-lg font-bold tracking-tight">Flist</span>
      </div>

      <div className="flex-1 overflow-y-auto p-3 pt-0">
        <section>
          <h3 className="text-[11px] font-medium text-slate-400 dark:text-slate-500 uppercase tracking-wider mb-2">
            导航
          </h3>
          <div className="space-y-0.5">
            <button
              onClick={() => navigate('/')}
              className={cn(
                'w-full flex items-center px-2 py-1.5 rounded-lg text-sm transition-colors',
                atRoot
                  ? 'bg-blue-50 text-blue-700 font-medium dark:bg-blue-900/40 dark:text-blue-300'
                  : 'text-slate-600 dark:text-slate-400 hover:bg-white dark:hover:bg-slate-800',
              )}
            >
              <Home className="w-4 h-4 mr-2.5 opacity-80" />
              <span>我的文件</span>
            </button>
          </div>
        </section>

        {/* 收藏夹 */}
        <section className="mt-5">
          <div className="flex items-center justify-between mb-2">
            <h3 className="text-[11px] font-medium text-slate-400 dark:text-slate-500 uppercase tracking-wider">
              收藏夹
            </h3>
            <button
              onClick={onAddCurrent}
              disabled={atRoot || currentBookmarked}
              title={atRoot ? '根目录不可收藏' : currentBookmarked ? '当前目录已收藏' : '收藏当前目录'}
              className="p-0.5 rounded text-slate-400 hover:text-blue-600 dark:hover:text-blue-400 hover:bg-white dark:hover:bg-slate-800 disabled:opacity-30 disabled:cursor-not-allowed transition-colors"
            >
              <Plus className="w-3.5 h-3.5" />
            </button>
          </div>

          {items.length === 0 ? (
            <p className="text-[11px] text-slate-400 dark:text-slate-600 px-2 py-1">
              暂无收藏
            </p>
          ) : (
            <div className="space-y-0.5">
              {(items.length > BOOKMARK_PREVIEW_LIMIT && !bookmarksExpanded
                ? items.slice(0, BOOKMARK_PREVIEW_LIMIT)
                : items
              ).map((bm, idx) => (
                <div
                  key={bm.id}
                  draggable
                  onDragStart={() => setDragIndex(idx)}
                  onDragOver={(e) => {
                    e.preventDefault();
                    if (overIndex !== idx) setOverIndex(idx);
                  }}
                  onDrop={onDrop}
                  onDragEnd={() => {
                    setDragIndex(null);
                    setOverIndex(null);
                  }}
                  onContextMenu={(e) => {
                    e.preventDefault();
                    setMenu({ x: e.clientX, y: e.clientY, bm });
                  }}
                  className={cn(
                    'group flex items-center px-1.5 py-1.5 rounded-lg text-sm transition-colors',
                    bm.valid
                      ? currentPath === bm.path
                        ? 'bg-blue-50 text-blue-700 font-medium dark:bg-blue-900/40 dark:text-blue-300'
                        : 'text-slate-600 dark:text-slate-400 hover:bg-white dark:hover:bg-slate-800 cursor-pointer'
                      : 'text-slate-300 dark:text-slate-600 cursor-not-allowed',
                    overIndex === idx && dragIndex !== null && dragIndex !== idx && 'ring-1 ring-blue-300 dark:ring-blue-700',
                  )}
                >
                  <button
                    onClick={() => onClickBookmark(bm)}
                    disabled={!bm.valid}
                    className="flex items-center min-w-0 flex-1 text-left"
                    title={bm.valid ? bm.path : `${bm.path}（目标已失效）`}
                  >
                    {!bm.valid && (
                      <AlertTriangle className="w-3.5 h-3.5 mr-2 shrink-0 text-slate-300 dark:text-slate-600" />
                    )}
                    <span className="min-w-0 flex-1">
                      <span className={cn('block truncate leading-4', !bm.valid && 'line-through')}>{bm.name}</span>
                      <span className={cn('block truncate text-[10px] leading-3 font-normal',
                        currentPath === bm.path && bm.valid
                          ? 'text-blue-500 dark:text-blue-300/80'
                          : 'text-slate-400 dark:text-slate-500')}>
                        {bm.path}
                      </span>
                    </span>
                  </button>
                  <button
                    onClick={(e) => {
                      e.stopPropagation();
                      setDeleteTarget(bm);
                    }}
                    title="删除收藏"
                    className="p-0.5 rounded text-slate-300 dark:text-slate-600 hover:text-rose-500 dark:hover:text-rose-400 opacity-0 group-hover:opacity-100 transition-opacity shrink-0"
                  >
                    <StarOff className="w-3.5 h-3.5" />
                  </button>
                </div>
              ))}
              {items.length > BOOKMARK_PREVIEW_LIMIT && (
                <button
                  onClick={() => setBookmarksExpanded((v) => !v)}
                  className="w-full flex items-center justify-center gap-1 mt-1 px-2 py-1 text-[11px] text-slate-400 hover:text-slate-600 dark:hover:text-slate-300 hover:bg-white dark:hover:bg-slate-800 rounded-lg transition-colors"
                >
                  {bookmarksExpanded ? (
                    <>收起 <ChevronUp className="w-3 h-3" /></>
                  ) : (
                    <>展开更多（共 {items.length} 个）<ChevronDown className="w-3 h-3" /></>
                  )}
                </button>
              )}
            </div>
          )}
        </section>

        {/* 最近访问 */}
        {recentEnabled && (
        <section className="mt-5">
          <div className="flex items-center justify-between mb-2">
            <h3 className="text-[11px] font-medium text-slate-400 dark:text-slate-500 uppercase tracking-wider">
              最近访问
            </h3>
            {recentAccess.length > 0 && (
              <button
                onClick={clearRecentAccess}
                title="清空最近访问"
                className="text-[10px] px-1.5 py-0.5 rounded text-slate-400 hover:text-rose-500 hover:bg-white dark:hover:bg-slate-800 transition-colors"
              >
                清空
              </button>
            )}
          </div>

          {recentAccess.length === 0 ? (
            <p className="text-[11px] text-slate-400 dark:text-slate-600 px-2 py-1">
              暂无记录
            </p>
          ) : (
            <div className="space-y-0.5">
              {recentAccess.map((item) => (
                <button
                  key={item.path}
                  onClick={() => onClickRecent(item)}
                  className={cn(
                    'w-full flex items-center gap-2 px-1.5 py-1.5 rounded-lg text-sm text-left transition-colors',
                    item.type === 'dir' && currentPath === item.path
                      ? 'bg-blue-50 text-blue-700 font-medium dark:bg-blue-900/40 dark:text-blue-300'
                      : 'text-slate-600 dark:text-slate-400 hover:bg-white dark:hover:bg-slate-800',
                  )}
                  title={item.path}
                >
                  <FileIcon kind={kindOf({ name: item.name, type: item.type })} className="w-4 h-4 shrink-0" />
                  <span className="min-w-0 flex-1">
                    <span className="block truncate leading-4">{item.name}</span>
                    <span className={cn('block truncate text-[10px] leading-3 font-normal',
                      item.type === 'dir' && currentPath === item.path
                        ? 'text-blue-500 dark:text-blue-300/80'
                        : 'text-slate-400 dark:text-slate-500')}>
                      {item.path}
                    </span>
                  </span>
                </button>
              ))}
            </div>
          )}
        </section>
        )}
      </div>

      {/* 路径级容量：常驻底部状态栏，随当前目录刷新；驱动不支持或不可用时隐藏。 */}
      {space && space.space.supported && (space.space.total ?? 0) > 0 && (() => {
        const s = space.space;
        const total = s.total ?? 0;
        const used = s.used ?? 0;
        const free = s.free ?? 0;
        const pct = total > 0 ? Math.round((used / total) * 100) : 0;
        return (
          <div className="px-3 py-2.5 border-t border-slate-200 dark:border-slate-800">
            <div className="flex items-center text-slate-600 dark:text-slate-400 mb-1.5">
              <HardDrive className="w-3.5 h-3.5 mr-2 shrink-0 opacity-80" />
              <span className="text-[11px] truncate" title={`当前位置所在存储：${space.mount.name || '本地'}`}>
                {formatBytes(total)}
              </span>
              <span className="text-[11px] text-slate-400 dark:text-slate-600 ml-auto pl-2 shrink-0">
                剩余 {formatBytes(free)}
              </span>
            </div>
            <div
              className="h-1.5 rounded-full bg-slate-200 dark:bg-slate-800 overflow-hidden cursor-help"
              title={`已用 ${formatBytes(used)}（${pct}%）· 当前目录所在存储`}
            >
              <div
                className={cn(
                  'h-full rounded-full transition-all duration-300',
                  pct >= 90
                    ? 'bg-rose-500 dark:bg-rose-400'
                    : 'bg-blue-600 dark:bg-blue-400',
                )}
                style={{ width: `${Math.min(100, pct)}%` }}
              />
            </div>
          </div>
        );
      })()}

      <div className="px-3 py-3 border-t border-slate-200 dark:border-slate-800">
        <button
          onClick={() => setSettingsOpen(true)}
          className="w-full flex items-center gap-2 px-2 py-1.5 rounded-lg text-slate-600 dark:text-slate-300 hover:bg-white dark:hover:bg-slate-800 transition-colors group"
          title="设置"
        >
          <div className="w-6 h-6 rounded-full bg-blue-100 dark:bg-blue-900/40 flex items-center justify-center text-[11px] font-semibold text-blue-700 dark:text-blue-300 shrink-0">
            {(user?.username ?? '?').slice(0, 1).toUpperCase()}
          </div>
          <span className="flex-1 min-w-0 text-sm text-left truncate" title={user?.username}>
            {user?.username ?? '未登录'}
          </span>
          <Settings className="w-4 h-4 text-slate-400 group-hover:text-slate-600 dark:group-hover:text-slate-200 shrink-0 transition-colors" />
        </button>
      </div>

      {settingsOpen && <SettingsModal onClose={() => setSettingsOpen(false)} />}

      {menu && (
        <ContextMenu x={menu.x} y={menu.y} items={menuItems()} onClose={() => setMenu(null)} />
      )}

      {renameTarget && (
        <InputModal
          title="重命名收藏"
          label="收藏名称"
          initialValue={renameTarget.name}
          confirmText="保存"
          onSubmit={async (name) => {
            const err = await rename(renameTarget.id, name);
            return err;
          }}
          onClose={() => setRenameTarget(null)}
        />
      )}

      {deleteTarget && (
        <ConfirmModal
          title="删除收藏"
          confirmText="删除"
          message={
            <span>
              确定要删除收藏 <span className="font-medium break-all">{deleteTarget.name}</span> 吗？
              （仅移除收藏，不影响实际目录）
            </span>
          }
          onConfirm={async () => {
            const err = await remove(deleteTarget.id);
            return err;
          }}
          onClose={() => setDeleteTarget(null)}
        />
      )}
    </div>
  );
}

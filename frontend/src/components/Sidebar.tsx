import React, { useEffect, useState } from 'react';
import { HardDrive, Home, Settings, StarOff, Pencil, Trash2, Plus, GripVertical, AlertTriangle } from 'lucide-react';
import { useFsStore } from '../fsStore';
import { useAuthStore } from '../authStore';
import { useBookmarkStore } from '../bookmarkStore';
import { cn, formatBytes } from '../lib/utils';
import { api } from '../lib/api';
import { SettingsModal } from './SettingsModal';
import { InputModal } from './InputModal';
import { ConfirmModal } from './ConfirmModal';
import { ContextMenu, MenuItem } from './ContextMenu';
import { Bookmark, DiskInfo } from '../types';

export function Sidebar() {
  const { currentPath, navigate } = useFsStore();
  const { user } = useAuthStore();
  const { items, load, add, rename, remove, reorder } = useBookmarkStore();
  const [settingsOpen, setSettingsOpen] = useState(false);

  // 磁盘用量（Phase 6），登录后加载。
  const [disk, setDisk] = useState<DiskInfo | null>(null);

  // 收藏夹相关弹窗 / 菜单状态。
  const [renameTarget, setRenameTarget] = useState<Bookmark | null>(null);
  const [deleteTarget, setDeleteTarget] = useState<Bookmark | null>(null);
  const [menu, setMenu] = useState<{ x: number; y: number; bm: Bookmark } | null>(null);
  const [dragIndex, setDragIndex] = useState<number | null>(null);
  const [overIndex, setOverIndex] = useState<number | null>(null);

  const atRoot = currentPath === '/';

  // 登录后加载收藏列表。
  useEffect(() => {
    load();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // 登录后加载磁盘用量（驱动不支持时静默忽略，不展示）。
  useEffect(() => {
    api.system
      .info()
      .then(setDisk)
      .catch(() => setDisk(null));
  }, []);

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
              {items.map((bm, idx) => (
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
                    <span className={cn('truncate', !bm.valid && 'line-through')}>{bm.name}</span>
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
            </div>
          )}
        </section>
      </div>

      {/* 磁盘用量（Phase 6）：常驻底部状态栏，不随收藏夹滚动。 */}
      {disk && disk.total > 0 && (
        <div className="px-3 py-2.5 border-t border-slate-200 dark:border-slate-800">
          <div className="flex items-center text-slate-600 dark:text-slate-400 mb-1.5">
            <HardDrive className="w-3.5 h-3.5 mr-2 shrink-0 opacity-80" />
            <span className="text-[11px] truncate">
              {formatBytes(disk.total)}
            </span>
            <span className="text-[11px] text-slate-400 dark:text-slate-600 ml-auto pl-2 shrink-0">
              剩余 {formatBytes(disk.free)}
            </span>
          </div>
          <div
            className="h-1.5 rounded-full bg-slate-200 dark:bg-slate-800 overflow-hidden cursor-help"
            title={`已用 ${formatBytes(disk.used)}（${Math.round((disk.used / disk.total) * 100)}%）`}
          >
            <div
              className={cn(
                'h-full rounded-full transition-all duration-300',
                disk.used / disk.total >= 0.9
                  ? 'bg-rose-500 dark:bg-rose-400'
                  : 'bg-blue-600 dark:bg-blue-400',
              )}
              style={{ width: `${Math.min(100, Math.round((disk.used / disk.total) * 100))}%` }}
            />
          </div>
        </div>
      )}

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

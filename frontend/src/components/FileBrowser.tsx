import React, { useEffect, useState } from 'react';
import { useFsStore } from '../fsStore';
import { useStore } from '../store';
import { FileIcon } from './FileIcon';
import { ContextMenu, MenuItem } from './ContextMenu';
import { PropertiesModal } from './PropertiesModal';
import { InputModal } from './InputModal';
import { ConfirmModal } from './ConfirmModal';
import { SearchBar } from './SearchBar';
import { cn } from '../lib/utils';
import { kindOf, joinPath, breadcrumbs, parentPath } from '../lib/path';
import { api } from '../lib/api';
import { FileEntry, SearchHit } from '../types';
import {
  ArrowLeft, ArrowRight, ArrowUp, RefreshCw, Download, Info,
  Eye, EyeOff, ArrowDownAZ, ArrowUpAZ, LayoutGrid, List as ListIcon,
  Link2, AlertTriangle, Loader2, FolderOpen, ExternalLink,
  FolderPlus, FilePlus, Pencil, Trash2,
} from 'lucide-react';

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

interface MenuState {
  x: number;
  y: number;
  entry: FileEntry | null; // null 表示空白处右键
}

// DialogState 描述当前打开的写操作弹窗。
type DialogState =
  | { kind: 'mkdir' }
  | { kind: 'touch' }
  | { kind: 'rename'; entry: FileEntry }
  | { kind: 'delete'; entry: FileEntry };

export function FileBrowser() {
  const {
    currentPath, entries, total, loading, error,
    sort, order, showHidden, history, historyIndex, selected,
    navigate, initFromUrl, restore, refresh, goBack, goForward, goUp,
    setSort, toggleOrder, toggleHidden, select, openPreview,
    mkdir, touch, rename, remove,
    searchOpen, searchQuery, searching, searchResults, searchTruncated, searchTimedOut, clearSearch,
  } = useFsStore();
  const { viewMode, setViewMode } = useStore();

  const [menu, setMenu] = useState<MenuState | null>(null);
  const [propsTarget, setPropsTarget] = useState<{ path: string; entry: FileEntry } | null>(null);
  // 弹窗状态：新建目录 / 新建文件 / 重命名 / 删除确认。
  const [dialog, setDialog] = useState<DialogState | null>(null);

  // 挂载时按 URL 恢复目录，并监听浏览器物理前进/后退（popstate）。
  useEffect(() => {
    initFromUrl();
    const onPop = (e: PopStateEvent) => {
      const st = e.state as { index?: number; path?: string } | null;
      if (st && typeof st.path === 'string' && typeof st.index === 'number') {
        restore(st.path, st.index);
      } else {
        // 无 state（极少数情况）：回退到根。
        restore('/', 0);
      }
    };
    window.addEventListener('popstate', onPop);
    return () => window.removeEventListener('popstate', onPop);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const crumbs = breadcrumbs(currentPath);
  const canBack = historyIndex > 0;
  const canForward = historyIndex < history.length - 1;
  const canUp = currentPath !== '/';

  const openEntry = (entry: FileEntry) => {
    const full = joinPath(currentPath, entry.name);
    if (entry.unreachable) return;
    if (entry.type === 'dir') {
      navigate(full);
    } else {
      openPreview(entry, full);
    }
  };

  const doDownload = (entry: FileEntry) => {
    const full = joinPath(currentPath, entry.name);
    const url = api.fs.downloadUrl(full, { download: true });
    const a = document.createElement('a');
    a.href = url;
    a.download = entry.name;
    document.body.appendChild(a);
    a.click();
    a.remove();
  };

  const showProps = (entry: FileEntry) => {
    setPropsTarget({ path: joinPath(currentPath, entry.name), entry });
  };

  // openHit 点击搜索结果：目录直接进入，文件进入其所在目录（并清除搜索）。
  const openHit = (hitPath: string, type: 'file' | 'dir') => {
    clearSearch();
    if (type === 'dir') {
      navigate(hitPath);
    } else {
      navigate(parentPath(hitPath));
    }
  };

  const selectedEntry = entries.find((e) => e.name === selected) || null;

  const onItemContextMenu = (e: React.MouseEvent, entry: FileEntry) => {
    e.preventDefault();
    e.stopPropagation();
    select(entry.name);
    setMenu({ x: e.clientX, y: e.clientY, entry });
  };

  const onBackgroundContextMenu = (e: React.MouseEvent) => {
    e.preventDefault();
    select(null);
    setMenu({ x: e.clientX, y: e.clientY, entry: null });
  };

  const menuItems = (): MenuItem[] => {
    if (!menu) return [];
    const entry = menu.entry;
    if (!entry) {
      // 空白处右键：新建文件夹 / 新建文件 / 刷新。
      return [
        { label: '新建文件夹', icon: <FolderPlus className="w-4 h-4" />, onClick: () => setDialog({ kind: 'mkdir' }) },
        { label: '新建文件', icon: <FilePlus className="w-4 h-4" />, onClick: () => setDialog({ kind: 'touch' }) },
        { label: '刷新', icon: <RefreshCw className="w-4 h-4" />, onClick: refresh },
      ];
    }
    if (entry.unreachable) {
      return [
        { label: '重命名', icon: <Pencil className="w-4 h-4" />, onClick: () => setDialog({ kind: 'rename', entry }) },
        { label: '删除', icon: <Trash2 className="w-4 h-4" />, danger: true, onClick: () => setDialog({ kind: 'delete', entry }) },
        { label: '属性', icon: <Info className="w-4 h-4" />, onClick: () => showProps(entry) },
      ];
    }
    if (entry.type === 'dir') {
      return [
        { label: '打开', icon: <FolderOpen className="w-4 h-4" />, onClick: () => openEntry(entry) },
        { label: '重命名', icon: <Pencil className="w-4 h-4" />, onClick: () => setDialog({ kind: 'rename', entry }) },
        { label: '删除', icon: <Trash2 className="w-4 h-4" />, danger: true, onClick: () => setDialog({ kind: 'delete', entry }) },
        { label: '属性', icon: <Info className="w-4 h-4" />, onClick: () => showProps(entry) },
      ];
    }
    return [
      { label: '打开预览', icon: <ExternalLink className="w-4 h-4" />, onClick: () => openEntry(entry) },
      { label: '下载', icon: <Download className="w-4 h-4" />, onClick: () => doDownload(entry) },
      { label: '重命名', icon: <Pencil className="w-4 h-4" />, onClick: () => setDialog({ kind: 'rename', entry }) },
      { label: '删除', icon: <Trash2 className="w-4 h-4" />, danger: true, onClick: () => setDialog({ kind: 'delete', entry }) },
      { label: '属性', icon: <Info className="w-4 h-4" />, onClick: () => showProps(entry) },
    ];
  };

  return (
    <div className="flex-1 flex flex-col h-full bg-white dark:bg-slate-950 transition-colors duration-200">
      {/* 顶部工具栏 */}
      <div className="border-b border-slate-200 dark:border-slate-800 sticky top-0 z-10 shrink-0">
        <div className="h-14 flex items-center justify-between px-4 gap-3">
          {/* 导航 + 面包屑 */}
          <div className="flex items-center text-sm min-w-0">
            <div className="flex items-center space-x-1 mr-3 text-slate-400 shrink-0">
              <button onClick={goBack} disabled={!canBack}
                className="p-1 rounded hover:bg-slate-100 dark:hover:bg-slate-800 disabled:opacity-30"
                title="后退">
                <ArrowLeft className="w-4 h-4" />
              </button>
              <button onClick={goForward} disabled={!canForward}
                className="p-1 rounded hover:bg-slate-100 dark:hover:bg-slate-800 disabled:opacity-30"
                title="前进">
                <ArrowRight className="w-4 h-4" />
              </button>
              <button onClick={goUp} disabled={!canUp}
                className="p-1 rounded hover:bg-slate-100 dark:hover:bg-slate-800 disabled:opacity-30"
                title="上级目录">
                <ArrowUp className="w-4 h-4" />
              </button>
            </div>
            <div className="flex items-center min-w-0 overflow-x-auto">
              {crumbs.map((c, idx) => (
                <React.Fragment key={c.path}>
                  {idx > 0 && <span className="text-slate-300 dark:text-slate-600 mx-1.5">/</span>}
                  <button
                    onClick={() => navigate(c.path)}
                    className={cn(
                      'hover:text-blue-600 dark:hover:text-blue-400 transition-colors max-w-[160px] truncate shrink-0',
                      idx === crumbs.length - 1
                        ? 'text-slate-900 dark:text-slate-100 font-medium'
                        : 'text-slate-500 dark:text-slate-400',
                    )}
                  >
                    {c.name}
                  </button>
                </React.Fragment>
              ))}
            </div>
          </div>

          {/* 操作区 */}
          <div className="flex items-center space-x-1 shrink-0">
            <SearchBar />
            <div className="w-px h-4 bg-slate-200 dark:bg-slate-700 mx-1" />
            <button onClick={() => setDialog({ kind: 'mkdir' })}
              className="p-1.5 text-slate-500 hover:bg-slate-100 dark:hover:bg-slate-800 rounded-lg"
              title="新建文件夹">
              <FolderPlus className="w-4 h-4" />
            </button>
            <button onClick={() => setDialog({ kind: 'touch' })}
              className="p-1.5 text-slate-500 hover:bg-slate-100 dark:hover:bg-slate-800 rounded-lg"
              title="新建文件">
              <FilePlus className="w-4 h-4" />
            </button>
            <div className="w-px h-4 bg-slate-200 dark:bg-slate-700 mx-1" />
            <button onClick={() => selectedEntry && showProps(selectedEntry)}
              disabled={!selectedEntry}
              className="p-1.5 text-slate-500 hover:bg-slate-100 dark:hover:bg-slate-800 rounded-lg disabled:opacity-30"
              title="属性">
              <Info className="w-4 h-4" />
            </button>
            <button onClick={() => selectedEntry && selectedEntry.type === 'file' && doDownload(selectedEntry)}
              disabled={!selectedEntry || selectedEntry.type !== 'file'}
              className="p-1.5 text-slate-500 hover:bg-slate-100 dark:hover:bg-slate-800 rounded-lg disabled:opacity-30"
              title="下载">
              <Download className="w-4 h-4" />
            </button>
            <div className="w-px h-4 bg-slate-200 dark:bg-slate-700 mx-1" />
            <button onClick={toggleHidden}
              className={cn('p-1.5 rounded-lg hover:bg-slate-100 dark:hover:bg-slate-800',
                showHidden ? 'text-blue-600 dark:text-blue-400' : 'text-slate-500')}
              title={showHidden ? '隐藏隐藏文件' : '显示隐藏文件'}>
              {showHidden ? <Eye className="w-4 h-4" /> : <EyeOff className="w-4 h-4" />}
            </button>
            <select
              value={sort}
              onChange={(e) => setSort(e.target.value as 'name' | 'size' | 'mtime')}
              className="text-xs bg-slate-100 dark:bg-slate-800 border-0 rounded-lg px-2 py-1.5 text-slate-600 dark:text-slate-300 outline-none"
              title="排序方式">
              <option value="name">名称</option>
              <option value="size">大小</option>
              <option value="mtime">修改时间</option>
            </select>
            <button onClick={toggleOrder}
              className="p-1.5 text-slate-500 hover:bg-slate-100 dark:hover:bg-slate-800 rounded-lg"
              title={order === 'asc' ? '升序' : '降序'}>
              {order === 'asc' ? <ArrowDownAZ className="w-4 h-4" /> : <ArrowUpAZ className="w-4 h-4" />}
            </button>
            <button onClick={refresh}
              className="p-1.5 text-slate-500 hover:bg-slate-100 dark:hover:bg-slate-800 rounded-lg"
              title="刷新">
              <RefreshCw className={cn('w-4 h-4', loading && 'animate-spin')} />
            </button>
            <div className="w-px h-4 bg-slate-200 dark:bg-slate-700 mx-1" />
            <button onClick={() => setViewMode(viewMode === 'grid' ? 'list' : 'grid')}
              className="p-1.5 text-slate-500 hover:bg-slate-100 dark:hover:bg-slate-800 rounded-lg"
              title="切换视图">
              {viewMode === 'grid' ? <ListIcon className="w-4 h-4" /> : <LayoutGrid className="w-4 h-4" />}
            </button>
          </div>
        </div>
      </div>

      {/* 内容区 */}
      <div className="flex-1 overflow-auto p-4"
        onClick={() => select(null)}
        onContextMenu={onBackgroundContextMenu}>
        {searchOpen ? (
          <SearchResultsView
            query={searchQuery}
            results={searchResults}
            searching={searching}
            truncated={searchTruncated}
            timedOut={searchTimedOut}
            onOpen={openHit}
          />
        ) : error ? (
          <div className="flex flex-col items-center justify-center h-full text-slate-400">
            <AlertTriangle className="w-10 h-10 mb-3 text-amber-500" />
            <p className="text-sm">{error}</p>
            <button onClick={refresh} className="mt-3 text-xs text-blue-600 hover:underline">重试</button>
          </div>
        ) : loading && entries.length === 0 ? (
          <div className="flex items-center justify-center h-full text-slate-400">
            <Loader2 className="w-6 h-6 animate-spin" />
          </div>
        ) : entries.length === 0 ? (
          <div className="flex items-center justify-center h-full text-slate-400 text-sm">此目录为空</div>
        ) : viewMode === 'grid' ? (
          <GridView entries={entries} selected={selected} onSelect={select} onOpen={openEntry} onContextMenu={onItemContextMenu} />
        ) : (
          <ListView entries={entries} selected={selected} onSelect={select} onOpen={openEntry} onContextMenu={onItemContextMenu} />
        )}
      </div>

      {/* 状态栏 */}
      <div className="border-t border-slate-200 dark:border-slate-800 px-4 py-1.5 text-[11px] text-slate-400 flex justify-between shrink-0">
        <span>{total} 项{showHidden ? '（含隐藏）' : ''}</span>
        <span>{selectedEntry ? selectedEntry.name : currentPath}</span>
      </div>

      {menu && (
        <ContextMenu x={menu.x} y={menu.y} items={menuItems()} onClose={() => setMenu(null)} />
      )}
      {propsTarget && (
        <PropertiesModal
          path={propsTarget.path}
          fallback={propsTarget.entry}
          onClose={() => setPropsTarget(null)}
        />
      )}

      {dialog?.kind === 'mkdir' && (
        <InputModal
          title="新建文件夹"
          label="文件夹名称"
          confirmText="创建"
          onSubmit={(name) => mkdir(name)}
          onClose={() => setDialog(null)}
        />
      )}
      {dialog?.kind === 'touch' && (
        <InputModal
          title="新建文件"
          label="文件名称"
          confirmText="创建"
          onSubmit={(name) => touch(name)}
          onClose={() => setDialog(null)}
        />
      )}
      {dialog?.kind === 'rename' && (
        <InputModal
          title="重命名"
          label="新名称"
          initialValue={dialog.entry.name}
          confirmText="重命名"
          onSubmit={(name) => rename(dialog.entry, name)}
          onClose={() => setDialog(null)}
        />
      )}
      {dialog?.kind === 'delete' && (
        <ConfirmModal
          title="删除确认"
          confirmText="删除"
          message={
            <span>
              确定要删除 <span className="font-medium break-all">{dialog.entry.name}</span>
              {dialog.entry.type === 'dir' ? '（及其全部内容）' : ''} 吗？此操作不可恢复。
            </span>
          }
          onConfirm={() => remove([dialog.entry])}
          onClose={() => setDialog(null)}
        />
      )}
    </div>
  );
}

interface ViewProps {
  entries: FileEntry[];
  selected: string | null;
  onSelect: (name: string) => void;
  onOpen: (entry: FileEntry) => void;
  onContextMenu: (e: React.MouseEvent, entry: FileEntry) => void;
}

function GridView({ entries, selected, onSelect, onOpen, onContextMenu }: ViewProps) {
  return (
    <div className="grid grid-cols-[repeat(auto-fill,minmax(100px,1fr))] gap-2">
      {entries.map((entry) => (
        <button
          key={entry.name}
          onClick={(e) => { e.stopPropagation(); onSelect(entry.name); }}
          onDoubleClick={() => onOpen(entry)}
          onContextMenu={(e) => onContextMenu(e, entry)}
          className={cn(
            'flex flex-col items-center p-3 rounded-xl transition-colors group relative',
            selected === entry.name
              ? 'bg-blue-50 dark:bg-blue-900/30'
              : 'hover:bg-slate-50 dark:hover:bg-slate-900',
          )}
        >
          <div className="relative">
            <FileIcon kind={kindOf(entry)} className="w-10 h-10 mb-1.5" />
            {entry.isSymlink && (
              <Link2 className="w-3 h-3 absolute -bottom-0.5 -right-1 text-slate-400 bg-white dark:bg-slate-950 rounded-full" />
            )}
          </div>
          <span className={cn('text-xs text-center break-all line-clamp-2',
            entry.unreachable ? 'text-slate-300 dark:text-slate-600 line-through' : 'text-slate-700 dark:text-slate-300')}>
            {entry.name}
          </span>
        </button>
      ))}
    </div>
  );
}

function ListView({ entries, selected, onSelect, onOpen, onContextMenu }: ViewProps) {
  return (
    <table className="w-full text-sm">
      <thead>
        <tr className="text-[11px] text-slate-400 border-b border-slate-100 dark:border-slate-800">
          <th className="text-left font-medium py-2 px-2">名称</th>
          <th className="text-right font-medium py-2 px-2 w-24">大小</th>
          <th className="text-left font-medium py-2 px-2 w-44 hidden sm:table-cell">修改时间</th>
          <th className="text-left font-medium py-2 px-2 w-20 hidden md:table-cell">权限</th>
        </tr>
      </thead>
      <tbody>
        {entries.map((entry) => (
          <tr
            key={entry.name}
            onClick={(e) => { e.stopPropagation(); onSelect(entry.name); }}
            onDoubleClick={() => onOpen(entry)}
            onContextMenu={(e) => onContextMenu(e, entry)}
            className={cn(
              'cursor-default border-b border-slate-50 dark:border-slate-900/50',
              selected === entry.name
                ? 'bg-blue-50 dark:bg-blue-900/30'
                : 'hover:bg-slate-50 dark:hover:bg-slate-900',
            )}
          >
            <td className="py-1.5 px-2">
              <div className="flex items-center gap-2 min-w-0">
                <FileIcon kind={kindOf(entry)} className="w-4 h-4 shrink-0" />
                <span className={cn('truncate',
                  entry.unreachable ? 'text-slate-300 dark:text-slate-600 line-through' : 'text-slate-700 dark:text-slate-300')}>
                  {entry.name}
                </span>
                {entry.isSymlink && <Link2 className="w-3 h-3 text-slate-400 shrink-0" />}
              </div>
            </td>
            <td className="py-1.5 px-2 text-right text-slate-400 tabular-nums">
              {entry.type === 'dir' ? '-' : formatBytes(entry.size)}
            </td>
            <td className="py-1.5 px-2 text-slate-400 hidden sm:table-cell">{formatTime(entry.modTime)}</td>
            <td className="py-1.5 px-2 text-slate-400 font-mono text-xs hidden md:table-cell">{entry.mode}</td>
          </tr>
        ))}
      </tbody>
    </table>
  );
}

interface SearchResultsViewProps {
  query: string;
  results: SearchHit[];
  searching: boolean;
  truncated: boolean;
  timedOut: boolean;
  onOpen: (path: string, type: 'file' | 'dir') => void;
}

// SearchResultsView 以列表形式展示搜索命中，点击进入目标所在目录。
function SearchResultsView({ query, results, searching, truncated, timedOut, onOpen }: SearchResultsViewProps) {
  if (searching) {
    return (
      <div className="flex items-center justify-center h-full text-slate-400">
        <Loader2 className="w-6 h-6 animate-spin" />
      </div>
    );
  }
  if (results.length === 0) {
    return (
      <div className="flex flex-col items-center justify-center h-full text-slate-400 text-sm">
        <FolderOpen className="w-10 h-10 mb-3 opacity-40" />
        <p>未找到与「{query}」匹配的文件</p>
      </div>
    );
  }
  return (
    <div className="flex flex-col">
      <div className="text-xs text-slate-400 mb-2 px-1">
        找到 {results.length} 个匹配「{query}」的结果
        {truncated && '（已达上限，结果可能不完整）'}
        {timedOut && '（搜索超时，结果可能不完整）'}
      </div>
      <div className="divide-y divide-slate-50 dark:divide-slate-900/50">
        {results.map((hit) => (
          <button
            key={hit.path}
            onClick={() => onOpen(hit.path, hit.type)}
            className="w-full flex items-center gap-2.5 py-2 px-2 text-left rounded-lg hover:bg-slate-50 dark:hover:bg-slate-900 transition-colors"
          >
            <FileIcon kind={kindOf({ name: hit.name, type: hit.type })} className="w-5 h-5 shrink-0" />
            <div className="flex-1 min-w-0">
              <div className="text-sm text-slate-700 dark:text-slate-200 truncate">{hit.name}</div>
              <div className="text-[11px] text-slate-400 truncate">{hit.path}</div>
            </div>
            <span className="text-[11px] text-slate-400 shrink-0 tabular-nums">
              {hit.type === 'dir' ? '目录' : formatBytes(hit.size)}
            </span>
          </button>
        ))}
      </div>
    </div>
  );
}

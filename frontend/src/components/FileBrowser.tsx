import React, { useEffect, useState, useRef } from 'react';
import { useFsStore } from '../fsStore';
import { useStore } from '../store';
import { useUploadStore } from '../uploadStore';
import { useDownloadStore } from '../downloadStore';
import { FileIcon } from './FileIcon';
import { ContextMenu, MenuItem } from './ContextMenu';
import { PropertiesModal } from './PropertiesModal';
import { InputModal } from './InputModal';
import { ConfirmModal } from './ConfirmModal';
import { SearchBar } from './SearchBar';
import { cn } from '../lib/utils';
import { kindOf, joinPath, breadcrumbs, parentPath } from '../lib/path';
import { closeTopModal, subscribeModal, openModalCount } from '../lib/modalRegistry';
import { api } from '../lib/api';
import { FileEntry, SearchHit } from '../types';
import {
  ArrowLeft, ArrowRight, ArrowUp, RefreshCw, Download, Info,
  Eye, EyeOff, ArrowDownAZ, ArrowUpAZ, LayoutGrid, List as ListIcon,
  Link2, AlertTriangle, Loader2, FolderOpen, ExternalLink,
  FolderPlus, FilePlus, Pencil, Trash2, Copy, Scissors, ClipboardPaste, Star, Upload,
} from 'lucide-react';
import { useBookmarkStore } from '../bookmarkStore';

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
  | { kind: 'delete'; entries: FileEntry[] }
  | { kind: 'delete-paths'; paths: string[] }; // 搜索结果按完整路径批量删除

export function FileBrowser() {
  const {
    currentPath, entries, total, loading, error,
    sort, order, showHidden, history, historyIndex, selected,
    navigate, initFromUrl, restore, refresh, goBack, goForward, goUp,
    setSort, toggleOrder, toggleHidden,
    selectOne, toggleSelect, rangeSelect, selectAll, clearSelection, openPreview,
    mkdir, touch, rename, remove, removePaths,
    searchOpen, searchQuery, searching, searchResults, searchTruncated, searchTimedOut, clearSearch, exitSearch,
    searchSelected, selectOneHit, toggleHit, rangeHit, selectAllHits, clearHitSelection,
    clipboard, copyToClipboard, cutToClipboard, copyPathsToClipboard, cutPathsToClipboard, paste, clearClipboard,
  } = useFsStore();
  const { viewMode, setViewMode } = useStore();
  const addBookmark = useBookmarkStore((s) => s.add);
  const enqueueUpload = useUploadStore((s) => s.enqueue);
  const startDownload = useDownloadStore((s) => s.start);

  const [menu, setMenu] = useState<MenuState | null>(null);
  const [propsTarget, setPropsTarget] = useState<{ path: string; entry: FileEntry } | null>(null);
  // 弹窗状态：新建目录 / 新建文件 / 重命名 / 删除确认。
  const [dialog, setDialog] = useState<DialogState | null>(null);
  // 短暂操作提示（粘贴失败 / 收藏结果等）。
  const [toast, setToast] = useState<string | null>(null);
  // 拖拽上传的悬停高亮态（仅非搜索态生效）。
  const [dragOver, setDragOver] = useState(false);
  // 隐藏的文件选择 input，用于工具栏「上传」按钮触发。
  const fileInputRef = useRef<HTMLInputElement>(null);

  // doUpload 把一批文件加入上传队列（目标为当前目录）。
  const doUpload = (files: FileList | File[] | null) => {
    if (!files) return;
    const arr = Array.from(files);
    if (arr.length === 0) return;
    void enqueueUpload(arr, currentPath);
  };

  // notify 显示一条短暂提示，3 秒后自动消失。
  const notify = (msg: string) => {
    setToast(msg);
    window.setTimeout(() => setToast((cur) => (cur === msg ? null : cur)), 3000);
  };

  // 挂载时按 URL 恢复目录，并协调浏览器物理前进/后退（popstate）与弹窗。
  //
  // 守卫历史方案：弹窗打开时在当前 URL 之上压入一枚「同址守卫」历史条目，维持不变式
  // ——「只要有弹窗打开，当前位置之上/即为一枚与当前 URL 相同的守卫」。物理返回先消费守卫
  // （前后 URL 相同，地址栏不跳动、不切目录），并关闭顶层弹窗；经 X/遮罩/Esc/按钮关闭时，
  // 主动回退一格把守卫清掉。这样彻底避免「先跳到目标目录、再被巻回」的地址栏闪跳。
  useEffect(() => {
    initFromUrl();

    let guardActive = false; // 当前是否已压入守卫
    let cleaningGuard = false; // 主动回退清理守卫时，吞掉随之而来的 popstate

    const pushGuard = () => {
      // 用与当前相同的 state 与 URL 压栈：即便日后 forward 命中它，也只触发同址 no-op。
      window.history.pushState(window.history.state, '', window.location.href);
      guardActive = true;
    };

    // 弹窗栈变化：保证「有弹窗时恒有一枚守卫」。
    const unsub = subscribeModal(() => {
      const count = openModalCount();
      if (count > 0 && !guardActive) {
        pushGuard();
      } else if (count === 0 && guardActive) {
        // 弹窗经非物理返回方式关闭（X / 遮罩 / Esc / 按钮）：仍停在守卫上，回退一格消费掉。
        guardActive = false;
        cleaningGuard = true;
        window.history.back();
      }
    });

    const onPop = (e: PopStateEvent) => {
      if (cleaningGuard) {
        // 这是上面主动 back() 清理守卫触发的 popstate，忽略。
        cleaningGuard = false;
        return;
      }
      if (guardActive) {
        // 物理前进/后退消费了守卫：关闭顶层弹窗，不切换底层目录。
        // 若仍有堆叠弹窗，subscribeModal 回调会立即补压一枚新守卫，维持不变式。
        guardActive = false;
        closeTopModal();
        return;
      }
      const st = e.state as { index?: number; path?: string } | null;
      if (st && typeof st.path === 'string' && typeof st.index === 'number') {
        restore(st.path, st.index);
      } else {
        // 无 state（极少数情况）：回退到根。
        restore('/', 0);
      }
    };

    window.addEventListener('popstate', onPop);
    return () => {
      window.removeEventListener('popstate', onPop);
      unsub();
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // 选中条目集合（派生）与单选便捷量（恰好选中一项时非空）。
  const selectedEntries = entries.filter((e) => selected.has(e.name));
  const singleSelected = selectedEntries.length === 1 ? selectedEntries[0] : null;

  // 剪贴板快捷键：Ctrl/Cmd+C 复制、Ctrl/Cmd+X 剪切选中项，Ctrl/Cmd+V 粘贴；Ctrl/Cmd+A 全选。
  // 在搜索视图或输入框聚焦时不拦截，避免干扰文本编辑与浏览器原生复制。
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (!(e.ctrlKey || e.metaKey) || e.altKey || e.shiftKey) return;
      if (searchOpen) return;
      const tag = (e.target as HTMLElement | null)?.tagName;
      if (tag === 'INPUT' || tag === 'TEXTAREA') return;
      const sels = entries.filter((it) => selected.has(it.name) && !it.unreachable);
      const key = e.key.toLowerCase();
      if (key === 'a') {
        e.preventDefault();
        selectAll();
      } else if (key === 'c' && sels.length > 0) {
        e.preventDefault();
        copyToClipboard(sels);
      } else if (key === 'x' && sels.length > 0) {
        e.preventDefault();
        cutToClipboard(sels);
      } else if (key === 'v' && clipboard && clipboard.paths.length > 0) {
        e.preventDefault();
        void doPaste();
      }
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [entries, selected, clipboard, searchOpen]);

  // 无修饰键的快捷键：F2 重命名（单选）、Delete 批量删除、Escape 清空选择。
  // 搜索态、输入框聚焦或已有弹窗时不触发（Escape 在有弹窗时交给弹窗处理）。
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.ctrlKey || e.metaKey || e.altKey) return;
      if (searchOpen) return;
      const tag = (e.target as HTMLElement | null)?.tagName;
      if (tag === 'INPUT' || tag === 'TEXTAREA') return;
      const sels = entries.filter((it) => selected.has(it.name));
      if (e.key === 'F2' && !dialog) {
        if (sels.length !== 1) return;
        e.preventDefault();
        setDialog({ kind: 'rename', entry: sels[0] });
      } else if ((e.key === 'Delete' || e.key === 'Backspace') && !dialog) {
        if (sels.length === 0) return;
        e.preventDefault();
        setDialog({ kind: 'delete', entries: sels });
      } else if (e.key === 'Escape' && !dialog && !menu) {
        if (selected.size > 0) {
          e.preventDefault();
          clearSelection();
        }
      }
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [entries, selected, searchOpen, dialog, menu]);

  // 搜索态快捷键：Ctrl/Cmd+A 全选命中、Ctrl/Cmd+C/X 复制/剪切选中路径、Delete 批量删除、Esc 清空选择。
  // 输入框聚焦或已有弹窗时不拦截。
  useEffect(() => {
    if (!searchOpen) return;
    const onKey = (e: KeyboardEvent) => {
      const tag = (e.target as HTMLElement | null)?.tagName;
      if (tag === 'INPUT' || tag === 'TEXTAREA') return;
      const paths = searchResults.filter((h) => searchSelected.has(h.path)).map((h) => h.path);
      if (e.ctrlKey || e.metaKey) {
        if (e.altKey || e.shiftKey) return;
        const key = e.key.toLowerCase();
        if (key === 'a') {
          e.preventDefault();
          selectAllHits();
        } else if (key === 'c' && paths.length > 0) {
          e.preventDefault();
          copyPathsToClipboard(paths);
          notify(`已复制 ${paths.length} 项到剪贴板`);
        } else if (key === 'x' && paths.length > 0) {
          e.preventDefault();
          cutPathsToClipboard(paths);
          notify(`已剪切 ${paths.length} 项到剪贴板`);
        }
        return;
      }
      if ((e.key === 'Delete' || e.key === 'Backspace') && !dialog) {
        if (paths.length === 0) return;
        e.preventDefault();
        setDialog({ kind: 'delete-paths', paths });
      } else if (e.key === 'Escape' && !dialog && !menu) {
        if (searchSelected.size > 0) {
          e.preventDefault();
          clearHitSelection();
        }
      }
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [searchOpen, searchResults, searchSelected, dialog, menu]);

  // handleItemClick 统一处理点选：Shift 范围、Ctrl/Cmd 切换、普通单选。
  const handleItemClick = (entry: FileEntry, e: React.MouseEvent) => {
    if (e.shiftKey) {
      rangeSelect(entry.name);
    } else if (e.ctrlKey || e.metaKey) {
      toggleSelect(entry.name);
    } else {
      selectOne(entry.name);
    }
  };

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

  // editorUrl 构造编辑器页面 URL（/editor?path=...，token 仍在 localStorage，同源复用，不入 URL）。
  const editorUrl = (entry: FileEntry) =>
    `/editor?path=${encodeURIComponent(joinPath(currentPath, entry.name))}`;

  // openEditor 在当前窗口打开编辑器（整页切换，App 按 pathname 渲染 Editor）。
  const openEditor = (entry: FileEntry) => {
    window.location.href = editorUrl(entry);
  };

  // openEditorNewWindow 在新窗口打开编辑器；noopener 防止新页面反向操作本窗口。
  const openEditorNewWindow = (entry: FileEntry) => {
    window.open(editorUrl(entry), '_blank', 'noopener');
  };

  const showProps = (entry: FileEntry) => {
    setPropsTarget({ path: joinPath(currentPath, entry.name), entry });
  };

  // openHit 双击搜索结果：目录进入并退出搜索；文件直接预览，停留在搜索结果页
  //（搜索范围本就是当前目录，跳转到所在目录意义不大）。
  const openHit = (hit: SearchHit) => {
    if (hit.type === 'dir') {
      clearSearch();
      navigate(hit.path);
      return;
    }
    openPreview(
      {
        name: hit.name,
        type: 'file',
        size: hit.size,
        mode: hit.mode,
        modTime: hit.modTime,
        isSymlink: false,
      },
      hit.path,
    );
  };

  // handleHitClick 统一处理搜索结果点选：Shift 范围、Ctrl/Cmd 切换、普通单选。
  const handleHitClick = (hit: SearchHit, e: React.MouseEvent) => {
    if (e.shiftKey) {
      rangeHit(hit.path);
    } else if (e.ctrlKey || e.metaKey) {
      toggleHit(hit.path);
    } else {
      selectOneHit(hit.path);
    }
  };

  // selectedHitPaths 为当前搜索选中项的完整路径数组（按结果顺序）。
  const selectedHitPaths = searchResults.filter((h) => searchSelected.has(h.path)).map((h) => h.path);

  // cutNames 为当前目录下被「剪切」的条目名集合，用于视图淡显标记。
  const cutNames = new Set<string>(
    clipboard?.mode === 'cut'
      ? clipboard.paths
          .filter((p) => parentPath(p) === currentPath)
          .map((p) => p.slice(p.lastIndexOf('/') + 1))
      : [],
  );

  // onItemContextMenu 在条目上右键：若该项已在多选集合内则保留选择（批量菜单），
  // 否则单选该项再弹菜单。
  const onItemContextMenu = (e: React.MouseEvent, entry: FileEntry) => {
    e.preventDefault();
    e.stopPropagation();
    if (!selected.has(entry.name)) {
      selectOne(entry.name);
    }
    setMenu({ x: e.clientX, y: e.clientY, entry });
  };

  const onBackgroundContextMenu = (e: React.MouseEvent) => {
    e.preventDefault();
    clearSelection();
    setMenu({ x: e.clientX, y: e.clientY, entry: null });
  };

  // doArchive 把给定条目打包下载为 zip（目录与多文件均可）。交给 downloadStore，进度在传输面板展示。
  const doArchive = (items: FileEntry[]) => {
    const paths = items.filter((it) => !it.unreachable).map((it) => joinPath(currentPath, it.name));
    if (paths.length === 0) return;
    const name = items.length === 1 ? items[0].name : undefined;
    startDownload(paths, name);
  };

  // doArchivePaths 按完整路径打包下载（供搜索结果使用）。
  const doArchivePaths = (paths: string[], name?: string) => {
    if (paths.length === 0) return;
    startDownload(paths, name);
  };

  // doPaste 粘贴剪贴板内容到当前目录，失败时提示。
  const doPaste = async () => {
    const err = await paste();
    if (err) notify(err);
  };

  // doBookmarkCurrent 收藏当前目录（根不可收藏）。
  const doBookmarkCurrent = async () => {
    if (currentPath === '/') return;
    const err = await addBookmark(currentPath);
    notify(err ?? '已收藏当前目录');
  };

  // pasteItem 当剪贴板非空时返回「粘贴」菜单项，否则返回 null。
  const pasteItem = (): MenuItem | null => {
    if (!clipboard || clipboard.paths.length === 0) return null;
    const verb = clipboard.mode === 'cut' ? '移动' : '粘贴';
    return {
      label: `${verb} ${clipboard.paths.length} 项到此处`,
      icon: <ClipboardPaste className="w-4 h-4" />,
      onClick: doPaste,
    };
  };

  const menuItems = (): MenuItem[] => {
    if (!menu) return [];
    const entry = menu.entry;
    if (!entry) {
      // 空白处右键：新建 / 粘贴 / 收藏当前目录 / 刷新。
      const items: MenuItem[] = [
        { label: '新建文件夹', icon: <FolderPlus className="w-4 h-4" />, onClick: () => setDialog({ kind: 'mkdir' }) },
        { label: '新建文件', icon: <FilePlus className="w-4 h-4" />, onClick: () => setDialog({ kind: 'touch' }) },
      ];
      const p = pasteItem();
      if (p) items.push(p);
      items.push({
        label: '收藏当前目录',
        icon: <Star className="w-4 h-4" />,
        disabled: currentPath === '/',
        onClick: doBookmarkCurrent,
      });
      items.push({ label: '刷新', icon: <RefreshCw className="w-4 h-4" />, onClick: refresh });
      return items;
    }

    // 多选（>1）：弹批量菜单，作用于整个选择集。
    if (selected.size > 1) {
      const sels = selectedEntries;
      const reachable = sels.filter((it) => !it.unreachable);
      return [
        {
          label: `打包下载 ${reachable.length} 项`,
          icon: <Download className="w-4 h-4" />,
          disabled: reachable.length === 0,
          onClick: () => void doArchive(reachable),
        },
        {
          label: `复制 ${reachable.length} 项`,
          icon: <Copy className="w-4 h-4" />,
          disabled: reachable.length === 0,
          onClick: () => copyToClipboard(reachable),
        },
        {
          label: `剪切 ${reachable.length} 项`,
          icon: <Scissors className="w-4 h-4" />,
          disabled: reachable.length === 0,
          onClick: () => cutToClipboard(reachable),
        },
        {
          label: `删除 ${sels.length} 项`,
          icon: <Trash2 className="w-4 h-4" />,
          danger: true,
          onClick: () => setDialog({ kind: 'delete', entries: sels }),
        },
      ];
    }

    // 复制 / 剪切对所有可达条目通用。
    const clip: MenuItem[] = [
      { label: '复制', icon: <Copy className="w-4 h-4" />, onClick: () => copyToClipboard([entry]) },
      { label: '剪切', icon: <Scissors className="w-4 h-4" />, onClick: () => cutToClipboard([entry]) },
    ];
    if (entry.unreachable) {
      return [
        { label: '重命名', icon: <Pencil className="w-4 h-4" />, onClick: () => setDialog({ kind: 'rename', entry }) },
        { label: '删除', icon: <Trash2 className="w-4 h-4" />, danger: true, onClick: () => setDialog({ kind: 'delete', entries: [entry] }) },
        { label: '属性', icon: <Info className="w-4 h-4" />, onClick: () => showProps(entry) },
      ];
    }
    if (entry.type === 'dir') {
      return [
        { label: '打开', icon: <FolderOpen className="w-4 h-4" />, onClick: () => openEntry(entry) },
        { label: '打包下载', icon: <Download className="w-4 h-4" />, onClick: () => void doArchive([entry]) },
        ...clip,
        { label: '收藏', icon: <Star className="w-4 h-4" />, onClick: () => doBookmarkEntry(entry) },
        { label: '重命名', icon: <Pencil className="w-4 h-4" />, onClick: () => setDialog({ kind: 'rename', entry }) },
        { label: '删除', icon: <Trash2 className="w-4 h-4" />, danger: true, onClick: () => setDialog({ kind: 'delete', entries: [entry] }) },
        { label: '属性', icon: <Info className="w-4 h-4" />, onClick: () => showProps(entry) },
      ];
    }
    const fileItems: MenuItem[] = [
      { label: '打开预览', icon: <ExternalLink className="w-4 h-4" />, onClick: () => openEntry(entry) },
    ];
    // 文本 / 未知类型提供编辑入口（媒体二进制不展示）。
    const fileKind = kindOf(entry);
    if (fileKind === 'text' || fileKind === 'unknown') {
      fileItems.push(
        { label: '编辑', icon: <Pencil className="w-4 h-4" />, onClick: () => openEditor(entry) },
        { label: '新窗口编辑', icon: <ExternalLink className="w-4 h-4" />, onClick: () => openEditorNewWindow(entry) },
      );
    }
    fileItems.push(
      { label: '下载', icon: <Download className="w-4 h-4" />, onClick: () => doDownload(entry) },
      ...clip,
      { label: '重命名', icon: <Pencil className="w-4 h-4" />, onClick: () => setDialog({ kind: 'rename', entry }) },
      { label: '删除', icon: <Trash2 className="w-4 h-4" />, danger: true, onClick: () => setDialog({ kind: 'delete', entries: [entry] }) },
      { label: '属性', icon: <Info className="w-4 h-4" />, onClick: () => showProps(entry) },
    );
    return fileItems;
  };

  // doBookmarkEntry 收藏选中的子目录。
  const doBookmarkEntry = async (entry: FileEntry) => {
    const err = await addBookmark(joinPath(currentPath, entry.name), entry.name);
    notify(err ?? `已收藏「${entry.name}」`);
  };

  return (
    <div className="flex-1 flex flex-col h-full bg-white dark:bg-slate-950 transition-colors duration-200 relative">
      {/* 顶部工具栏 */}
      <div className="border-b border-slate-200 dark:border-slate-800 sticky top-0 z-10 shrink-0">
        <div className="h-14 flex items-center justify-between px-4 gap-3">
          {/* 导航 + 面包屑（搜索态切换为「退出搜索」按钮 + 结果标题） */}
          <div className="flex items-center text-sm min-w-0">
            {searchOpen ? (
              <>
                <button
                  onClick={exitSearch}
                  className="flex items-center gap-1.5 mr-3 px-2.5 py-1.5 rounded-lg text-slate-600 dark:text-slate-300 hover:bg-slate-100 dark:hover:bg-slate-800 transition-colors shrink-0"
                  title="退出搜索，返回目录浏览"
                >
                  <ArrowLeft className="w-[18px] h-[18px]" />
                  <span>退出搜索</span>
                </button>
                <span className="text-slate-900 dark:text-slate-100 font-medium truncate min-w-0">
                  「{crumbs[crumbs.length - 1].name}」目录下的搜索结果
                </span>
              </>
            ) : (
              <>
                <div className="flex items-center space-x-1 mr-3 text-slate-400 shrink-0">
                  <button onClick={goBack} disabled={!canBack}
                    className="p-1.5 rounded-lg hover:bg-slate-100 dark:hover:bg-slate-800 disabled:opacity-30"
                    title="后退">
                    <ArrowLeft className="w-[18px] h-[18px]" />
                  </button>
                  <button onClick={goForward} disabled={!canForward}
                    className="p-1.5 rounded-lg hover:bg-slate-100 dark:hover:bg-slate-800 disabled:opacity-30"
                    title="前进">
                    <ArrowRight className="w-[18px] h-[18px]" />
                  </button>
                  <button onClick={goUp} disabled={!canUp}
                    className="p-1.5 rounded-lg hover:bg-slate-100 dark:hover:bg-slate-800 disabled:opacity-30"
                    title="上级目录">
                    <ArrowUp className="w-[18px] h-[18px]" />
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
              </>
            )}
          </div>

          {/* 操作区（新建 / 属性 / 下载 / 删除 均移至右键菜单，工具栏只保留高频的上传与视图控制） */}
          <div className="flex items-center space-x-1 shrink-0">
            <SearchBar />
            <div className="w-px h-5 bg-slate-200 dark:bg-slate-700 mx-1" />
            <button onClick={() => fileInputRef.current?.click()}
              disabled={searchOpen}
              className="p-2 text-slate-500 hover:bg-slate-100 dark:hover:bg-slate-800 rounded-lg disabled:opacity-30 disabled:cursor-not-allowed"
              title="上传文件">
              <Upload className="w-[18px] h-[18px]" />
            </button>
            <div className="w-px h-5 bg-slate-200 dark:bg-slate-700 mx-1" />
            <button onClick={toggleHidden}
              disabled={searchOpen}
              className={cn('p-2 rounded-lg hover:bg-slate-100 dark:hover:bg-slate-800 disabled:opacity-30 disabled:cursor-not-allowed',
                showHidden ? 'text-blue-600 dark:text-blue-400' : 'text-slate-500')}
              title={showHidden ? '隐藏隐藏文件' : '显示隐藏文件'}>
              {showHidden ? <Eye className="w-[18px] h-[18px]" /> : <EyeOff className="w-[18px] h-[18px]" />}
            </button>
            <select
              value={sort}
              onChange={(e) => setSort(e.target.value as 'name' | 'size' | 'mtime')}
              disabled={searchOpen}
              className="text-sm bg-slate-100 dark:bg-slate-800 border-0 rounded-lg px-2 py-2 text-slate-600 dark:text-slate-300 outline-none disabled:opacity-30 disabled:cursor-not-allowed"
              title="排序方式">
              <option value="name">名称</option>
              <option value="size">大小</option>
              <option value="mtime">修改时间</option>
            </select>
            <button onClick={toggleOrder}
              disabled={searchOpen}
              className="p-2 text-slate-500 hover:bg-slate-100 dark:hover:bg-slate-800 rounded-lg disabled:opacity-30 disabled:cursor-not-allowed"
              title={order === 'asc' ? '升序' : '降序'}>
              {order === 'asc' ? <ArrowDownAZ className="w-[18px] h-[18px]" /> : <ArrowUpAZ className="w-[18px] h-[18px]" />}
            </button>
            <button onClick={refresh}
              disabled={searchOpen}
              className="p-2 text-slate-500 hover:bg-slate-100 dark:hover:bg-slate-800 rounded-lg disabled:opacity-30 disabled:cursor-not-allowed"
              title="刷新">
              <RefreshCw className={cn('w-[18px] h-[18px]', loading && 'animate-spin')} />
            </button>
            <div className="w-px h-5 bg-slate-200 dark:bg-slate-700 mx-1" />
            <button onClick={() => setViewMode(viewMode === 'grid' ? 'list' : 'grid')}
              disabled={searchOpen}
              className="p-2 text-slate-500 hover:bg-slate-100 dark:hover:bg-slate-800 rounded-lg disabled:opacity-30 disabled:cursor-not-allowed"
              title="切换视图">
              {viewMode === 'grid' ? <ListIcon className="w-[18px] h-[18px]" /> : <LayoutGrid className="w-[18px] h-[18px]" />}
            </button>
          </div>
        </div>
      </div>

      {/* 隐藏的文件选择器，工具栏「上传」按钮触发 */}
      <input
        ref={fileInputRef}
        type="file"
        multiple
        className="hidden"
        onChange={(e) => {
          doUpload(e.target.files);
          e.target.value = ''; // 允许重复选择同一文件再次触发
        }}
      />

      {/* 内容区（兼作拖拽上传放置区） */}
      <div className="flex-1 overflow-auto p-4 relative"
        onClick={() => clearSelection()}
        onContextMenu={onBackgroundContextMenu}
        onDragOver={(e) => {
          if (searchOpen) return;
          // 仅当拖入的是文件时才接管（忽略元素内部拖拽）。
          if (!Array.from(e.dataTransfer.types).includes('Files')) return;
          e.preventDefault();
          e.dataTransfer.dropEffect = 'copy';
          if (!dragOver) setDragOver(true);
        }}
        onDragLeave={(e) => {
          // 仅当离开内容区边界时取消高亮（忽略子元素间的冒泡）。
          if (e.currentTarget.contains(e.relatedTarget as Node)) return;
          setDragOver(false);
        }}
        onDrop={(e) => {
          if (searchOpen) return;
          if (!Array.from(e.dataTransfer.types).includes('Files')) return;
          e.preventDefault();
          setDragOver(false);
          doUpload(e.dataTransfer.files);
        }}>
        {dragOver && (
          <div className="absolute inset-2 z-20 rounded-xl border-2 border-dashed border-blue-400 bg-blue-50/70 dark:bg-blue-900/30 flex flex-col items-center justify-center pointer-events-none">
            <Upload className="w-10 h-10 text-blue-500 mb-2" />
            <p className="text-sm text-blue-600 dark:text-blue-300 font-medium">释放以上传到当前目录</p>
          </div>
        )}
        {searchOpen ? (
          <SearchResultsView
            query={searchQuery}
            results={searchResults}
            searching={searching}
            truncated={searchTruncated}
            timedOut={searchTimedOut}
            selected={searchSelected}
            onItemClick={handleHitClick}
            onOpen={openHit}
            selectedCount={selectedHitPaths.length}
            onArchive={() => void doArchivePaths(selectedHitPaths)}
            onCopy={() => { copyPathsToClipboard(selectedHitPaths); notify(`已复制 ${selectedHitPaths.length} 项到剪贴板`); }}
            onCut={() => { cutPathsToClipboard(selectedHitPaths); notify(`已剪切 ${selectedHitPaths.length} 项到剪贴板`); }}
            onDelete={() => setDialog({ kind: 'delete-paths', paths: selectedHitPaths })}
            onSelectAll={selectAllHits}
            onClearSelection={clearHitSelection}
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
          <GridView entries={entries} selected={selected} cutNames={cutNames} onItemClick={handleItemClick} onOpen={openEntry} onContextMenu={onItemContextMenu} />
        ) : (
          <ListView entries={entries} selected={selected} cutNames={cutNames} onItemClick={handleItemClick} onOpen={openEntry} onContextMenu={onItemContextMenu} />
        )}
      </div>

      {/* 状态栏 */}
      <div className="border-t border-slate-200 dark:border-slate-800 px-4 py-1.5 text-[11px] text-slate-400 flex justify-between shrink-0">
        <span>{total} 项{showHidden ? '（含隐藏）' : ''}</span>
        <div className="flex items-center gap-3">
          {clipboard && clipboard.paths.length > 0 && (
            <button
              onClick={clearClipboard}
              className="flex items-center gap-1 hover:text-slate-600 dark:hover:text-slate-300"
              title="点击清空剪贴板"
            >
              {clipboard.mode === 'cut' ? <Scissors className="w-3 h-3" /> : <Copy className="w-3 h-3" />}
              <span>剪贴板 {clipboard.paths.length} 项</span>
            </button>
          )}
          <span>{selectedEntries.length > 1 ? `已选 ${selectedEntries.length} 项` : singleSelected ? singleSelected.name : currentPath}</span>
        </div>
      </div>

      {/* 操作提示 toast */}
      {toast && (
        <div className="absolute bottom-10 left-1/2 -translate-x-1/2 z-[150] px-4 py-2 rounded-lg bg-slate-900/90 dark:bg-slate-700 text-white text-xs shadow-lg">
          {toast}
        </div>
      )}

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
            dialog.entries.length === 1 ? (
              <span>
                确定要删除 <span className="font-medium break-all">{dialog.entries[0].name}</span>
                {dialog.entries[0].type === 'dir' ? '（及其全部内容）' : ''} 吗？此操作不可恢复。
              </span>
            ) : (
              <span>
                确定要删除选中的 <span className="font-medium">{dialog.entries.length}</span> 项
                {dialog.entries.some((e) => e.type === 'dir') ? '（含目录及其全部内容）' : ''} 吗？此操作不可恢复。
              </span>
            )
          }
          onConfirm={() => remove(dialog.entries)}
          onClose={() => setDialog(null)}
        />
      )}
      {dialog?.kind === 'delete-paths' && (
        <ConfirmModal
          title="删除确认"
          confirmText="删除"
          message={
            <span>
              确定要删除选中的 <span className="font-medium">{dialog.paths.length}</span> 项吗？此操作不可恢复。
            </span>
          }
          onConfirm={() => removePaths(dialog.paths)}
          onClose={() => setDialog(null)}
        />
      )}
    </div>
  );
}

interface ViewProps {
  entries: FileEntry[];
  selected: Set<string>;
  cutNames: Set<string>;
  onItemClick: (entry: FileEntry, e: React.MouseEvent) => void;
  onOpen: (entry: FileEntry) => void;
  onContextMenu: (e: React.MouseEvent, entry: FileEntry) => void;
}

function GridView({ entries, selected, cutNames, onItemClick, onOpen, onContextMenu }: ViewProps) {
  return (
    <div className="grid grid-cols-[repeat(auto-fill,minmax(100px,1fr))] gap-2">
      {entries.map((entry) => (
        <button
          key={entry.name}
          onClick={(e) => { e.stopPropagation(); onItemClick(entry, e); }}
          onDoubleClick={() => onOpen(entry)}
          onContextMenu={(e) => onContextMenu(e, entry)}
          className={cn(
            'flex flex-col items-center p-3 rounded-xl transition-colors group relative select-none focus:outline-none',
            selected.has(entry.name)
              ? 'bg-blue-50 dark:bg-blue-900/30'
              : 'hover:bg-slate-50 dark:hover:bg-slate-900',
            cutNames.has(entry.name) && 'opacity-50',
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

function ListView({ entries, selected, cutNames, onItemClick, onOpen, onContextMenu }: ViewProps) {
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
            onClick={(e) => { e.stopPropagation(); onItemClick(entry, e); }}
            onDoubleClick={() => onOpen(entry)}
            onContextMenu={(e) => onContextMenu(e, entry)}
            className={cn(
              'cursor-default border-b border-slate-50 dark:border-slate-900/50 select-none focus:outline-none',
              selected.has(entry.name)
                ? 'bg-blue-50 dark:bg-blue-900/30'
                : 'hover:bg-slate-50 dark:hover:bg-slate-900',
              cutNames.has(entry.name) && 'opacity-50',
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
  selected: Set<string>; // 选中项完整路径集合
  onItemClick: (hit: SearchHit, e: React.MouseEvent) => void; // 单击点选（含 Ctrl/Shift 修饰）
  onOpen: (hit: SearchHit) => void; // 双击打开
  selectedCount: number;
  onArchive: () => void;
  onCopy: () => void;
  onCut: () => void;
  onDelete: () => void;
  onSelectAll: () => void;
  onClearSelection: () => void;
}

// SearchResultsView 以列表形式展示搜索命中：单击点选（支持 Ctrl/Cmd/Shift 多选），双击打开。
// 选中 ≥1 项时顶部显示批量操作条（打包下载 / 复制 / 剪切 / 删除）。
function SearchResultsView({
  query, results, searching, truncated, timedOut,
  selected, onItemClick, onOpen,
  selectedCount, onArchive, onCopy, onCut, onDelete, onSelectAll, onClearSelection,
}: SearchResultsViewProps) {
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
      {/* 顶部信息行 / 批量操作条：选中 ≥1 项时切换为操作条。 */}
      {selectedCount > 0 ? (
        <div className="flex items-center gap-2 mb-2 px-1 text-xs">
          <span className="text-slate-500 dark:text-slate-400">已选 {selectedCount} 项</span>
          <button onClick={onArchive}
            className="flex items-center gap-1 px-2 py-1 rounded-md text-slate-600 dark:text-slate-300 hover:bg-slate-100 dark:hover:bg-slate-800">
            <Download className="w-3.5 h-3.5" /> 打包下载
          </button>
          <button onClick={onCopy}
            className="flex items-center gap-1 px-2 py-1 rounded-md text-slate-600 dark:text-slate-300 hover:bg-slate-100 dark:hover:bg-slate-800">
            <Copy className="w-3.5 h-3.5" /> 复制
          </button>
          <button onClick={onCut}
            className="flex items-center gap-1 px-2 py-1 rounded-md text-slate-600 dark:text-slate-300 hover:bg-slate-100 dark:hover:bg-slate-800">
            <Scissors className="w-3.5 h-3.5" /> 剪切
          </button>
          <button onClick={onDelete}
            className="flex items-center gap-1 px-2 py-1 rounded-md text-red-500 hover:bg-red-50 dark:hover:bg-red-900/30">
            <Trash2 className="w-3.5 h-3.5" /> 删除
          </button>
          <div className="flex-1" />
          <button onClick={onSelectAll} className="px-2 py-1 rounded-md text-slate-500 hover:bg-slate-100 dark:hover:bg-slate-800">全选</button>
          <button onClick={onClearSelection} className="px-2 py-1 rounded-md text-slate-500 hover:bg-slate-100 dark:hover:bg-slate-800">取消选择</button>
        </div>
      ) : (
        <div className="text-xs text-slate-400 mb-2 px-1">
          找到 {results.length} 个匹配「{query}」的结果
          {truncated && '（已达上限，结果可能不完整）'}
          {timedOut && '（搜索超时，结果可能不完整）'}
        </div>
      )}
      <div className="divide-y divide-slate-50 dark:divide-slate-900/50">
        {results.map((hit) => (
          <button
            key={hit.path}
            onClick={(e) => onItemClick(hit, e)}
            onDoubleClick={() => onOpen(hit)}
            className={cn(
              'w-full flex items-center gap-2.5 py-2 px-2 text-left rounded-lg transition-colors select-none focus:outline-none',
              selected.has(hit.path)
                ? 'bg-blue-50 dark:bg-blue-900/30'
                : 'hover:bg-slate-50 dark:hover:bg-slate-900',
            )}
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

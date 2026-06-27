import React, { useEffect, useRef, useState } from 'react';
import { Search, X, Loader2, FolderTree, Clock } from 'lucide-react';
import { useFsStore } from '../fsStore';
import { cn } from '../lib/utils';

// SearchBar 工具栏搜索入口：输入关键字回车在当前目录搜索，可切换是否递归子目录，
// 聚焦时展示最近搜索词下拉，便于快速回到历史搜索。
export function SearchBar() {
  const {
    currentPath, searchOpen, searching, searchRecursive, searchHistory, previewPath,
    runSearch, clearSearch, toggleSearchRecursive, clearSearchHistory,
  } = useFsStore();
  const [text, setText] = useState('');
  const [focused, setFocused] = useState(false);
  const wrapRef = useRef<HTMLDivElement>(null);

  // 搜索被清除（导航 / 点 X / 点目录结果）后同步清空输入框。
  useEffect(() => {
    if (!searchOpen) setText('');
  }, [searchOpen]);

  // 搜索态下打开预览只可能来自点击文件结果，此时清空输入框（结果列表仍在预览框背后保留）。
  useEffect(() => {
    if (searchOpen && previewPath) setText('');
  }, [searchOpen, previewPath]);

  // 点击组件外部时收起历史下拉。
  useEffect(() => {
    const onDown = (e: MouseEvent) => {
      if (wrapRef.current && !wrapRef.current.contains(e.target as Node)) {
        setFocused(false);
      }
    };
    document.addEventListener('mousedown', onDown);
    return () => document.removeEventListener('mousedown', onDown);
  }, []);

  const submit = () => {
    const q = text.trim();
    if (q) {
      runSearch(q);
      setFocused(false);
    }
  };

  const clear = () => {
    setText('');
    clearSearch();
  };

  // pickHistory 选中历史词：回填输入并立即搜索。
  const pickHistory = (q: string) => {
    setText(q);
    runSearch(q);
    setFocused(false);
  };

  // 下拉展示的历史词：有输入时按子串过滤并排除完全相同项，输入为空则全部。
  const query = text.trim().toLowerCase();
  const filtered = query
    ? searchHistory.filter((h) => h.toLowerCase().includes(query) && h.toLowerCase() !== query)
    : searchHistory;
  const showDropdown = focused && filtered.length > 0;

  return (
    <div ref={wrapRef} className="relative flex items-center">
      <Search className="w-4 h-4 absolute left-2.5 text-slate-400 pointer-events-none" />
      <input
        value={text}
        onChange={(e) => setText(e.target.value)}
        onFocus={() => setFocused(true)}
        onKeyDown={(e) => {
          if (e.key === 'Enter') submit();
          if (e.key === 'Escape') {
            if (showDropdown) setFocused(false);
            else clear();
          }
        }}
        placeholder={searchRecursive ? '搜索当前目录及子目录' : '搜索当前目录'}
        className={cn(
          'w-44 lg:w-56 pl-8 pr-14 py-2 text-sm rounded-lg outline-none transition-colors',
          'bg-slate-100 dark:bg-slate-800 text-slate-700 dark:text-slate-200',
          'border border-transparent focus:border-blue-400 dark:focus:border-blue-500',
        )}
        title={
          searchRecursive
            ? `在 ${currentPath} 及其子目录内递归搜索`
            : `仅在 ${currentPath} 当前层级搜索`
        }
      />

      {/* 递归开关：开启后递归搜索子目录，关闭则仅当前目录。 */}
      <button
        onClick={toggleSearchRecursive}
        className={cn(
          'absolute right-8 p-0.5 rounded transition-colors',
          searchRecursive
            ? 'text-blue-600 dark:text-blue-400'
            : 'text-slate-400 hover:text-slate-600 dark:hover:text-slate-200',
        )}
        title={searchRecursive ? '递归搜索子目录（已开启，点击仅搜当前目录）' : '仅搜当前目录（点击开启递归子目录）'}
      >
        <FolderTree className="w-4 h-4" />
      </button>

      {searching ? (
        <Loader2 className="w-4 h-4 absolute right-2.5 text-slate-400 animate-spin" />
      ) : (searchOpen || text) ? (
        <button
          onClick={clear}
          className="absolute right-2 p-0.5 text-slate-400 hover:text-slate-600 dark:hover:text-slate-200"
          title="清除搜索"
        >
          <X className="w-4 h-4" />
        </button>
      ) : null}

      {/* 最近搜索词下拉 */}
      {showDropdown && (
        <div className="absolute top-full left-0 mt-1 w-full min-w-[14rem] py-1 rounded-lg shadow-lg z-50 bg-white dark:bg-slate-800 border border-slate-200 dark:border-slate-700">
          <div className="px-2.5 py-1 flex items-center justify-between">
            <span className="text-[10px] font-medium text-slate-400 uppercase tracking-wider">最近搜索</span>
            <button
              onMouseDown={(e) => e.preventDefault()}
              onClick={clearSearchHistory}
              className="text-[10px] text-slate-400 hover:text-rose-500 dark:hover:text-rose-400 transition-colors"
              title="清空搜索历史"
            >
              清空
            </button>
          </div>
          <div className="max-h-64 overflow-y-auto">
            {filtered.map((h) => (
              <button
                key={h}
                onClick={() => pickHistory(h)}
                className="w-full flex items-center gap-2 px-2.5 py-1.5 text-left text-sm text-slate-600 dark:text-slate-300 hover:bg-slate-100 dark:hover:bg-slate-700/60 transition-colors"
              >
                <Clock className="w-3.5 h-3.5 shrink-0 text-slate-400" />
                <span className="truncate">{h}</span>
              </button>
            ))}
          </div>
        </div>
      )}
    </div>
  );
}

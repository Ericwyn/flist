import React, { useState } from 'react';
import { Search, X, Loader2 } from 'lucide-react';
import { useFsStore } from '../fsStore';
import { cn } from '../lib/utils';

// SearchBar 工具栏搜索入口：输入关键字回车在当前目录递归搜索。
export function SearchBar() {
  const { currentPath, searchOpen, searching, runSearch, clearSearch } = useFsStore();
  const [text, setText] = useState('');

  const submit = () => {
    const q = text.trim();
    if (q) runSearch(q);
  };

  const clear = () => {
    setText('');
    clearSearch();
  };

  return (
    <div className="relative flex items-center">
      <Search className="w-3.5 h-3.5 absolute left-2.5 text-slate-400 pointer-events-none" />
      <input
        value={text}
        onChange={(e) => setText(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === 'Enter') submit();
          if (e.key === 'Escape') clear();
        }}
        placeholder="在当前目录搜索"
        className={cn(
          'w-40 lg:w-52 pl-8 pr-7 py-1.5 text-xs rounded-lg outline-none transition-colors',
          'bg-slate-100 dark:bg-slate-800 text-slate-700 dark:text-slate-200',
          'border border-transparent focus:border-blue-400 dark:focus:border-blue-500',
        )}
        title={`在 ${currentPath} 内递归搜索`}
      />
      {searching ? (
        <Loader2 className="w-3.5 h-3.5 absolute right-2.5 text-slate-400 animate-spin" />
      ) : (searchOpen || text) ? (
        <button
          onClick={clear}
          className="absolute right-2 p-0.5 text-slate-400 hover:text-slate-600 dark:hover:text-slate-200"
          title="清除搜索"
        >
          <X className="w-3.5 h-3.5" />
        </button>
      ) : null}
    </div>
  );
}

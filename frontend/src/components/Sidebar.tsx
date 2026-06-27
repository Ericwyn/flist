import React from 'react';
import { HardDrive, Moon, Sun, Home, X } from 'lucide-react';
import { useStore } from '../store';
import { cn } from '../lib/utils';
import { FileIcon } from './FileIcon';

const formatCapacity = (bytes: number) => {
  if (bytes === 0) return '0 B';
  const k = 1024;
  const sizes = ['B', 'KB', 'MB', 'GB', 'TB'];
  const i = Math.floor(Math.log(bytes) / Math.log(k));
  return parseFloat((bytes / Math.pow(k, i)).toFixed(1)) + ' ' + sizes[i];
};

export function Sidebar() {
  const { files, favorites, currentFolderId, setCurrentFolder, theme, toggleTheme, openPreview, removeRecentAccess } = useStore();

  const favoriteFolders = favorites.map(id => files.find(f => f.id === id)).filter(Boolean) as any[];
  const recentFiles = files
    .filter(f => f.accessedAt)
    .sort((a, b) => (b.accessedAt || 0) - (a.accessedAt || 0))
    .slice(0, 10);

  const totalStorage = 10 * 1024 * 1024 * 1024; // 10 GB
  const usedStorage = files.filter(f => f.type !== 'folder').reduce((acc, f) => acc + f.size, 0);
  const usagePercent = Math.min((usedStorage / totalStorage) * 100, 100);

  return (
    <div className="w-52 bg-[#f1f5f9] dark:bg-slate-900/50 border-r border-slate-200 dark:border-slate-800 flex flex-col h-full shrink-0 transition-colors duration-200">
      <div className="p-3 flex items-center space-x-2 text-slate-800 dark:text-slate-200 mb-1">
        <HardDrive className="w-5 h-5 text-blue-600 dark:text-blue-400" />
        <span className="text-lg font-bold tracking-tight">Flist</span>
      </div>

      <div className="flex-1 overflow-y-auto p-3 pt-0">
        <div className="space-y-4">
          <section>
            <h3 className="text-[11px] font-medium text-slate-400 dark:text-slate-500 uppercase tracking-wider mb-2 px-2">
              导航
            </h3>
            <div className="space-y-0.5">
              <button
                onClick={() => setCurrentFolder('root')}
                className={cn(
                  "w-full flex items-center px-2 py-1.5 rounded-lg text-sm transition-colors",
                  currentFolderId === 'root'
                    ? "bg-blue-50 text-blue-700 font-medium dark:bg-blue-900/40 dark:text-blue-300"
                    : "text-slate-600 dark:text-slate-400 hover:bg-white dark:hover:bg-slate-800"
                )}
              >
                <Home className="w-4 h-4 mr-2.5 opacity-80" />
                <span>我的文件</span>
              </button>
            </div>
          </section>

          <section>
            <div className="flex items-center justify-between mb-2 px-2">
              <h3 className="text-[11px] font-medium text-slate-400 dark:text-slate-500 uppercase tracking-wider">收藏夹</h3>
            </div>
            {favoriteFolders.length === 0 ? (
              <p className="px-2 text-[11px] text-slate-400 dark:text-slate-500 italic">暂无收藏</p>
            ) : (
              <div className="space-y-0.5">
                {favoriteFolders.map((folder, idx) => {
                  const colors = [
                    "bg-blue-400",
                    "bg-amber-400",
                    "bg-emerald-400",
                    "bg-purple-400",
                    "bg-rose-400"
                  ];
                  const dotColor = colors[idx % colors.length];

                  return (
                    <button
                      key={folder.id}
                      onClick={() => setCurrentFolder(folder.id)}
                      className={cn(
                        "w-full flex items-center px-2 py-1.5 rounded-lg text-sm transition-colors",
                        currentFolderId === folder.id
                          ? "bg-blue-50 text-blue-700 font-medium dark:bg-blue-900/40 dark:text-blue-300"
                          : "text-slate-600 dark:text-slate-400 hover:bg-white dark:hover:bg-slate-800"
                      )}
                    >
                      <div className={cn("w-1.5 h-1.5 rounded-full mr-3 ml-1", dotColor)}></div>
                      <span className="truncate">{folder.name}</span>
                    </button>
                  );
                })}
              </div>
            )}
          </section>

          <section>
            <div className="flex items-center justify-between mb-2 px-2">
              <h3 className="text-[11px] font-medium text-slate-400 dark:text-slate-500 uppercase tracking-wider">最近访问</h3>
            </div>
            {recentFiles.length === 0 ? (
              <p className="px-2 text-[11px] text-slate-400 dark:text-slate-500 italic">暂无记录</p>
            ) : (
              <div className="space-y-0.5">
                {recentFiles.map((file) => {
                  return (
                    <div
                      key={file.id}
                      className="group w-full flex items-center justify-between px-2 py-1.5 rounded-lg text-sm transition-colors text-slate-600 dark:text-slate-400 hover:bg-white dark:hover:bg-slate-800"
                    >
                      <button
                        onClick={() => file.type === 'folder' ? setCurrentFolder(file.id) : openPreview(file.id)}
                        className="flex-1 flex items-center min-w-0"
                      >
                        <FileIcon type={file.type} className="w-4 h-4 mr-2.5 opacity-80 shrink-0" />
                        <span className="truncate">{file.name}</span>
                      </button>
                      <button
                        onClick={() => removeRecentAccess(file.id)}
                        className="opacity-0 group-hover:opacity-100 p-0.5 text-slate-400 hover:text-slate-600 dark:hover:text-slate-300 rounded transition-all"
                        title="移除记录"
                      >
                        <X className="w-3 h-3" />
                      </button>
                    </div>
                  );
                })}
              </div>
            )}
          </section>
        </div>
      </div>

      <div className="px-4 py-3 border-t border-slate-200 dark:border-slate-800 flex flex-col space-y-3">
        <div className="flex flex-col space-y-1.5">
          <div className="flex items-center justify-between text-[10px] text-slate-500 dark:text-slate-400">
            <span>已用 {formatCapacity(usedStorage)}</span>
            <span>共 {formatCapacity(totalStorage)}</span>
          </div>
          <div className="w-full h-1.5 bg-slate-200 dark:bg-slate-800 rounded-full overflow-hidden">
            <div 
              className="h-full bg-blue-500 dark:bg-blue-600 rounded-full"
              style={{ width: `${usagePercent}%` }}
            />
          </div>
        </div>
        
        <div className="flex justify-center">
          <div className="bg-slate-200/70 dark:bg-slate-800 p-0.5 rounded-full flex items-center w-[84px] relative">
            <div 
              className={cn(
                "absolute left-0.5 top-0.5 bottom-0.5 w-[calc(50%-2px)] bg-white dark:bg-slate-700 rounded-full shadow-sm transition-transform duration-300 ease-in-out", 
                theme === 'dark' ? "translate-x-full" : "translate-x-0"
              )}
            />
            <button
              onClick={() => theme === 'dark' && toggleTheme()}
              className={cn(
                "flex-1 flex justify-center py-1 z-10 transition-colors",
                theme === 'light' ? "text-amber-500" : "text-slate-400 hover:text-slate-300"
              )}
            >
              <Sun className="w-3.5 h-3.5" />
            </button>
            <button
              onClick={() => theme === 'light' && toggleTheme()}
              className={cn(
                "flex-1 flex justify-center py-1 z-10 transition-colors",
                theme === 'dark' ? "text-blue-400" : "text-slate-400 hover:text-slate-500"
              )}
            >
              <Moon className="w-3.5 h-3.5" />
            </button>
          </div>
        </div>
      </div>
    </div>
  );
}

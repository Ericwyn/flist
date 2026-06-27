import React from 'react';
import { HardDrive, Moon, Sun, Home, LogOut } from 'lucide-react';
import { useStore } from '../store';
import { useFsStore } from '../fsStore';
import { useAuthStore } from '../authStore';
import { cn } from '../lib/utils';

export function Sidebar() {
  const { theme, toggleTheme } = useStore();
  const { currentPath, navigate } = useFsStore();
  const { user, logout } = useAuthStore();

  const atRoot = currentPath === '/';

  return (
    <div className="w-52 bg-[#f1f5f9] dark:bg-slate-900/50 border-r border-slate-200 dark:border-slate-800 flex flex-col h-full shrink-0 transition-colors duration-200">
      <div className="p-3 flex items-center space-x-2 text-slate-800 dark:text-slate-200 mb-1">
        <HardDrive className="w-5 h-5 text-blue-600 dark:text-blue-400" />
        <span className="text-lg font-bold tracking-tight">Flist</span>
      </div>

      <div className="flex-1 overflow-y-auto p-3 pt-0">
        <section>
          <h3 className="text-[11px] font-medium text-slate-400 dark:text-slate-500 uppercase tracking-wider mb-2 px-2">
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

        {/* 收藏夹（Phase 3）与最近访问、磁盘用量（Phase 6）将在后续阶段接入。 */}
      </div>

      <div className="px-4 py-3 border-t border-slate-200 dark:border-slate-800 flex flex-col space-y-3">
        <div className="flex items-center justify-between gap-2">
          <div className="flex items-center min-w-0 gap-2">
            <div className="w-6 h-6 rounded-full bg-blue-100 dark:bg-blue-900/40 flex items-center justify-center text-[11px] font-semibold text-blue-700 dark:text-blue-300 shrink-0">
              {(user?.username ?? '?').slice(0, 1).toUpperCase()}
            </div>
            <span className="text-xs text-slate-600 dark:text-slate-300 truncate" title={user?.username}>
              {user?.username ?? '未登录'}
            </span>
          </div>
          <button
            onClick={() => logout()}
            className="p-1.5 text-slate-400 hover:text-rose-500 hover:bg-rose-50 dark:hover:bg-rose-900/20 rounded-lg transition-colors shrink-0"
            title="登出"
          >
            <LogOut className="w-4 h-4" />
          </button>
        </div>

        <div className="flex justify-center">
          <div className="bg-slate-200/70 dark:bg-slate-800 p-0.5 rounded-full flex items-center w-[84px] relative">
            <div
              className={cn(
                'absolute left-0.5 top-0.5 bottom-0.5 w-[calc(50%-2px)] bg-white dark:bg-slate-700 rounded-full shadow-sm transition-transform duration-300 ease-in-out',
                theme === 'dark' ? 'translate-x-full' : 'translate-x-0',
              )}
            />
            <button
              onClick={() => theme === 'dark' && toggleTheme()}
              className={cn(
                'flex-1 flex justify-center py-1 z-10 transition-colors',
                theme === 'light' ? 'text-amber-500' : 'text-slate-400 hover:text-slate-300',
              )}
            >
              <Sun className="w-3.5 h-3.5" />
            </button>
            <button
              onClick={() => theme === 'light' && toggleTheme()}
              className={cn(
                'flex-1 flex justify-center py-1 z-10 transition-colors',
                theme === 'dark' ? 'text-blue-400' : 'text-slate-400 hover:text-slate-500',
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

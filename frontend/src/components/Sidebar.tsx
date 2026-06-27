import React, { useState } from 'react';
import { HardDrive, Home, Settings } from 'lucide-react';
import { useFsStore } from '../fsStore';
import { useAuthStore } from '../authStore';
import { cn } from '../lib/utils';
import { SettingsModal } from './SettingsModal';

export function Sidebar() {
  const { currentPath, navigate } = useFsStore();
  const { user } = useAuthStore();
  const [settingsOpen, setSettingsOpen] = useState(false);

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
    </div>
  );
}

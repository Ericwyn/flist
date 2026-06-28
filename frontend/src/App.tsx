import React, { lazy, Suspense, useEffect } from 'react';
import { Loader2 } from 'lucide-react';
import { Sidebar } from './components/Sidebar';
import { FileBrowser } from './components/FileBrowser';
import { LoginPage } from './components/LoginPage';
import { TransferPanel } from './components/TransferPanel';
import { useStore } from './store';
import { useAuthStore } from './authStore';
import { useFsStore } from './fsStore';

const PreviewModal = lazy(() => import('./components/PreviewModal').then((mod) => ({ default: mod.PreviewModal })));
const Editor = lazy(() => import('./components/Editor').then((mod) => ({ default: mod.Editor })));

function PageLoading() {
  return (
    <div className="flex h-screen w-full items-center justify-center bg-[#f8fafc] dark:bg-slate-900">
      <Loader2 className="w-6 h-6 text-blue-500 animate-spin" />
    </div>
  );
}

export default function App() {
  const theme = useStore((state) => state.theme);
  const { status, init } = useAuthStore();
  const previewEntry = useFsStore((state) => state.previewEntry);

  // Apply initial theme
  useEffect(() => {
    if (theme === 'dark') {
      document.documentElement.classList.add('dark');
    } else {
      document.documentElement.classList.remove('dark');
    }
  }, [theme]);

  // 应用启动时校验本地令牌，决定进入登录页还是主界面。
  useEffect(() => {
    init();
  }, [init]);

  if (status === 'loading') {
    return <PageLoading />;
  }

  if (status === 'unauthenticated') {
    return <LoginPage />;
  }

  // 编辑器独立页面：/editor?path=...（支持新窗口打开，同源复用登录态）。
  // 与文件浏览主界面互斥渲染，避免侧边栏 / 浏览器列表抢占整屏。
  if (window.location.pathname === '/editor') {
    return (
      <Suspense fallback={<PageLoading />}>
        <Editor />
      </Suspense>
    );
  }

  return (
    <div className="flex h-screen w-full overflow-hidden bg-[#f8fafc] dark:bg-slate-900 text-slate-800 dark:text-slate-100 font-sans">
      <Sidebar />
      <FileBrowser />
      {previewEntry && (
        <Suspense fallback={null}>
          <PreviewModal />
        </Suspense>
      )}
      <TransferPanel />
    </div>
  );
}

import React, { useEffect } from 'react';
import { Sidebar } from './components/Sidebar';
import { FileBrowser } from './components/FileBrowser';
import { PreviewModal } from './components/PreviewModal';
import { useStore } from './store';

export default function App() {
  const theme = useStore((state) => state.theme);

  // Apply initial theme
  useEffect(() => {
    if (theme === 'dark') {
      document.documentElement.classList.add('dark');
    } else {
      document.documentElement.classList.remove('dark');
    }
  }, [theme]);

  return (
    <div className="flex h-screen w-full overflow-hidden bg-[#f8fafc] dark:bg-slate-900 text-slate-800 dark:text-slate-100 font-sans">
      <Sidebar />
      <FileBrowser />
      <PreviewModal />
    </div>
  );
}

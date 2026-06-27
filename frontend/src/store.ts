import { create } from 'zustand';

// UIState 仅保存与文件系统无关的界面偏好；文件浏览状态见 fsStore.ts。
interface UIState {
  theme: 'light' | 'dark';
  viewMode: 'grid' | 'list';
  iconSize: 'small' | 'medium' | 'large';

  toggleTheme: () => void;
  setViewMode: (mode: 'grid' | 'list') => void;
  setIconSize: (size: 'small' | 'medium' | 'large') => void;
}

export const useStore = create<UIState>((set) => ({
  theme: 'light',
  viewMode: 'grid',
  iconSize: 'medium',

  toggleTheme: () =>
    set((state) => {
      const next = state.theme === 'light' ? 'dark' : 'light';
      if (next === 'dark') {
        document.documentElement.classList.add('dark');
      } else {
        document.documentElement.classList.remove('dark');
      }
      return { theme: next };
    }),

  setViewMode: (mode) => set({ viewMode: mode }),
  setIconSize: (size) => set({ iconSize: size }),
}));

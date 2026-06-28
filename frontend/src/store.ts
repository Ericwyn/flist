import { create } from 'zustand';
import { RecentAccessItem } from './types';

type Theme = 'light' | 'dark';
type ViewMode = 'grid' | 'list';

const UI_PREFS_KEY = 'flist.uiPrefs';
const RECENT_ACCESS_KEY = 'flist.recentAccess';
const DEFAULT_RECENT_LIMIT = 10;
const MIN_RECENT_LIMIT = 1;
const MAX_RECENT_LIMIT = 50;
const VIEW_SCALE_MIN = 0.75;
const VIEW_SCALE_MAX = 1.4;
const VIEW_SCALE_STEP = 0.1;
const DEFAULT_VIEW_SCALE = 1;

function clampRecentLimit(limit: number): number {
  if (!Number.isFinite(limit)) return DEFAULT_RECENT_LIMIT;
  return Math.min(MAX_RECENT_LIMIT, Math.max(MIN_RECENT_LIMIT, Math.floor(limit)));
}

function clampViewScale(scale: number): number {
  return Math.min(VIEW_SCALE_MAX, Math.max(VIEW_SCALE_MIN, Number(scale.toFixed(2))));
}

type UiPrefs = Pick<UIState, 'viewMode' | 'viewScale' | 'recentEnabled' | 'recentLimit'>;

function loadUiPrefs(): UiPrefs {
  try {
    const raw = localStorage.getItem(UI_PREFS_KEY);
    if (!raw) return { viewMode: 'grid', viewScale: DEFAULT_VIEW_SCALE, recentEnabled: true, recentLimit: DEFAULT_RECENT_LIMIT };
    const parsed = JSON.parse(raw) as Partial<UiPrefs>;
    return {
      viewMode: parsed.viewMode === 'list' || parsed.viewMode === 'grid' ? parsed.viewMode : 'grid',
      viewScale: typeof parsed.viewScale === 'number' ? clampViewScale(parsed.viewScale) : DEFAULT_VIEW_SCALE,
      recentEnabled: typeof parsed.recentEnabled === 'boolean' ? parsed.recentEnabled : true,
      recentLimit: typeof parsed.recentLimit === 'number' ? clampRecentLimit(parsed.recentLimit) : DEFAULT_RECENT_LIMIT,
    };
  } catch {
    return { viewMode: 'grid', viewScale: DEFAULT_VIEW_SCALE, recentEnabled: true, recentLimit: DEFAULT_RECENT_LIMIT };
  }
}

function saveUiPrefs(prefs: UiPrefs) {
  try {
    localStorage.setItem(UI_PREFS_KEY, JSON.stringify(prefs));
  } catch {
    // 忽略隐私模式或存储配额导致的失败，当前会话内状态仍然有效。
  }
}

function loadRecentAccess(limit: number): RecentAccessItem[] {
  try {
    const raw = localStorage.getItem(RECENT_ACCESS_KEY);
    if (!raw) return [];
    const parsed = JSON.parse(raw);
    if (!Array.isArray(parsed)) return [];
    return parsed
      .filter((item): item is RecentAccessItem => (
        item &&
        typeof item.path === 'string' &&
        typeof item.name === 'string' &&
        (item.type === 'file' || item.type === 'dir') &&
        typeof item.visitedAt === 'number'
      ))
      .slice(0, limit);
  } catch {
    return [];
  }
}

function saveRecentAccess(items: RecentAccessItem[], limit: number) {
  try {
    localStorage.setItem(RECENT_ACCESS_KEY, JSON.stringify(items.slice(0, limit)));
  } catch {
    // 忽略持久化失败，最近访问仍在当前内存态可用。
  }
}

// UIState 仅保存与文件系统无关的界面偏好；文件浏览状态见 fsStore.ts。
interface UIState {
  theme: Theme;
  viewMode: ViewMode;
  viewScale: number;
  iconSize: 'small' | 'medium' | 'large';
  recentAccess: RecentAccessItem[];
  recentEnabled: boolean;
  recentLimit: number;

  toggleTheme: () => void;
  setViewMode: (mode: ViewMode) => void;
  setViewScale: (scale: number) => void;
  zoomIn: () => void;
  zoomOut: () => void;
  resetViewScale: () => void;
  setIconSize: (size: 'small' | 'medium' | 'large') => void;
  recordRecentAccess: (item: Omit<RecentAccessItem, 'visitedAt'>) => void;
  clearRecentAccess: () => void;
  setRecentEnabled: (enabled: boolean) => void;
  setRecentLimit: (limit: number) => void;
}

const initialPrefs = loadUiPrefs();
const initialRecentAccess = loadRecentAccess(initialPrefs.recentLimit);

export const useStore = create<UIState>((set, get) => ({
  theme: 'light',
  viewMode: initialPrefs.viewMode,
  viewScale: initialPrefs.viewScale,
  iconSize: 'medium',
  recentAccess: initialRecentAccess,
  recentEnabled: initialPrefs.recentEnabled,
  recentLimit: initialPrefs.recentLimit,

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

  setViewMode: (mode) => {
    set({ viewMode: mode });
    saveUiPrefs({ viewMode: mode, viewScale: get().viewScale, recentEnabled: get().recentEnabled, recentLimit: get().recentLimit });
  },
  setViewScale: (scale) => {
    const next = clampViewScale(scale);
    set({ viewScale: next });
    saveUiPrefs({ viewMode: get().viewMode, viewScale: next, recentEnabled: get().recentEnabled, recentLimit: get().recentLimit });
  },
  zoomIn: () => get().setViewScale(get().viewScale + VIEW_SCALE_STEP),
  zoomOut: () => get().setViewScale(get().viewScale - VIEW_SCALE_STEP),
  resetViewScale: () => get().setViewScale(DEFAULT_VIEW_SCALE),
  setIconSize: (size) => set({ iconSize: size }),
  recordRecentAccess: (item) => {
    if (!get().recentEnabled) return;
    const limit = get().recentLimit;
    const nextItem: RecentAccessItem = { ...item, visitedAt: Date.now() };
    const next = [
      nextItem,
      ...get().recentAccess.filter((x) => x.path !== item.path),
    ].slice(0, limit);
    set({ recentAccess: next });
    saveRecentAccess(next, limit);
  },
  clearRecentAccess: () => {
    set({ recentAccess: [] });
    saveRecentAccess([], get().recentLimit);
  },
  setRecentEnabled: (enabled) => {
    set({ recentEnabled: enabled });
    saveUiPrefs({ viewMode: get().viewMode, viewScale: get().viewScale, recentEnabled: enabled, recentLimit: get().recentLimit });
  },
  setRecentLimit: (limit) => {
    const next = clampRecentLimit(limit);
    const truncated = get().recentAccess.slice(0, next);
    set({ recentLimit: next, recentAccess: truncated });
    saveUiPrefs({ viewMode: get().viewMode, viewScale: get().viewScale, recentEnabled: get().recentEnabled, recentLimit: next });
    saveRecentAccess(truncated, next);
  },
}));

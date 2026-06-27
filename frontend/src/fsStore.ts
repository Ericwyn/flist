import { create } from 'zustand';
import { api, ApiError } from './lib/api';
import { FileEntry } from './types';
import { parentPath } from './lib/path';
import { useAuthStore } from './authStore';

type SortKey = 'name' | 'size' | 'mtime';
type SortOrder = 'asc' | 'desc';

interface FsState {
  currentPath: string;
  entries: FileEntry[];
  total: number;
  loading: boolean;
  error: string | null;

  sort: SortKey;
  order: SortOrder;
  showHidden: boolean;

  // 路径历史栈（用于前进/后退）。
  history: string[];
  historyIndex: number;

  selected: string | null; // 选中条目名
  previewEntry: FileEntry | null; // 当前预览的条目（含所在目录拼出的完整路径见 previewPath）
  previewPath: string | null;

  navigate: (path: string, pushHistory?: boolean) => Promise<void>;
  initFromUrl: () => void;
  restore: (path: string, index: number) => void;
  refresh: () => Promise<void>;
  goBack: () => void;
  goForward: () => void;
  goUp: () => void;
  setSort: (sort: SortKey) => void;
  toggleOrder: () => void;
  toggleHidden: () => void;
  select: (name: string | null) => void;
  openPreview: (entry: FileEntry, fullPath: string) => void;
  closePreview: () => void;
}

// URL_PREFIX 为文件浏览路由的统一前缀，避免与 /api、/assets 等真实路径冲突
// （例如 root 下恰好存在名为 api / assets 的文件夹）。
const URL_PREFIX = '/files';

// pathToUrl / urlToPath 在浏览器 URL pathname 与 API 路径间转换。
// 采用 History 路由：/files 即根，/files/Roboto_Mono/static 即目录 /Roboto_Mono/static
//（逐段编码以兼容空格等特殊字符，保留 /）。
function pathToUrl(p: string): string {
  if (p === '/' || p === '') return URL_PREFIX;
  return URL_PREFIX + '/' + p.replace(/^\//, '').split('/').map(encodeURIComponent).join('/');
}
function urlToPath(pathname: string): string {
  // 仅 /files 前缀下的路径视为目录路径；其余（如登录后短暂停留的 /）一律回到根。
  if (pathname === URL_PREFIX || pathname === URL_PREFIX + '/') return '/';
  if (!pathname.startsWith(URL_PREFIX + '/')) return '/';
  const rest = pathname.slice(URL_PREFIX.length).replace(/^\//, '');
  if (!rest) return '/';
  try {
    return '/' + rest.split('/').map(decodeURIComponent).join('/');
  } catch {
    return '/';
  }
}

export const useFsStore = create<FsState>((set, get) => ({
  currentPath: '/',
  entries: [],
  total: 0,
  loading: false,
  error: null,
  sort: 'name',
  order: 'asc',
  showHidden: false,
  history: ['/'],
  historyIndex: 0,
  selected: null,
  previewEntry: null,
  previewPath: null,

  navigate: async (path, pushHistory = true) => {
    const { sort, order, showHidden } = get();
    set({ loading: true, error: null, selected: null });
    try {
      const res = await api.fs.list(path, { sort, order, showHidden, pageSize: 1000 });
      set((state) => {
        let history = state.history;
        let historyIndex = state.historyIndex;
        if (pushHistory && res.path !== state.currentPath) {
          history = state.history.slice(0, state.historyIndex + 1);
          history.push(res.path);
          historyIndex = history.length - 1;
          // 同步推入浏览器历史，使物理前进/后退键与 URL 一致。
          window.history.pushState(
            { index: historyIndex, path: res.path },
            '',
            pathToUrl(res.path),
          );
        } else {
          // 非 push（首次/刷新/前进后退恢复）：用 replaceState 校正 URL。
          window.history.replaceState(
            { index: historyIndex, path: res.path },
            '',
            pathToUrl(res.path),
          );
        }
        return {
          currentPath: res.path,
          entries: res.items,
          total: res.total,
          loading: false,
          history,
          historyIndex,
        };
      });
    } catch (e) {
      handleError(e);
      const msg = e instanceof Error ? e.message : '加载失败';
      set({ loading: false, error: msg });
    }
  },

  // initFromUrl 在应用挂载时按 URL pathname 恢复目录，并初始化浏览器历史 state。
  initFromUrl: () => {
    const path = urlToPath(window.location.pathname);
    set({ history: [path], historyIndex: 0 });
    window.history.replaceState({ index: 0, path }, '', pathToUrl(path));
    get().navigate(path, false);
  },

  // restore 由 popstate 调用：跳转到浏览器历史 state 指向的路径与索引，不再二次入栈。
  restore: (path, index) => {
    set({ historyIndex: index });
    get().navigate(path, false);
  },

  refresh: async () => {
    await get().navigate(get().currentPath, false);
  },

  // 工具栏按钮委托给浏览器历史，统一经 popstate 触发，保证内部栈与浏览器一致。
  goBack: () => {
    if (get().historyIndex > 0) {
      window.history.back();
    }
  },

  goForward: () => {
    if (get().historyIndex < get().history.length - 1) {
      window.history.forward();
    }
  },

  goUp: () => {
    const up = parentPath(get().currentPath);
    if (up !== get().currentPath) {
      get().navigate(up);
    }
  },

  setSort: (sort) => {
    set({ sort });
    get().refresh();
  },

  toggleOrder: () => {
    set({ order: get().order === 'asc' ? 'desc' : 'asc' });
    get().refresh();
  },

  toggleHidden: () => {
    set({ showHidden: !get().showHidden });
    get().refresh();
  },

  select: (name) => set({ selected: name }),

  openPreview: (entry, fullPath) => set({ previewEntry: entry, previewPath: fullPath }),
  closePreview: () => set({ previewEntry: null, previewPath: null }),
}));

// handleError 在遇到 401（会话失效）时登出回到登录页。
function handleError(e: unknown) {
  if (e instanceof ApiError && e.code === 1001) {
    useAuthStore.getState().logout();
  }
}

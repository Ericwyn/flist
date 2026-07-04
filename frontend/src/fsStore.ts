import { create } from 'zustand';
import { api, ApiError } from './lib/api';
import { FileEntry, SearchHit, Clipboard } from './types';
import { parentPath, joinPath } from './lib/path';
import { useAuthStore } from './authStore';
import { useStore } from './store';
import { useFileOpStore } from './fileOpStore';

type SortKey = 'name' | 'size' | 'mtime';
type SortOrder = 'asc' | 'desc';

interface FsState {
  currentPath: string;
  entries: FileEntry[];
  total: number;
  loading: boolean;
  error: string | null;

  // spaceVersion 在每次成功 navigate（含写操作后的 refresh）时自增，
  // 供侧边栏路径级容量展示感知「目录切换 / 写操作完成」并重新拉取。
  spaceVersion: number;

  sort: SortKey;
  order: SortOrder;
  showHidden: boolean;

  // 路径历史栈（用于前进/后退）。
  history: string[];
  historyIndex: number;

  selected: Set<string>; // 选中条目名集合（多选）
  selectionAnchor: string | null; // Shift 范围选择的锚点条目名
  previewEntry: FileEntry | null; // 当前预览的条目（含所在目录拼出的完整路径见 previewPath）
  previewPath: string | null;

  // 搜索状态。
  searchOpen: boolean;
  searchQuery: string;
  searching: boolean;
  searchResults: SearchHit[];
  searchTruncated: boolean;
  searchTimedOut: boolean;
  searchRecursive: boolean; // 是否递归搜索子目录（会话级偏好）
  searchHistory: string[]; // 最近搜索词（最多 10 条，最新在前，持久化到 localStorage）
  searchSelected: Set<string>; // 搜索结果选中项（完整路径集合，跨目录）
  searchAnchor: string | null; // 搜索结果 Shift 范围选择锚点（完整路径）

  // 剪贴板状态（复制 / 剪切两态）。
  clipboard: Clipboard | null;

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

  // 多选：单选 / 切换 / 范围 / 全选 / 清空。
  selectOne: (name: string | null) => void;
  toggleSelect: (name: string) => void;
  rangeSelect: (name: string) => void;
  selectAll: () => void;
  clearSelection: () => void;

  // 搜索结果多选（按完整路径，跨目录）：单选 / 切换 / 范围 / 全选 / 清空。
  selectOneHit: (path: string | null) => void;
  toggleHit: (path: string) => void;
  rangeHit: (path: string) => void;
  selectAllHits: () => void;
  clearHitSelection: () => void;

  openPreview: (entry: FileEntry, fullPath: string) => void;
  closePreview: () => void;

  // 写操作（成功后刷新当前目录）；返回错误信息字符串，成功返回 null。
  mkdir: (name: string) => Promise<string | null>;
  touch: (name: string) => Promise<string | null>;
  rename: (entry: FileEntry, newName: string) => Promise<string | null>;
  remove: (entries: FileEntry[]) => Promise<string | null>;
  // removePaths 按完整路径批量删除（供搜索结果使用）；删除后若在搜索态则重跑搜索，否则刷新目录。
  removePaths: (paths: string[]) => Promise<string | null>;

  // 搜索。
  runSearch: (query: string) => Promise<void>;
  clearSearch: () => void;
  // exitSearch 显式退出搜索：清空搜索态并刷新当前目录，使搜索期间被删/改的文件不再以旧快照残留。
  exitSearch: () => void;
  toggleSearchRecursive: () => void;
  clearSearchHistory: () => void;

  // 剪贴板：复制 / 剪切选中项，粘贴到当前目录（粘贴用 auto_rename 自动避让）。
  // 返回错误信息字符串，成功返回 null。
  copyToClipboard: (entries: FileEntry[]) => void;
  cutToClipboard: (entries: FileEntry[]) => void;
  // 路径版剪贴板（供搜索结果使用，条目跨目录、只有完整路径）。
  copyPathsToClipboard: (paths: string[]) => void;
  cutPathsToClipboard: (paths: string[]) => void;
  paste: () => Promise<string | null>;
  clearClipboard: () => void;
}

// DEFAULT_PATH 为登录后的默认落地路径。设备管理路径分层后，普通文件收在 /files 下，
// 外接设备收在 /drive 下；虚拟根 / 仅列出这两个挂载点，正常不停留。
const DEFAULT_PATH = '/files';

// pathToUrl / urlToPath 在浏览器 URL pathname 与虚拟 API 路径间转换。
// 设备管理路径分层后，API 路径本身即以 /files 或 /drive 开头，与 URL pathname 一一对应，
// 仅需逐段编码 / 解码以兼容空格等特殊字符。虚拟根 / 归一到 /files。
function pathToUrl(p: string): string {
  if (p === '/' || p === '') return DEFAULT_PATH;
  return '/' + p.replace(/^\//, '').split('/').map(encodeURIComponent).join('/');
}
function urlToPath(pathname: string): string {
  const raw = pathname.replace(/^\//, '');
  if (!raw) return DEFAULT_PATH;
  let segs: string[];
  try {
    segs = raw.split('/').map(decodeURIComponent);
  } catch {
    return DEFAULT_PATH;
  }
  // 仅 files / drive 是有效挂载点前缀；其余（含旧的根相对 URL）回落到默认落地页。
  if (segs[0] !== 'files' && segs[0] !== 'drive') return DEFAULT_PATH;
  return '/' + segs.join('/');
}

function pathName(p: string): string {
  if (p === '/files' || p === '/' || p === '') return '我的文件';
  if (p === '/drive') return '设备';
  const trimmed = p.replace(/\/$/, '');
  return trimmed.slice(trimmed.lastIndexOf('/') + 1) || trimmed;
}

// 搜索历史持久化：保存最近搜索词到 localStorage，跨会话保留。
const SEARCH_HISTORY_KEY = 'flist.searchHistory';
const SEARCH_HISTORY_MAX = 10;

// 隐藏文件展示偏好持久化：跨会话保留用户选择（true = 显示隐藏文件）。
const SHOW_HIDDEN_KEY = 'flist.showHidden';

function loadShowHidden(): boolean {
  try {
    return localStorage.getItem(SHOW_HIDDEN_KEY) === '1';
  } catch {
    return false;
  }
}

function saveShowHidden(v: boolean) {
  try {
    localStorage.setItem(SHOW_HIDDEN_KEY, v ? '1' : '0');
  } catch {
    // 忽略持久化失败（隐私模式 / 配额），内存态仍可用。
  }
}

function loadSearchHistory(): string[] {
  try {
    const raw = localStorage.getItem(SEARCH_HISTORY_KEY);
    if (!raw) return [];
    const arr = JSON.parse(raw);
    if (!Array.isArray(arr)) return [];
    return arr.filter((x): x is string => typeof x === 'string').slice(0, SEARCH_HISTORY_MAX);
  } catch {
    return [];
  }
}

// pushSearchHistory 将词去重置顶，截断到上限并持久化，返回新列表。
function pushSearchHistory(prev: string[], query: string): string[] {
  const q = query.trim();
  if (!q) return prev;
  const next = [q, ...prev.filter((x) => x !== q)].slice(0, SEARCH_HISTORY_MAX);
  try {
    localStorage.setItem(SEARCH_HISTORY_KEY, JSON.stringify(next));
  } catch {
    // 忽略持久化失败（隐私模式 / 配额），内存态仍可用。
  }
  return next;
}

export const useFsStore = create<FsState>((set, get) => ({
  currentPath: DEFAULT_PATH,
  entries: [],
  total: 0,
  loading: false,
  error: null,
  spaceVersion: 0,
  sort: 'name',
  order: 'asc',
  showHidden: loadShowHidden(),
  history: [DEFAULT_PATH],
  historyIndex: 0,
  selected: new Set<string>(),
  selectionAnchor: null,
  previewEntry: null,
  previewPath: null,

  searchOpen: false,
  searchQuery: '',
  searching: false,
  searchResults: [],
  searchTruncated: false,
  searchTimedOut: false,
  searchRecursive: false, // 默认仅搜索当前目录，递归由用户显式开启
  searchHistory: loadSearchHistory(),
  searchSelected: new Set<string>(),
  searchAnchor: null,

  clipboard: null,

  navigate: async (path, pushHistory = true) => {
    const { sort, order, showHidden } = get();
    // 切换到不同目录时立即清空旧 entries：避免「旧目录文件残留 + 加载卡顿」造成文件幻觉
    //（例如后退时旧的 2.txt 还在，加载完才消失，像凭空蒸发）。同目录 refresh 不清空，避免闪烁。
    const changingDir = path !== get().currentPath;
    // 进入任意目录都退出搜索态：搜索是覆盖在目录之上的临时视图，导航即应回到目录浏览。
    set({
      loading: true,
      error: null,
      ...(changingDir ? { entries: [], total: 0 } : {}),
      selected: new Set<string>(),
      selectionAnchor: null,
      searchOpen: false,
      searchQuery: '',
      searching: false,
      searchResults: [],
      searchTruncated: false,
      searchTimedOut: false,
      searchSelected: new Set<string>(),
      searchAnchor: null,
    });
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
          spaceVersion: state.spaceVersion + 1,
          history,
          historyIndex,
        };
      });
      useStore.getState().recordRecentAccess({ path: res.path, name: pathName(res.path), type: 'dir' });
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
    // 落点与当前目录相同时（如弹窗拦截物理前进/后退后的历史复位）：仅校正索引与 URL，
    // 跳过重复列目录，避免无谓的网络刷新与列表闪烁（大目录列举成本高）。
    if (path === get().currentPath) {
      set({ historyIndex: index });
      window.history.replaceState({ index, path }, '', pathToUrl(path));
      return;
    }
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
    const next = !get().showHidden;
    set({ showHidden: next });
    saveShowHidden(next);
    get().refresh();
  },

  // selectOne 单选：清空其余，仅选 name；置锚点。name 为 null 时清空。
  selectOne: (name) => {
    if (name === null) {
      set({ selected: new Set<string>(), selectionAnchor: null });
      return;
    }
    set({ selected: new Set([name]), selectionAnchor: name });
  },

  // toggleSelect 切换 name 的选中态（Ctrl/Cmd 单击）；置锚点。
  toggleSelect: (name) => {
    set((state) => {
      const next = new Set(state.selected);
      if (next.has(name)) {
        next.delete(name);
      } else {
        next.add(name);
      }
      return { selected: next, selectionAnchor: name };
    });
  },

  // rangeSelect 从锚点到 name（按当前 entries 顺序）的连续区间作为选择（Shift 单击）。
  // 无锚点时退化为单选。
  rangeSelect: (name) => {
    set((state) => {
      const names = state.entries.map((e) => e.name);
      const anchor = state.selectionAnchor ?? name;
      const from = names.indexOf(anchor);
      const to = names.indexOf(name);
      if (from === -1 || to === -1) {
        return { selected: new Set([name]), selectionAnchor: name };
      }
      const [lo, hi] = from <= to ? [from, to] : [to, from];
      return { selected: new Set(names.slice(lo, hi + 1)), selectionAnchor: anchor };
    });
  },

  // selectAll 选中当前目录全部条目（Ctrl/Cmd+A）。
  selectAll: () => {
    set((state) => ({ selected: new Set(state.entries.map((e) => e.name)) }));
  },

  // clearSelection 清空选择（Esc / 点击空白）。
  clearSelection: () => set({ selected: new Set<string>(), selectionAnchor: null }),

  // selectOneHit 搜索结果单选：清空其余，仅选 path；置锚点。path 为 null 时清空。
  selectOneHit: (path) => {
    if (path === null) {
      set({ searchSelected: new Set<string>(), searchAnchor: null });
      return;
    }
    set({ searchSelected: new Set([path]), searchAnchor: path });
  },

  // toggleHit 切换 path 的选中态（Ctrl/Cmd 单击）；置锚点。
  toggleHit: (path) => {
    set((state) => {
      const next = new Set(state.searchSelected);
      if (next.has(path)) {
        next.delete(path);
      } else {
        next.add(path);
      }
      return { searchSelected: next, searchAnchor: path };
    });
  },

  // rangeHit 从锚点到 path（按当前 searchResults 顺序）的连续区间作为选择（Shift 单击）。
  // 无锚点时退化为单选。
  rangeHit: (path) => {
    set((state) => {
      const paths = state.searchResults.map((h) => h.path);
      const anchor = state.searchAnchor ?? path;
      const from = paths.indexOf(anchor);
      const to = paths.indexOf(path);
      if (from === -1 || to === -1) {
        return { searchSelected: new Set([path]), searchAnchor: path };
      }
      const [lo, hi] = from <= to ? [from, to] : [to, from];
      return { searchSelected: new Set(paths.slice(lo, hi + 1)), searchAnchor: anchor };
    });
  },

  // selectAllHits 选中全部搜索结果（Ctrl/Cmd+A）。
  selectAllHits: () => {
    set((state) => ({ searchSelected: new Set(state.searchResults.map((h) => h.path)) }));
  },

  // clearHitSelection 清空搜索结果选择（Esc / 点击空白）。
  clearHitSelection: () => set({ searchSelected: new Set<string>(), searchAnchor: null }),

  openPreview: (entry, fullPath) => set({ previewEntry: entry, previewPath: fullPath }),
  closePreview: () => set({ previewEntry: null, previewPath: null }),

  mkdir: async (name) => {
    const target = joinPath(get().currentPath, name);
    try {
      await api.fs.mkdir(target);
      await get().refresh();
      return null;
    } catch (e) {
      handleError(e);
      return errMessage(e);
    }
  },

  touch: async (name) => {
    const target = joinPath(get().currentPath, name);
    try {
      await api.fs.touch(target);
      await get().refresh();
      return null;
    } catch (e) {
      handleError(e);
      return errMessage(e);
    }
  },

  rename: async (entry, newName) => {
    const from = joinPath(get().currentPath, entry.name);
    try {
      const res = await api.fs.rename(from, newName);
      if (res && !res.ok) {
        return opErrMessage(res.error);
      }
      await get().refresh();
      return null;
    } catch (e) {
      handleError(e);
      return errMessage(e);
    }
  },

  remove: async (entries) => {
    const paths = entries.map((e) => joinPath(get().currentPath, e.name));
    return get().removePaths(paths);
  },

  // removePaths 按完整路径批量删除。委托给异步文件操作服务，立即返回；
  // 进度走传输面板，完成事件里若当前目录受影响则刷新。
  removePaths: async (paths) => {
    if (paths.length === 0) return null;
    void useFileOpStore.getState().startDelete(paths);
    // 搜索态下清空已失效的搜索选择，避免操作期间残留高亮。
    if (get().searchOpen && get().searchQuery) {
      set({ searchSelected: new Set<string>(), searchAnchor: null });
      void get().runSearch(get().searchQuery);
    }
    return null;
  },

  runSearch: async (query) => {
    const q = query.trim();
    if (!q) {
      get().clearSearch();
      return;
    }
    set((state) => ({
      searchOpen: true,
      searchQuery: q,
      searching: true,
      searchHistory: pushSearchHistory(state.searchHistory, q),
    }));
    try {
      const res = await api.fs.search(get().currentPath, q, {
        recursive: get().searchRecursive,
        showHidden: get().showHidden,
      });
      set({
        searching: false,
        searchResults: res.items,
        searchTruncated: res.truncated,
        searchTimedOut: res.timedOut,
      });
    } catch (e) {
      handleError(e);
      set({ searching: false, searchResults: [], error: errMessage(e) });
    }
  },

  clearSearch: () =>
    set({
      searchOpen: false,
      searchQuery: '',
      searching: false,
      searchResults: [],
      searchTruncated: false,
      searchTimedOut: false,
      searchSelected: new Set<string>(),
      searchAnchor: null,
    }),

  // exitSearch 显式退出搜索：清空搜索态并清空旧目录快照后刷新当前目录，
  // 确保搜索期间被删/改的文件不以旧快照残留闪现（先 spinner 再呈现最新结果）。
  exitSearch: () => {
    get().clearSearch();
    set({ entries: [], total: 0, loading: true });
    void get().refresh();
  },

  // clearSearchHistory 清空最近搜索词记录（含 localStorage）。
  clearSearchHistory: () => {
    try {
      localStorage.removeItem(SEARCH_HISTORY_KEY);
    } catch {
      // 忽略持久化失败。
    }
    set({ searchHistory: [] });
  },

  // toggleSearchRecursive 翻转递归开关；若当前已有搜索结果则用新范围立即重搜。
  toggleSearchRecursive: () => {
    const next = !get().searchRecursive;
    set({ searchRecursive: next });
    const { searchOpen, searchQuery } = get();
    if (searchOpen && searchQuery) {
      void get().runSearch(searchQuery);
    }
  },

  copyToClipboard: (entries) => {
    if (entries.length === 0) return;
    const base = get().currentPath;
    get().copyPathsToClipboard(entries.map((e) => joinPath(base, e.name)));
  },

  cutToClipboard: (entries) => {
    if (entries.length === 0) return;
    const base = get().currentPath;
    get().cutPathsToClipboard(entries.map((e) => joinPath(base, e.name)));
  },

  // copyPathsToClipboard 按完整路径置复制态剪贴板（供搜索结果跨目录使用）。
  copyPathsToClipboard: (paths) => {
    if (paths.length === 0) return;
    set({ clipboard: { mode: 'copy', paths: [...paths] } });
  },

  // cutPathsToClipboard 按完整路径置剪切态剪贴板（供搜索结果跨目录使用）。
  cutPathsToClipboard: (paths) => {
    if (paths.length === 0) return;
    set({ clipboard: { mode: 'cut', paths: [...paths] } });
  },

  clearClipboard: () => set({ clipboard: null }),

  paste: async () => {
    const clip = get().clipboard;
    if (!clip || clip.paths.length === 0) return null;
    const dst = get().currentPath;
    // 委托给异步文件操作服务：复制走 copy、剪切走 move；均开 auto_rename 自动避让。
    // 任务进度通过传输面板展示，完成事件里按需刷新当前目录。立即返回不阻塞 UI。
    if (clip.mode === 'copy') {
      void useFileOpStore.getState().startCopy(clip.paths, dst, true);
    } else {
      void useFileOpStore.getState().startMove(clip.paths, dst, true);
      // 剪切粘贴已发起即清空剪贴板（复制保留，便于多次粘贴）。
      set({ clipboard: null });
    }
    return null;
  },
}));

// errMessage 提取异常的可读信息。
function errMessage(e: unknown): string {
  if (e instanceof ApiError) {
    return apiCodeMessage(e.code) ?? e.message;
  }
  return e instanceof Error ? e.message : '操作失败';
}

// apiCodeMessage 将常见业务错误码翻译为中文提示。
function apiCodeMessage(code: number): string | null {
  switch (code) {
    case 2001:
      return '路径不存在';
    case 2002:
      return '路径越界';
    case 2003:
      return '权限不足';
    case 2004:
      return '目标已存在';
    case 2005:
      return '磁盘空间不足';
    case 2006:
      return '名称非法';
    case 2008:
      return '目标不是目录';
    case 3001:
      return '该目录已收藏';
    case 3002:
      return '收藏不存在';
    default:
      return null;
  }
}

// opErrMessage 将批量结果里的错误码名翻译为中文提示。
function opErrMessage(name?: string): string {
  switch (name) {
    case 'file_exists':
      return '目标已存在';
    case 'disk_full':
      return '磁盘空间不足';
    case 'name_invalid':
      return '名称非法';
    case 'path_not_found':
      return '路径不存在';
    case 'permission_denied':
      return '权限不足';
    case 'path_traversal':
      return '路径越界';
    case 'not_a_dir':
      return '目标不是目录';
    case 'bad_request':
      return '非法操作';
    default:
      return '操作失败';
  }
}

// handleError 在遇到 401（会话失效）时登出回到登录页。
function handleError(e: unknown) {
  if (e instanceof ApiError && e.code === 1001) {
    useAuthStore.getState().logout();
  }
}

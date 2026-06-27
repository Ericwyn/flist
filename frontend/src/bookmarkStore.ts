import { create } from 'zustand';
import { api, ApiError } from './lib/api';
import { Bookmark } from './types';
import { useAuthStore } from './authStore';

interface BookmarkState {
  items: Bookmark[];
  loading: boolean;
  error: string | null;

  load: () => Promise<void>;
  // add 收藏一个目录；返回错误信息字符串，成功返回 null。
  add: (path: string, name?: string) => Promise<string | null>;
  rename: (id: number, name: string) => Promise<string | null>;
  remove: (id: number) => Promise<string | null>;
  // reorder 应用新的顺序（items 已是目标顺序），重排 sort_order 并持久化。
  reorder: (items: Bookmark[]) => Promise<void>;
  reset: () => void;
}

export const useBookmarkStore = create<BookmarkState>((set, get) => ({
  items: [],
  loading: false,
  error: null,

  load: async () => {
    set({ loading: true, error: null });
    try {
      const items = await api.bookmarks.list();
      set({ items, loading: false });
    } catch (e) {
      handleAuth(e);
      set({ loading: false, error: errMessage(e) });
    }
  },

  add: async (path, name) => {
    try {
      await api.bookmarks.create(path, name);
      await get().load();
      return null;
    } catch (e) {
      handleAuth(e);
      return errMessage(e);
    }
  },

  rename: async (id, name) => {
    try {
      await api.bookmarks.update(id, name);
      await get().load();
      return null;
    } catch (e) {
      handleAuth(e);
      return errMessage(e);
    }
  },

  remove: async (id) => {
    try {
      await api.bookmarks.remove(id);
      await get().load();
      return null;
    } catch (e) {
      handleAuth(e);
      return errMessage(e);
    }
  },

  reorder: async (items) => {
    // 乐观更新：先按新顺序展示，再持久化。
    set({ items });
    const orders = items.map((b, i) => ({ id: b.id, sortOrder: i + 1 }));
    try {
      await api.bookmarks.reorder(orders);
    } catch (e) {
      handleAuth(e);
      // 失败则重新拉取以回到服务端真实顺序。
      await get().load();
    }
  },

  reset: () => set({ items: [], loading: false, error: null }),
}));

function errMessage(e: unknown): string {
  if (e instanceof ApiError) {
    switch (e.code) {
      case 3001:
        return '该目录已收藏';
      case 3002:
        return '收藏不存在';
      case 2008:
        return '只能收藏目录';
      case 2001:
        return '路径不存在';
      default:
        return e.message;
    }
  }
  return e instanceof Error ? e.message : '操作失败';
}

function handleAuth(e: unknown) {
  if (e instanceof ApiError && e.code === 1001) {
    useAuthStore.getState().logout();
  }
}

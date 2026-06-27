import { create } from 'zustand';
import { api, getToken, setToken, clearToken, MeData } from './lib/api';

interface AuthState {
  user: MeData | null;
  status: 'loading' | 'authenticated' | 'unauthenticated';
  error: string | null;

  // 应用启动时调用：若本地有令牌则校验，决定初始状态。
  init: () => Promise<void>;
  login: (username: string, password: string) => Promise<void>;
  logout: () => Promise<void>;
  clearError: () => void;
}

export const useAuthStore = create<AuthState>((set) => ({
  user: null,
  status: 'loading',
  error: null,

  init: async () => {
    if (!getToken()) {
      set({ status: 'unauthenticated' });
      return;
    }
    try {
      const user = await api.me();
      set({ user, status: 'authenticated' });
    } catch {
      // 令牌失效或网络错误：清理并要求重新登录。
      clearToken();
      set({ user: null, status: 'unauthenticated' });
    }
  },

  login: async (username, password) => {
    set({ error: null });
    try {
      const data = await api.login(username, password);
      setToken(data.token);
      set({
        user: { id: 0, username: data.username },
        status: 'authenticated',
      });
    } catch (e) {
      const msg = e instanceof Error ? e.message : '登录失败';
      set({ error: msg });
      throw e;
    }
  },

  logout: async () => {
    try {
      await api.logout();
    } catch {
      // 即便后端登出失败也清理本地状态。
    }
    clearToken();
    set({ user: null, status: 'unauthenticated' });
  },

  clearError: () => set({ error: null }),
}));

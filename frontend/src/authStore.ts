import { create } from 'zustand';
import { api, getToken, setToken, clearToken, MeData } from './lib/api';

interface AuthState {
  user: MeData | null;
  status: 'loading' | 'authenticated' | 'unauthenticated';
  error: string | null;
  twoFactorRequired: boolean;
  tempToken: string | null;

  // 应用启动时调用：若本地有令牌则校验，决定初始状态。
  init: () => Promise<void>;
  login: (username: string, password: string) => Promise<void>;
  verifyTwoFactor: (code: string) => Promise<void>;
  logout: () => Promise<void>;
  setUser: (user: MeData) => void;
  clearError: () => void;
  backToLogin: () => void;
}

export const useAuthStore = create<AuthState>((set, get) => ({
  user: null,
  status: 'loading',
  error: null,
  twoFactorRequired: false,
  tempToken: null,

  init: async () => {
    if (!getToken()) {
      set({ status: 'unauthenticated' });
      return;
    }
    try {
      const user = await api.me();
      set({ user, status: 'authenticated' });
    } catch {
      clearToken();
      set({ user: null, status: 'unauthenticated' });
    }
  },

  login: async (username, password) => {
    set({ error: null });
    try {
      const data = await api.login(username, password);
      if (data.requires_two_factor && data.temp_token) {
        set({
          twoFactorRequired: true,
          tempToken: data.temp_token,
        });
        return;
      }
      if (data.token) {
        setToken(data.token);
        set({
          user: { id: 0, username: data.username ?? '' },
          status: 'authenticated',
          twoFactorRequired: false,
          tempToken: null,
        });
        return;
      }
      set({ error: '登录失败' });
    } catch (e) {
      const msg = e instanceof Error ? e.message : '登录失败';
      set({ error: msg });
      throw e;
    }
  },

  verifyTwoFactor: async (code) => {
    const tempToken = get().tempToken;
    if (!tempToken) {
      set({ error: '验证会话已过期，请重新登录' });
      return;
    }
    set({ error: null });
    try {
      const data = await api.verifyTwoFactor(tempToken, code);
      if (data.token) {
        setToken(data.token);
        set({
          user: { id: 0, username: data.username ?? '' },
          status: 'authenticated',
          twoFactorRequired: false,
          tempToken: null,
          error: null,
        });
      } else {
        set({ error: '验证失败' });
      }
    } catch (e) {
      const msg = e instanceof Error ? e.message : '验证失败';
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
    set({ user: null, status: 'unauthenticated', twoFactorRequired: false, tempToken: null });
  },

  setUser: (user) => set({ user }),

  clearError: () => set({ error: null }),

  backToLogin: () => set({ twoFactorRequired: false, tempToken: null, error: null }),
}));

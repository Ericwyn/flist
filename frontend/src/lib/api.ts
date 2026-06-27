// 统一响应信封，与后端 handler.Envelope 对齐。
export interface Envelope<T = unknown> {
  code: number;
  message: string;
  data: T;
}

export interface LoginData {
  token: string;
  expires_at: number;
  username: string;
}

export interface MeData {
  id: number;
  username: string;
}

const TOKEN_KEY = 'flist_token';

export function getToken(): string | null {
  return localStorage.getItem(TOKEN_KEY);
}

export function setToken(token: string): void {
  localStorage.setItem(TOKEN_KEY, token);
}

export function clearToken(): void {
  localStorage.removeItem(TOKEN_KEY);
}

// ApiError 携带业务错误码，便于上层按 code 区分处理。
export class ApiError extends Error {
  code: number;
  constructor(code: number, message: string) {
    super(message);
    this.code = code;
    this.name = 'ApiError';
  }
}

interface RequestOptions {
  method?: string;
  body?: unknown;
}

async function request<T>(path: string, opts: RequestOptions = {}): Promise<T> {
  const headers: Record<string, string> = {};
  const token = getToken();
  if (token) {
    headers['Authorization'] = `Bearer ${token}`;
  }
  let body: string | undefined;
  if (opts.body !== undefined) {
    headers['Content-Type'] = 'application/json';
    body = JSON.stringify(opts.body);
  }

  const resp = await fetch(path, {
    method: opts.method ?? 'GET',
    headers,
    body,
    credentials: 'same-origin',
  });

  let env: Envelope<T>;
  try {
    env = (await resp.json()) as Envelope<T>;
  } catch {
    throw new ApiError(resp.status, `请求失败 (HTTP ${resp.status})`);
  }

  if (env.code !== 0) {
    throw new ApiError(env.code, env.message || '请求失败');
  }
  return env.data;
}

export const api = {
  login(username: string, password: string): Promise<LoginData> {
    return request<LoginData>('/api/auth/login', {
      method: 'POST',
      body: { username, password },
    });
  },

  logout(): Promise<null> {
    return request<null>('/api/auth/logout', { method: 'POST' });
  },

  me(): Promise<MeData> {
    return request<MeData>('/api/auth/me');
  },

  changePassword(oldPassword: string, newPassword: string): Promise<null> {
    return request<null>('/api/auth/password', {
      method: 'PUT',
      body: { old_password: oldPassword, new_password: newPassword },
    });
  },
};

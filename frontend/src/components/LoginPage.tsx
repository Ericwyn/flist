import React, { useState } from 'react';
import { HardDrive, Loader2, Lock, User, ArrowLeft, Shield } from 'lucide-react';
import { useAuthStore } from '../authStore';

export function LoginPage() {
  const { login, error, clearError, twoFactorRequired, verifyTwoFactor, backToLogin } = useAuthStore();
  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');
  const [code, setCode] = useState('');
  const [submitting, setSubmitting] = useState(false);

  const handleLogin = async (e: React.FormEvent) => {
    e.preventDefault();
    if (submitting) return;
    setSubmitting(true);
    try {
      await login(username.trim(), password);
    } catch {
      // 错误已写入 store，由下方提示展示。
    } finally {
      setSubmitting(false);
    }
  };

  const handleVerify = async (e: React.FormEvent) => {
    e.preventDefault();
    if (submitting) return;
    setSubmitting(true);
    try {
      await verifyTwoFactor(code.trim());
    } catch {
      // 错误已写入 store。
    } finally {
      setSubmitting(false);
    }
  };

  const handleBack = () => {
    setCode('');
    backToLogin();
  };

  if (twoFactorRequired) {
    return (
      <div className="flex h-screen w-full items-center justify-center bg-[#f8fafc] dark:bg-slate-900 px-4">
        <div className="w-full max-w-sm">
          <div className="flex flex-col items-center mb-8">
            <div className="w-14 h-14 rounded-2xl bg-blue-600 flex items-center justify-center shadow-lg shadow-blue-600/20 mb-4">
              <Shield className="w-7 h-7 text-white" />
            </div>
            <h1 className="text-xl font-bold tracking-tight text-slate-900 dark:text-slate-100">
              两步验证
            </h1>
            <p className="text-sm text-slate-400 dark:text-slate-500 mt-1">
              请输入验证器 App 中的 6 位验证码
            </p>
          </div>

          <form
            onSubmit={handleVerify}
            className="bg-white dark:bg-slate-950 border border-slate-200 dark:border-slate-800 rounded-2xl shadow-sm p-6 space-y-4"
          >
            <div className="space-y-1.5">
              <label className="text-xs font-medium text-slate-600 dark:text-slate-400">
                验证码
              </label>
              <input
                type="text"
                inputMode="numeric"
                pattern="[0-9]*"
                maxLength={6}
                value={code}
                onChange={(e) => {
                  setCode(e.target.value.replace(/\D/g, ''));
                  if (error) clearError();
                }}
                autoFocus
                autoComplete="one-time-code"
                className="w-full px-3 py-2 text-center text-2xl tracking-[0.5em] font-mono rounded-lg border border-slate-200 dark:border-slate-700 bg-slate-50 dark:bg-slate-900 text-slate-900 dark:text-slate-100 outline-none focus:ring-2 focus:ring-blue-500/40 focus:border-blue-500 transition"
                placeholder="000000"
              />
            </div>

            {error && (
              <div className="text-xs text-rose-600 dark:text-rose-400 bg-rose-50 dark:bg-rose-900/20 border border-rose-100 dark:border-rose-900/40 rounded-lg px-3 py-2">
                {error}
              </div>
            )}

            <button
              type="submit"
              disabled={submitting || code.length !== 6}
              className="w-full flex items-center justify-center gap-2 py-2 text-sm font-medium bg-blue-600 text-white rounded-lg hover:bg-blue-700 disabled:opacity-50 disabled:cursor-not-allowed transition-colors shadow-sm"
            >
              {submitting && <Loader2 className="w-4 h-4 animate-spin" />}
              {submitting ? '验证中…' : '验证'}
            </button>

            <button
              type="button"
              onClick={handleBack}
              className="w-full flex items-center justify-center gap-1.5 py-1.5 text-xs text-slate-400 hover:text-slate-600 dark:hover:text-slate-300 transition-colors"
            >
              <ArrowLeft className="w-3.5 h-3.5" />
              返回重新登录
            </button>
          </form>
        </div>
      </div>
    );
  }

  return (
    <div className="flex h-screen w-full items-center justify-center bg-[#f8fafc] dark:bg-slate-900 px-4">
      <div className="w-full max-w-sm">
        <div className="flex flex-col items-center mb-8">
          <div className="w-14 h-14 rounded-2xl bg-blue-600 flex items-center justify-center shadow-lg shadow-blue-600/20 mb-4">
            <HardDrive className="w-7 h-7 text-white" />
          </div>
          <h1 className="text-xl font-bold tracking-tight text-slate-900 dark:text-slate-100">
            登录 Flist
          </h1>
          <p className="text-sm text-slate-400 dark:text-slate-500 mt-1">
            NAS 远程文件浏览器
          </p>
        </div>

        <form
          onSubmit={handleLogin}
          className="bg-white dark:bg-slate-950 border border-slate-200 dark:border-slate-800 rounded-2xl shadow-sm p-6 space-y-4"
        >
          <div className="space-y-1.5">
            <label className="text-xs font-medium text-slate-600 dark:text-slate-400">
              用户名
            </label>
            <div className="relative">
              <User className="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-slate-400" />
              <input
                type="text"
                value={username}
                onChange={(e) => {
                  setUsername(e.target.value);
                  if (error) clearError();
                }}
                autoFocus
                autoComplete="username"
                className="w-full pl-9 pr-3 py-2 text-sm rounded-lg border border-slate-200 dark:border-slate-700 bg-slate-50 dark:bg-slate-900 text-slate-900 dark:text-slate-100 outline-none focus:ring-2 focus:ring-blue-500/40 focus:border-blue-500 transition"
                placeholder="admin"
              />
            </div>
          </div>

          <div className="space-y-1.5">
            <label className="text-xs font-medium text-slate-600 dark:text-slate-400">
              密码
            </label>
            <div className="relative">
              <Lock className="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-slate-400" />
              <input
                type="password"
                value={password}
                onChange={(e) => {
                  setPassword(e.target.value);
                  if (error) clearError();
                }}
                autoComplete="current-password"
                className="w-full pl-9 pr-3 py-2 text-sm rounded-lg border border-slate-200 dark:border-slate-700 bg-slate-50 dark:bg-slate-900 text-slate-900 dark:text-slate-100 outline-none focus:ring-2 focus:ring-blue-500/40 focus:border-blue-500 transition"
                placeholder="••••••••"
              />
            </div>
          </div>

          {error && (
            <div className="text-xs text-rose-600 dark:text-rose-400 bg-rose-50 dark:bg-rose-900/20 border border-rose-100 dark:border-rose-900/40 rounded-lg px-3 py-2">
              {error}
            </div>
          )}

          <button
            type="submit"
            disabled={submitting || !username.trim() || !password}
            className="w-full flex items-center justify-center gap-2 py-2 text-sm font-medium bg-blue-600 text-white rounded-lg hover:bg-blue-700 disabled:opacity-50 disabled:cursor-not-allowed transition-colors shadow-sm"
          >
            {submitting && <Loader2 className="w-4 h-4 animate-spin" />}
            {submitting ? '登录中…' : '登录'}
          </button>
        </form>
      </div>
    </div>
  );
}

import React, { useState } from 'react';
import { Modal } from './Modal';
import { api, ApiError } from '../lib/api';
import { useAuthStore } from '../authStore';
import { useStore } from '../store';
import { cn } from '../lib/utils';
import { User, Lock, Palette, LogOut, Sun, Moon, Loader2, Check } from 'lucide-react';

interface SettingsModalProps {
  onClose: () => void;
}

type Status =
  | { kind: 'idle' }
  | { kind: 'saving' }
  | { kind: 'ok'; message: string }
  | { kind: 'error'; message: string };

const usernameErrors: Record<number, string> = {
  1005: '用户名格式不合法（3-32 位，仅限字母/数字/下划线/连字符，且不能以符号开头）',
  1006: '该用户名已被占用',
};

const passwordErrors: Record<number, string> = {
  1002: '当前密码不正确',
  1004: '新密码太弱（至少 8 位，且需同时包含字母和数字）',
};

function errMessage(e: unknown, map: Record<number, string>): string {
  if (e instanceof ApiError && map[e.code]) return map[e.code];
  if (e instanceof Error) return e.message;
  return '操作失败，请重试';
}

// SettingsModal 集中管理用户名、密码、主题与退出。
export function SettingsModal({ onClose }: SettingsModalProps) {
  const { user, setUser, logout } = useAuthStore();
  const { theme, toggleTheme } = useStore();

  return (
    <Modal isOpen={true} onClose={onClose} title="设置" maxWidth="md">
      <div className="space-y-6">
        <AccountSection
          currentName={user?.username ?? ''}
          onUpdated={(name) => setUser({ id: user?.id ?? 0, username: name })}
        />
        <PasswordSection />
        <AppearanceSection theme={theme} onToggle={toggleTheme} />
        <LogoutSection onLogout={() => logout()} />
      </div>
    </Modal>
  );
}

function SectionHeader({ icon, title }: { icon: React.ReactNode; title: string }) {
  return (
    <div className="flex items-center gap-2 mb-3">
      <span className="text-slate-400 dark:text-slate-500">{icon}</span>
      <h4 className="text-xs font-semibold uppercase tracking-wider text-slate-500 dark:text-slate-400">
        {title}
      </h4>
    </div>
  );
}

function StatusLine({ status }: { status: Status }) {
  if (status.kind === 'ok') {
    return (
      <p className="text-xs text-emerald-600 dark:text-emerald-400 flex items-center gap-1">
        <Check className="w-3.5 h-3.5" />
        {status.message}
      </p>
    );
  }
  if (status.kind === 'error') {
    return <p className="text-xs text-rose-500 dark:text-rose-400">{status.message}</p>;
  }
  return null;
}

const inputClass =
  'w-full px-3 py-2 text-sm rounded-lg border border-slate-200 dark:border-slate-700 bg-white dark:bg-slate-800 text-slate-800 dark:text-slate-100 placeholder:text-slate-400 focus:outline-none focus:ring-2 focus:ring-blue-500/40 focus:border-blue-400 transition-colors';

const btnPrimary =
  'shrink-0 whitespace-nowrap px-3 py-1.5 text-sm font-medium rounded-lg bg-blue-600 hover:bg-blue-700 text-white disabled:opacity-50 disabled:cursor-not-allowed transition-colors flex items-center gap-1.5';

function AccountSection({
  currentName,
  onUpdated,
}: {
  currentName: string;
  onUpdated: (name: string) => void;
}) {
  const [name, setName] = useState(currentName);
  const [status, setStatus] = useState<Status>({ kind: 'idle' });

  const dirty = name.trim() !== currentName && name.trim() !== '';

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!dirty || status.kind === 'saving') return;
    setStatus({ kind: 'saving' });
    try {
      const updated = await api.changeUsername(name.trim());
      onUpdated(updated.username);
      setName(updated.username);
      setStatus({ kind: 'ok', message: '用户名已更新' });
    } catch (e) {
      setStatus({ kind: 'error', message: errMessage(e, usernameErrors) });
    }
  };

  return (
    <section>
      <SectionHeader icon={<User className="w-4 h-4" />} title="账户" />
      <form onSubmit={submit} className="space-y-2">
        <label className="block text-xs text-slate-500 dark:text-slate-400">用户名</label>
        <div className="flex gap-2">
          <input
            className={inputClass}
            value={name}
            onChange={(e) => {
              setName(e.target.value);
              if (status.kind !== 'idle') setStatus({ kind: 'idle' });
            }}
            placeholder="用户名"
            autoComplete="username"
          />
          <button type="submit" className={btnPrimary} disabled={!dirty || status.kind === 'saving'}>
            {status.kind === 'saving' && <Loader2 className="w-3.5 h-3.5 animate-spin" />}
            保存
          </button>
        </div>
        <StatusLine status={status} />
      </form>
    </section>
  );
}

function PasswordSection() {
  const [oldPassword, setOldPassword] = useState('');
  const [newPassword, setNewPassword] = useState('');
  const [confirm, setConfirm] = useState('');
  const [status, setStatus] = useState<Status>({ kind: 'idle' });

  const reset = () => {
    setOldPassword('');
    setNewPassword('');
    setConfirm('');
  };

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (status.kind === 'saving') return;
    if (!oldPassword || !newPassword) {
      setStatus({ kind: 'error', message: '请填写当前密码与新密码' });
      return;
    }
    if (newPassword !== confirm) {
      setStatus({ kind: 'error', message: '两次输入的新密码不一致' });
      return;
    }
    setStatus({ kind: 'saving' });
    try {
      await api.changePassword(oldPassword, newPassword);
      reset();
      setStatus({ kind: 'ok', message: '密码已更新，其他设备的登录已失效' });
    } catch (e) {
      setStatus({ kind: 'error', message: errMessage(e, passwordErrors) });
    }
  };

  const clearStatus = () => {
    if (status.kind !== 'idle' && status.kind !== 'saving') setStatus({ kind: 'idle' });
  };

  return (
    <section>
      <SectionHeader icon={<Lock className="w-4 h-4" />} title="修改密码" />
      <form onSubmit={submit} className="space-y-2">
        <input
          type="password"
          className={inputClass}
          value={oldPassword}
          onChange={(e) => {
            setOldPassword(e.target.value);
            clearStatus();
          }}
          placeholder="当前密码"
          autoComplete="current-password"
        />
        <input
          type="password"
          className={inputClass}
          value={newPassword}
          onChange={(e) => {
            setNewPassword(e.target.value);
            clearStatus();
          }}
          placeholder="新密码（至少 8 位，含字母和数字）"
          autoComplete="new-password"
        />
        <input
          type="password"
          className={inputClass}
          value={confirm}
          onChange={(e) => {
            setConfirm(e.target.value);
            clearStatus();
          }}
          placeholder="确认新密码"
          autoComplete="new-password"
        />
        <div className="flex items-center justify-between pt-1">
          <StatusLine status={status} />
          <button
            type="submit"
            className={cn(btnPrimary, 'ml-auto')}
            disabled={status.kind === 'saving'}
          >
            {status.kind === 'saving' && <Loader2 className="w-3.5 h-3.5 animate-spin" />}
            更新密码
          </button>
        </div>
      </form>
    </section>
  );
}

function AppearanceSection({ theme, onToggle }: { theme: 'light' | 'dark'; onToggle: () => void }) {
  return (
    <section>
      <SectionHeader icon={<Palette className="w-4 h-4" />} title="外观" />
      <div className="flex items-center justify-between">
        <span className="text-sm text-slate-600 dark:text-slate-300">主题</span>
        <div className="bg-slate-200/70 dark:bg-slate-800 p-0.5 rounded-full flex items-center w-[84px] relative">
          <div
            className={cn(
              'absolute left-0.5 top-0.5 bottom-0.5 w-[calc(50%-2px)] bg-white dark:bg-slate-700 rounded-full shadow-sm transition-transform duration-300 ease-in-out',
              theme === 'dark' ? 'translate-x-full' : 'translate-x-0',
            )}
          />
          <button
            onClick={() => theme === 'dark' && onToggle()}
            className={cn(
              'flex-1 flex justify-center py-1 z-10 transition-colors',
              theme === 'light' ? 'text-amber-500' : 'text-slate-400 hover:text-slate-300',
            )}
            aria-label="浅色主题"
          >
            <Sun className="w-3.5 h-3.5" />
          </button>
          <button
            onClick={() => theme === 'light' && onToggle()}
            className={cn(
              'flex-1 flex justify-center py-1 z-10 transition-colors',
              theme === 'dark' ? 'text-blue-400' : 'text-slate-400 hover:text-slate-500',
            )}
            aria-label="深色主题"
          >
            <Moon className="w-3.5 h-3.5" />
          </button>
        </div>
      </div>
    </section>
  );
}

function LogoutSection({ onLogout }: { onLogout: () => void }) {
  return (
    <section className="pt-4 border-t border-slate-100 dark:border-slate-800">
      <button
        onClick={onLogout}
        className="w-full flex items-center justify-center gap-2 px-3 py-2 text-sm font-medium rounded-lg text-rose-600 dark:text-rose-400 bg-rose-50 dark:bg-rose-900/20 hover:bg-rose-100 dark:hover:bg-rose-900/30 transition-colors"
      >
        <LogOut className="w-4 h-4" />
        退出登录
      </button>
    </section>
  );
}

import React, { useState, useEffect } from 'react';
import { Modal } from './Modal';
import { api, ApiError } from '../lib/api';
import { useAuthStore } from '../authStore';
import { useStore } from '../store';
import { cn } from '../lib/utils';
import { User, Lock, Palette, LogOut, Sun, Moon, Loader2, Check, History, Shield } from 'lucide-react';

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

// 应用版本号（简单展示用，发版时手动同步）。
const APP_VERSION = 'v0.1.5';

// SettingsModal 集中管理用户名、密码、主题与退出。
export function SettingsModal({ onClose }: SettingsModalProps) {
  const { user, setUser, logout } = useAuthStore();
  const { theme, toggleTheme, recentEnabled, recentLimit, setRecentEnabled, setRecentLimit } = useStore();

  return (
    <Modal isOpen={true} onClose={onClose} title="设置" maxWidth="md">
      <div className="space-y-6">
        <AccountSection
          currentName={user?.username ?? ''}
          onUpdated={(name) => setUser({ id: user?.id ?? 0, username: name })}
        />
        <PasswordSection />
        <TwoFactorSection />
        <AppearanceSection theme={theme} onToggle={toggleTheme} />
        <RecentAccessSection
          enabled={recentEnabled}
          limit={recentLimit}
          onToggle={setRecentEnabled}
          onLimitChange={setRecentLimit}
        />
        <LogoutSection onLogout={() => logout()} />
      </div>
      <div className="text-[11px] text-slate-400 dark:text-slate-600 text-center select-none mt-2">
        flist {APP_VERSION}
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

function TwoFactorSection() {
  const [enabled, setEnabled] = useState(false);
  const [loading, setLoading] = useState(true);
  const [showSetup, setShowSetup] = useState(false);
  const [qrCode, setQrCode] = useState('');
  const [secret, setSecret] = useState('');
  const [code, setCode] = useState('');
  const [status, setStatus] = useState<Status>({ kind: 'idle' });

  const totpErrors: Record<number, string> = {
    1008: '验证码错误，请重试',
    1009: '2FA 已启用',
    1010: '2FA 未启用',
  };

  useEffect(() => {
    api.getTwoFactorStatus()
      .then((res) => setEnabled(res.enabled))
      .catch(() => {})
      .finally(() => setLoading(false));
  }, []);

  const handleSetup = async () => {
    setStatus({ kind: 'saving' });
    try {
      const res = await api.setupTwoFactor();
      setSecret(res.secret);
      setQrCode(res.qr_code);
      setShowSetup(true);
      setCode('');
      setStatus({ kind: 'idle' });
    } catch (e) {
      setStatus({ kind: 'error', message: errMessage(e, totpErrors) });
    }
  };

  const handleEnable = async (e: React.FormEvent) => {
    e.preventDefault();
    if (status.kind === 'saving') return;
    setStatus({ kind: 'saving' });
    try {
      await api.enableTwoFactor(code.trim());
      setEnabled(true);
      setShowSetup(false);
      setQrCode('');
      setSecret('');
      setCode('');
      setStatus({ kind: 'ok', message: '两步验证已开启' });
    } catch (e) {
      setStatus({ kind: 'error', message: errMessage(e, totpErrors) });
    }
  };

  const handleDisable = async (e: React.FormEvent) => {
    e.preventDefault();
    if (status.kind === 'saving') return;
    setStatus({ kind: 'saving' });
    try {
      await api.disableTwoFactor(code.trim());
      setEnabled(false);
      setCode('');
      setStatus({ kind: 'ok', message: '两步验证已关闭' });
    } catch (e) {
      setStatus({ kind: 'error', message: errMessage(e, totpErrors) });
    }
  };

  const clearStatus = () => {
    if (status.kind !== 'idle' && status.kind !== 'saving') setStatus({ kind: 'idle' });
  };

  return (
    <section>
      <SectionHeader icon={<Shield className="w-4 h-4" />} title="安全" />
      {loading ? (
        <div className="flex items-center gap-2 text-sm text-slate-400">
          <Loader2 className="w-3.5 h-3.5 animate-spin" />
          加载中…
        </div>
      ) : enabled ? (
        showSetup ? null : (
          <div className="space-y-3">
            <div className="flex items-center gap-2 text-sm text-emerald-600 dark:text-emerald-400">
              <Check className="w-4 h-4" />
              两步验证已开启
            </div>
            <form onSubmit={handleDisable} className="space-y-2">
              <p className="text-xs text-slate-500 dark:text-slate-400">
                关闭两步验证需输入当前验证码确认。
              </p>
              <input
                type="text"
                inputMode="numeric"
                pattern="[0-9]*"
                maxLength={6}
                value={code}
                onChange={(e) => {
                  setCode(e.target.value.replace(/\D/g, ''));
                  clearStatus();
                }}
                placeholder="6 位验证码"
                autoComplete="one-time-code"
                className={cn(inputClass, 'text-center tracking-[0.3em] font-mono')}
              />
              <div className="flex items-center justify-between">
                <StatusLine status={status} />
                <button
                  type="submit"
                  className={cn(btnPrimary, 'ml-auto bg-rose-600 hover:bg-rose-700')}
                  disabled={status.kind === 'saving' || code.length !== 6}
                >
                  {status.kind === 'saving' && <Loader2 className="w-3.5 h-3.5 animate-spin" />}
                  关闭 2FA
                </button>
              </div>
            </form>
          </div>
        )
      ) : showSetup ? (
        <div className="space-y-3">
          <div className="flex flex-col items-center gap-3">
            <img src={qrCode} alt="QR Code" className="w-48 h-48 rounded-lg border border-slate-200 dark:border-slate-700" />
            <p className="text-xs text-slate-500 dark:text-slate-400 text-center">
              使用 Google Authenticator、1Password 等验证器 App 扫描上方 QR 码
            </p>
            <p className="text-[11px] text-slate-400 dark:text-slate-500">
              无法扫码？手动输入密钥：<code className="font-mono text-slate-600 dark:text-slate-300 break-all">{secret}</code>
            </p>
          </div>
          <form onSubmit={handleEnable} className="space-y-2">
            <input
              type="text"
              inputMode="numeric"
              pattern="[0-9]*"
              maxLength={6}
              value={code}
              onChange={(e) => {
                setCode(e.target.value.replace(/\D/g, ''));
                clearStatus();
              }}
              placeholder="输入验证器中的 6 位验证码"
              autoComplete="one-time-code"
              className={cn(inputClass, 'text-center tracking-[0.3em] font-mono')}
            />
            <div className="flex items-center justify-between">
              <StatusLine status={status} />
              <div className="flex gap-2 ml-auto">
                <button
                  type="button"
                  onClick={() => { setShowSetup(false); setCode(''); setStatus({ kind: 'idle' }); }}
                  className="px-3 py-1.5 text-sm rounded-lg text-slate-500 hover:text-slate-700 dark:hover:text-slate-300 transition-colors"
                >
                  取消
                </button>
                <button
                  type="submit"
                  className={btnPrimary}
                  disabled={status.kind === 'saving' || code.length !== 6}
                >
                  {status.kind === 'saving' && <Loader2 className="w-3.5 h-3.5 animate-spin" />}
                  确认开启
                </button>
              </div>
            </div>
          </form>
        </div>
      ) : (
        <div className="space-y-2">
          <p className="text-sm text-slate-600 dark:text-slate-300">
            开启后，登录需在密码验证通过后额外输入 6 位验证码。
          </p>
          <button
            onClick={handleSetup}
            className={btnPrimary}
            disabled={status.kind === 'saving'}
          >
            {status.kind === 'saving' && <Loader2 className="w-3.5 h-3.5 animate-spin" />}
            开启 2FA
          </button>
          <StatusLine status={status} />
        </div>
      )}
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

function RecentAccessSection({
  enabled,
  limit,
  onToggle,
  onLimitChange,
}: {
  enabled: boolean;
  limit: number;
  onToggle: (enabled: boolean) => void;
  onLimitChange: (limit: number) => void;
}) {
  return (
    <section>
      <SectionHeader icon={<History className="w-4 h-4" />} title="最近访问" />
      <div className="space-y-3">
        <div className="flex items-center justify-between">
          <span className="text-sm text-slate-600 dark:text-slate-300">启用最近访问</span>
          <button
            onClick={() => onToggle(!enabled)}
            className={cn(
              'relative inline-flex h-5 w-9 items-center rounded-full transition-colors',
              enabled ? 'bg-blue-600' : 'bg-slate-300 dark:bg-slate-600',
            )}
            aria-label={enabled ? '关闭最近访问' : '开启最近访问'}
          >
            <span
              className={cn(
                'inline-block h-3.5 w-3.5 transform rounded-full bg-white shadow-sm transition-transform',
                enabled ? 'translate-x-[18px]' : 'translate-x-[3px]',
              )}
            />
          </button>
        </div>
        {enabled && (
          <div className="flex items-center justify-between">
            <span className="text-sm text-slate-600 dark:text-slate-300">保留数量</span>
            <div className="flex items-center gap-2">
              <input
                type="number"
                min={1}
                max={50}
                value={limit}
                onChange={(e) => {
                  const v = parseInt(e.target.value, 10);
                  if (Number.isFinite(v)) onLimitChange(v);
                }}
                className="w-16 px-2 py-1 text-sm rounded-lg border border-slate-200 dark:border-slate-700 bg-white dark:bg-slate-800 text-slate-800 dark:text-slate-100 text-center focus:outline-none focus:ring-2 focus:ring-blue-500/40 focus:border-blue-400 transition-colors"
              />
              <span className="text-xs text-slate-400 dark:text-slate-500">条（1–50）</span>
            </div>
          </div>
        )}
        <p className="text-[11px] text-slate-400 dark:text-slate-500">
          设置仅在当前浏览器生效（存储于 localStorage）。
        </p>
      </div>
    </section>
  );
}

function LogoutSection({ onLogout }: { onLogout: () => void }) {
  return (
    <section className="pt-2 border-slate-100 dark:border-slate-800">
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

import React, { useEffect, useState, useCallback } from 'react';
import { HardDrive, Usb, Loader2, RefreshCw, Unplug, FolderOpen, Lock } from 'lucide-react';
import { Modal } from './Modal';
import { api, ApiError } from '../lib/api';
import { Device } from '../types';
import { formatBytes, cn } from '../lib/utils';
import { useFsStore } from '../fsStore';

interface DeviceManagerProps {
  onClose: () => void;
}

// DeviceManager 列出块设备 / 分区，支持挂载 / 卸载 / 进入。仅在 deviceManagement 可用时展示入口。
export function DeviceManager({ onClose }: DeviceManagerProps) {
  const navigate = useFsStore((s) => s.navigate);
  const [devices, setDevices] = useState<Device[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  // 每个设备各自的操作中状态（挂载 / 卸载），按 device 路径索引。
  const [busy, setBusy] = useState<Record<string, boolean>>({});

  const load = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const res = await api.devices.list();
      if (!res.supported) {
        setError('当前系统不支持设备管理');
        setDevices([]);
      } else {
        setDevices(res.devices);
      }
    } catch (e) {
      setError(e instanceof Error ? e.message : '加载设备列表失败');
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    load();
  }, [load]);

  const setDeviceBusy = (dev: string, v: boolean) =>
    setBusy((prev) => ({ ...prev, [dev]: v }));

  const onMount = async (d: Device) => {
    setDeviceBusy(d.device, true);
    setError(null);
    try {
      const updated = await api.devices.mount(d.device);
      setDevices((prev) => prev.map((x) => (x.device === updated.device ? updated : x)));
    } catch (e) {
      setError(mountErrMsg(e));
    } finally {
      setDeviceBusy(d.device, false);
    }
  };

  const onUnmount = async (d: Device) => {
    setDeviceBusy(d.device, true);
    setError(null);
    try {
      const updated = await api.devices.unmount(d.device);
      setDevices((prev) => prev.map((x) => (x.device === updated.device ? updated : x)));
    } catch (e) {
      setError(mountErrMsg(e));
    } finally {
      setDeviceBusy(d.device, false);
    }
  };

  const onEnter = (d: Device) => {
    navigate(d.drivePath);
    onClose();
  };

  const footer = (
    <button
      onClick={load}
      disabled={loading}
      className="flex items-center gap-1.5 px-3 py-1.5 text-xs font-medium text-slate-600 dark:text-slate-300 hover:bg-slate-100 dark:hover:bg-slate-800 rounded-lg transition-colors disabled:opacity-50"
    >
      <RefreshCw className={cn('w-3.5 h-3.5', loading && 'animate-spin')} />
      刷新
    </button>
  );

  return (
    <Modal isOpen={true} onClose={onClose} title="设备管理" maxWidth="lg" footer={footer}>
      {loading && devices.length === 0 ? (
        <div className="flex items-center justify-center py-10 text-slate-400">
          <Loader2 className="w-5 h-5 animate-spin" />
        </div>
      ) : error && devices.length === 0 ? (
        <div className="py-8 text-center text-sm text-slate-500 dark:text-slate-400">{error}</div>
      ) : devices.length === 0 ? (
        <div className="py-8 text-center text-sm text-slate-400 dark:text-slate-500">
          未检测到可管理的块设备
        </div>
      ) : (
        <div className="space-y-2">
          {error && <p className="text-xs text-rose-500 pb-1">{error}</p>}
          {devices.map((d) => (
            <DeviceRow
              key={d.device}
              device={d}
              busy={!!busy[d.device]}
              onMount={() => onMount(d)}
              onUnmount={() => onUnmount(d)}
              onEnter={() => onEnter(d)}
            />
          ))}
        </div>
      )}
    </Modal>
  );
}

interface DeviceRowProps {
  device: Device;
  busy: boolean;
  onMount: () => void;
  onUnmount: () => void;
  onEnter: () => void;
}

function DeviceRow({ device: d, busy, onMount, onUnmount, onEnter }: DeviceRowProps) {
  const Icon = d.removable ? Usb : HardDrive;
  const title = d.label || d.name;
  return (
    <div className="flex items-center gap-3 p-2.5 rounded-lg border border-slate-200 dark:border-slate-800 bg-white dark:bg-slate-900/40">
      <div className="w-9 h-9 rounded-lg bg-slate-100 dark:bg-slate-800 flex items-center justify-center shrink-0">
        <Icon className="w-4.5 h-4.5 text-slate-500 dark:text-slate-400" />
      </div>

      <div className="min-w-0 flex-1">
        <div className="flex items-center gap-1.5">
          <span className="text-sm font-medium text-slate-800 dark:text-slate-100 truncate">{title}</span>
          {d.readonly && (
            <span title="只读">
              <Lock className="w-3 h-3 text-slate-400 shrink-0" />
            </span>
          )}
        </div>
        <div className="text-[11px] text-slate-400 dark:text-slate-500 truncate">
          {d.device}
          {d.fstype && <span className="ml-1.5">· {d.fstype}</span>}
          <span className="ml-1.5">· {formatBytes(d.size)}</span>
          {d.mounted && d.mountpoint && (
            <span className="ml-1.5 truncate" title={d.mountpoint}>· 已挂载于 {d.mountpoint}</span>
          )}
        </div>
      </div>

      <div className="flex items-center gap-1.5 shrink-0">
        {busy ? (
          <Loader2 className="w-4 h-4 animate-spin text-slate-400" />
        ) : d.mounted ? (
          <>
            <button
              onClick={onEnter}
              className="flex items-center gap-1 px-2 py-1 text-xs font-medium text-blue-600 dark:text-blue-400 hover:bg-blue-50 dark:hover:bg-blue-900/30 rounded-md transition-colors"
              title="进入设备目录"
            >
              <FolderOpen className="w-3.5 h-3.5" />
              进入
            </button>
            <button
              onClick={onUnmount}
              className="flex items-center gap-1 px-2 py-1 text-xs font-medium text-slate-600 dark:text-slate-300 hover:bg-slate-100 dark:hover:bg-slate-800 rounded-md transition-colors"
              title="卸载 / 弹出"
            >
              <Unplug className="w-3.5 h-3.5" />
              卸载
            </button>
          </>
        ) : (
          <button
            onClick={onMount}
            className="flex items-center gap-1 px-2 py-1 text-xs font-medium text-white bg-blue-600 hover:bg-blue-700 rounded-md transition-colors shadow-sm"
            title="挂载"
          >
            <HardDrive className="w-3.5 h-3.5" />
            挂载
          </button>
        )}
      </div>
    </div>
  );
}

// mountErrMsg 将设备操作错误码映射为可读中文提示。
function mountErrMsg(e: unknown): string {
  if (e instanceof ApiError) {
    switch (e.code) {
      case 3101:
        return '当前系统不支持设备管理';
      case 3102:
        return '设备不存在或已被移除';
      case 3103:
        return '设备正忙，请先关闭正在使用它的程序或目录';
      case 3104:
        return '无挂载权限，请检查服务器的 polkit / udisks 配置';
      case 3105:
        return '设备命令执行失败';
      default:
        return e.message || '操作失败';
    }
  }
  return e instanceof Error ? e.message : '操作失败';
}

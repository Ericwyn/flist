import React, { useEffect, useRef } from 'react';
import { createPortal } from 'react-dom';
import { cn } from '../lib/utils';

export interface MenuItem {
  label: string;
  icon?: React.ReactNode;
  onClick: () => void;
  disabled?: boolean;
  danger?: boolean;
}

interface ContextMenuProps {
  x: number;
  y: number;
  items: MenuItem[];
  onClose: () => void;
}

// ContextMenu 渲染一个定位到 (x,y) 的浮层菜单，点击外部或 Esc 关闭。
export function ContextMenu({ x, y, items, onClose }: ContextMenuProps) {
  const ref = useRef<HTMLDivElement>(null);

  useEffect(() => {
    const onDocClick = (e: MouseEvent) => {
      if (ref.current && !ref.current.contains(e.target as Node)) {
        onClose();
      }
    };
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose();
    };
    // 延迟一帧注册，避免触发本次右键的同一事件立刻关闭。
    const t = setTimeout(() => {
      document.addEventListener('mousedown', onDocClick);
      document.addEventListener('contextmenu', onDocClick);
      window.addEventListener('keydown', onKey);
      window.addEventListener('resize', onClose);
      window.addEventListener('blur', onClose);
    }, 0);
    return () => {
      clearTimeout(t);
      document.removeEventListener('mousedown', onDocClick);
      document.removeEventListener('contextmenu', onDocClick);
      window.removeEventListener('keydown', onKey);
      window.removeEventListener('resize', onClose);
      window.removeEventListener('blur', onClose);
    };
  }, [onClose]);

  // 防止菜单超出视口右/下边界。
  const style: React.CSSProperties = {
    left: Math.min(x, window.innerWidth - 180),
    top: Math.min(y, window.innerHeight - items.length * 36 - 8),
  };

  return createPortal(
    <div
      ref={ref}
      style={style}
      className="fixed z-[200] min-w-[160px] py-1 bg-white dark:bg-slate-900 border border-slate-200 dark:border-slate-700 rounded-lg shadow-lg"
      onContextMenu={(e) => e.preventDefault()}
    >
      {items.map((item, idx) => (
        <button
          key={idx}
          disabled={item.disabled}
          onClick={() => {
            if (item.disabled) return;
            item.onClick();
            onClose();
          }}
          className={cn(
            'w-full flex items-center gap-2.5 px-3 py-1.5 text-sm text-left transition-colors',
            item.disabled
              ? 'text-slate-300 dark:text-slate-600 cursor-not-allowed'
              : item.danger
                ? 'text-rose-600 dark:text-rose-400 hover:bg-rose-50 dark:hover:bg-rose-900/20'
                : 'text-slate-700 dark:text-slate-200 hover:bg-slate-100 dark:hover:bg-slate-800',
          )}
        >
          {item.icon && <span className="w-4 h-4 shrink-0 flex items-center justify-center">{item.icon}</span>}
          <span className="truncate">{item.label}</span>
        </button>
      ))}
    </div>,
    document.body,
  );
}

import React, { PointerEvent as ReactPointerEvent, useCallback, useEffect, useRef, useState } from 'react';
import {
  ImageOff,
  Info,
  Loader2,
  Maximize2,
  RotateCw,
  Scan,
  ZoomIn,
  ZoomOut,
  X,
} from 'lucide-react';
import { api } from '../lib/api';
import { formatBytes } from '../lib/utils';
import { ImageMetadata } from '../types';

const MIN_SCALE = 0.02;
const MAX_SCALE = 8;
const ZOOM_FACTOR = 1.2;

type Point = { x: number; y: number };
type ViewMode = 'fit' | 'actual' | 'custom';

interface ImagePreviewProps {
  path: string;
  url: string;
  name: string;
}

export function ImagePreview({ path, url, name }: ImagePreviewProps) {
  const viewportRef = useRef<HTMLDivElement>(null);
  const dragRef = useRef<{ pointerId: number; start: Point; pan: Point } | null>(null);
  const [naturalSize, setNaturalSize] = useState({ width: 0, height: 0 });
  const [scale, setScale] = useState(1);
  const [pan, setPan] = useState<Point>({ x: 0, y: 0 });
  const [rotation, setRotation] = useState(0);
  const [mode, setMode] = useState<ViewMode>('fit');
  const [dragging, setDragging] = useState(false);
  const [imageLoading, setImageLoading] = useState(true);
  const [imageError, setImageError] = useState(false);
  const [detailsOpen, setDetailsOpen] = useState(false);
  const [metadata, setMetadata] = useState<ImageMetadata | null>(null);
  const [metadataLoading, setMetadataLoading] = useState(true);
  const [metadataError, setMetadataError] = useState<string | null>(null);

  const calculateFit = useCallback((width = naturalSize.width, height = naturalSize.height, angle = rotation) => {
    const viewport = viewportRef.current;
    if (!viewport || !width || !height) return 1;
    const quarterTurn = angle % 180 !== 0;
    const displayWidth = quarterTurn ? height : width;
    const displayHeight = quarterTurn ? width : height;
    const availableWidth = Math.max(viewport.clientWidth - 48, 1);
    const availableHeight = Math.max(viewport.clientHeight - 72, 1);
    return Math.min(1, availableWidth / displayWidth, availableHeight / displayHeight);
  }, [naturalSize.height, naturalSize.width, rotation]);

  const fitToViewport = useCallback((angle = rotation) => {
    const next = calculateFit(naturalSize.width, naturalSize.height, angle);
    setScale(next);
    setPan({ x: 0, y: 0 });
    setMode('fit');
  }, [calculateFit, naturalSize.height, naturalSize.width, rotation]);

  const showActualSize = useCallback(() => {
    setScale(1);
    setPan({ x: 0, y: 0 });
    setMode('actual');
  }, []);

  const zoomTo = useCallback((requested: number, anchor?: Point) => {
    const next = Math.min(MAX_SCALE, Math.max(MIN_SCALE, requested));
    if (Math.abs(next - scale) < 0.0001) return;
    if (anchor && viewportRef.current) {
      const rect = viewportRef.current.getBoundingClientRect();
      const relative = { x: anchor.x - rect.left - rect.width / 2, y: anchor.y - rect.top - rect.height / 2 };
      const ratio = next / scale;
      setPan((current) => ({
        x: relative.x - (relative.x - current.x) * ratio,
        y: relative.y - (relative.y - current.y) * ratio,
      }));
    }
    setScale(next);
    setMode(Math.abs(next - 1) < 0.001 ? 'actual' : 'custom');
  }, [scale]);

  const rotate = useCallback(() => {
    const next = (rotation + 90) % 360;
    setRotation(next);
    const nextFit = calculateFit(naturalSize.width, naturalSize.height, next);
    if (mode === 'fit') setScale(nextFit);
    setPan({ x: 0, y: 0 });
  }, [calculateFit, mode, naturalSize.height, naturalSize.width, rotation]);

  useEffect(() => {
    if (!detailsOpen || metadata || metadataError) return;
    let cancelled = false;
    setMetadataLoading(true);
    setMetadataError(null);
    api.fs.imageMetadata(path)
      .then((result) => {
        if (!cancelled) setMetadata(result);
      })
      .catch((error) => {
        if (!cancelled) setMetadataError(error instanceof Error ? error.message : '图片信息加载失败');
      })
      .finally(() => {
        if (!cancelled) setMetadataLoading(false);
      });
    return () => { cancelled = true; };
  }, [detailsOpen, metadata, metadataError, path]);

  useEffect(() => {
    const viewport = viewportRef.current;
    if (!viewport || typeof ResizeObserver === 'undefined') return;
    const observer = new ResizeObserver(() => {
      const next = calculateFit();
      if (mode === 'fit') setScale(next);
    });
    observer.observe(viewport);
    return () => observer.disconnect();
  }, [calculateFit, mode]);

  // React 的 wheel 监听在部分浏览器中会被注册为 passive；原生非 passive 监听确保
  // 滚轮始终用于图片缩放，而不会把外层弹窗一起滚动。
  useEffect(() => {
    const viewport = viewportRef.current;
    if (!viewport) return;
    const handleWheel = (event: WheelEvent) => {
      event.preventDefault();
      const intensity = event.deltaMode === 1 ? 0.08 : 0.002;
      zoomTo(scale * Math.exp(-event.deltaY * intensity), { x: event.clientX, y: event.clientY });
    };
    viewport.addEventListener('wheel', handleWheel, { passive: false });
    return () => viewport.removeEventListener('wheel', handleWheel);
  }, [scale, zoomTo]);

  useEffect(() => {
    const onKeyDown = (event: KeyboardEvent) => {
      const target = event.target;
      if (target instanceof HTMLElement && target.closest('input, textarea, select, [contenteditable="true"]')) return;
      if (event.key === '+' || event.key === '=') {
        event.preventDefault();
        zoomTo(scale * ZOOM_FACTOR);
      } else if (event.key === '-') {
        event.preventDefault();
        zoomTo(scale / ZOOM_FACTOR);
      } else if (event.key === '0') {
        event.preventDefault();
        fitToViewport();
      } else if (event.key === '1') {
        event.preventDefault();
        showActualSize();
      }
    };
    window.addEventListener('keydown', onKeyDown);
    return () => window.removeEventListener('keydown', onKeyDown);
  }, [fitToViewport, scale, showActualSize, zoomTo]);

  const onImageLoad = (event: React.SyntheticEvent<HTMLImageElement>) => {
    const nextSize = { width: event.currentTarget.naturalWidth, height: event.currentTarget.naturalHeight };
    setNaturalSize(nextSize);
    setImageLoading(false);
    const nextFit = calculateFit(nextSize.width, nextSize.height, rotation);
    setScale(nextFit);
    setPan({ x: 0, y: 0 });
    setMode('fit');
  };

  const onPointerDown = (event: ReactPointerEvent<HTMLDivElement>) => {
    if (event.button !== 0 || (event.target as HTMLElement).closest('button, aside')) return;
    event.currentTarget.setPointerCapture(event.pointerId);
    dragRef.current = {
      pointerId: event.pointerId,
      start: { x: event.clientX, y: event.clientY },
      pan,
    };
    setDragging(true);
  };

  const onPointerMove = (event: ReactPointerEvent<HTMLDivElement>) => {
    const drag = dragRef.current;
    if (!drag || drag.pointerId !== event.pointerId) return;
    setPan({
      x: drag.pan.x + event.clientX - drag.start.x,
      y: drag.pan.y + event.clientY - drag.start.y,
    });
  };

  const stopDragging = (event: ReactPointerEvent<HTMLDivElement>) => {
    if (dragRef.current?.pointerId !== event.pointerId) return;
    dragRef.current = null;
    setDragging(false);
    if (event.currentTarget.hasPointerCapture(event.pointerId)) event.currentTarget.releasePointerCapture(event.pointerId);
  };

  const zoomPercent = Math.round(scale * 100);

  return (
    <div className="relative flex h-[72vh] min-h-[240px] w-full overflow-hidden bg-[#111419] text-white sm:min-h-[420px]">
      <div
        ref={viewportRef}
        className={`relative min-w-0 flex-1 select-none overflow-hidden touch-none bg-[radial-gradient(circle_at_center,_#252a32_0,_#16191e_46%,_#101216_100%)] ${dragging ? 'cursor-grabbing' : 'cursor-grab'}`}
        onPointerDown={onPointerDown}
        onPointerMove={onPointerMove}
        onPointerUp={stopDragging}
        onPointerCancel={stopDragging}
        onDoubleClick={() => mode === 'actual' ? fitToViewport() : showActualSize()}
        aria-label="图片查看区域，可滚轮缩放并拖动图片"
      >
        <div
          className="pointer-events-none absolute left-1/2 top-1/2 will-change-transform"
          style={{ transform: `translate3d(${pan.x}px, ${pan.y}px, 0)` }}
        >
          <img
            src={url}
            alt={name}
            draggable={false}
            onLoad={onImageLoad}
            onError={() => { setImageLoading(false); setImageError(true); }}
            className="block max-w-none shadow-[0_22px_70px_rgba(0,0,0,0.42)] will-change-transform"
            style={{
              transform: `translate(-50%, -50%) rotate(${rotation}deg) scale(${scale})`,
              transformOrigin: 'center center',
            }}
          />
        </div>

        {imageLoading && (
          <div className="absolute inset-0 flex items-center justify-center text-slate-400">
            <Loader2 className="h-6 w-6 animate-spin" />
          </div>
        )}
        {imageError && (
          <div className="absolute inset-0 flex flex-col items-center justify-center gap-3 text-slate-400">
            <ImageOff className="h-10 w-10 opacity-70" />
            <span className="text-sm">图片加载失败</span>
          </div>
        )}

        <div className="absolute left-4 top-4 z-30 rounded-full border border-white/10 bg-black/45 px-2.5 py-1 font-mono text-[11px] tracking-wide text-white/75 shadow-lg backdrop-blur-md">
          {zoomPercent}%{rotation ? ` · ${rotation}°` : ''}
        </div>

        <div className="absolute bottom-4 left-1/2 z-30 flex -translate-x-1/2 items-center gap-0.5 rounded-xl border border-white/10 bg-black/65 p-1.5 shadow-2xl backdrop-blur-xl">
          <ViewerButton label="缩小（-）" onClick={() => zoomTo(scale / ZOOM_FACTOR)} disabled={scale <= MIN_SCALE}>
            <ZoomOut />
          </ViewerButton>
          <button
            type="button"
            onClick={showActualSize}
            className="h-8 min-w-12 rounded-lg px-2 font-mono text-[11px] text-white/80 transition-colors hover:bg-white/12 hover:text-white focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-blue-400"
            title="按当前缩放比例切换到 1:1（快捷键 1）"
          >
            {zoomPercent}%
          </button>
          <ViewerButton label="放大（+）" onClick={() => zoomTo(scale * ZOOM_FACTOR)} disabled={scale >= MAX_SCALE}>
            <ZoomIn />
          </ViewerButton>
          <span className="mx-1 h-5 w-px bg-white/15" />
          <ViewerButton label="适应窗口（0）" onClick={() => fitToViewport()} active={mode === 'fit'}>
            <Maximize2 />
          </ViewerButton>
          <ViewerButton label="原始大小 1:1（1）" onClick={showActualSize} active={mode === 'actual'}>
            <Scan />
          </ViewerButton>
          <ViewerButton label="顺时针旋转 90°" onClick={rotate}>
            <RotateCw />
          </ViewerButton>
          <span className="mx-1 h-5 w-px bg-white/15" />
          <ViewerButton label="图片信息" onClick={() => setDetailsOpen((open) => !open)} active={detailsOpen}>
            <Info />
          </ViewerButton>
        </div>

        <div className="pointer-events-none absolute bottom-4 right-4 hidden rounded-md bg-black/30 px-2 py-1 text-[10px] text-white/40 backdrop-blur-sm lg:block">
          滚轮缩放 · 拖动浏览 · 双击切换 1:1
        </div>
      </div>

      {detailsOpen && (
        <MetadataPanel
          metadata={metadata}
          loading={metadataLoading}
          error={metadataError}
          fallbackSize={naturalSize}
          onClose={() => setDetailsOpen(false)}
        />
      )}
    </div>
  );
}

function ViewerButton({
  label,
  onClick,
  children,
  active = false,
  disabled = false,
}: {
  label: string;
  onClick: () => void;
  children: React.ReactNode;
  active?: boolean;
  disabled?: boolean;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      disabled={disabled}
      aria-label={label}
      aria-pressed={active || undefined}
      title={label}
      className={`flex h-8 w-8 items-center justify-center rounded-lg transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-blue-400 disabled:cursor-not-allowed disabled:opacity-30 [&_svg]:h-4 [&_svg]:w-4 ${active ? 'bg-white text-slate-950' : 'text-white/75 hover:bg-white/12 hover:text-white'}`}
    >
      {children}
    </button>
  );
}

function MetadataPanel({
  metadata,
  loading,
  error,
  fallbackSize,
  onClose,
}: {
  metadata: ImageMetadata | null;
  loading: boolean;
  error: string | null;
  fallbackSize: { width: number; height: number };
  onClose: () => void;
}) {
  const width = metadata?.width || fallbackSize.width;
  const height = metadata?.height || fallbackSize.height;
  return (
    <aside className="absolute inset-y-0 right-0 z-40 flex w-[min(20rem,88vw)] shrink-0 flex-col border-l border-white/10 bg-[#171a20]/95 shadow-[-18px_0_50px_rgba(0,0,0,0.3)] backdrop-blur-xl md:relative md:w-72 md:bg-[#171a20] md:shadow-none">
      <div className="flex h-12 shrink-0 items-center justify-between border-b border-white/8 px-4">
        <div>
          <h4 className="text-xs font-semibold tracking-wide text-white">图片信息</h4>
          <p className="mt-0.5 text-[9px] uppercase tracking-[0.18em] text-white/35">Original metadata</p>
        </div>
        <button type="button" onClick={onClose} className="rounded-md p-1 text-white/45 hover:bg-white/10 hover:text-white" aria-label="关闭图片信息">
          <X className="h-4 w-4" />
        </button>
      </div>

      <div className="flex-1 overflow-y-auto px-4 py-4">
        {loading && (
          <div className="flex items-center gap-2 py-6 text-xs text-white/45">
            <Loader2 className="h-4 w-4 animate-spin" /> 正在读取原始信息…
          </div>
        )}
        {!loading && error && <p className="rounded-lg bg-rose-500/10 p-3 text-xs leading-5 text-rose-300">{error}</p>}
        {!loading && metadata && (
          <>
            <MetadataSection title="文件">
              <MetadataRow label="尺寸" value={width && height ? `${width} × ${height} px` : '未知'} />
              <MetadataRow label="格式" value={metadata.format || metadata.mime || '未知'} />
              <MetadataRow label="文件大小" value={formatBytes(metadata.size)} />
              {metadata.colorModel && <MetadataRow label="颜色模型" value={metadata.colorModel} />}
              <MetadataRow label="修改时间" value={formatMetadataDate(metadata.modTime)} />
            </MetadataSection>

            <MetadataSection title="EXIF / 拍摄信息">
              {metadata.exif.length > 0
                ? metadata.exif.map((field) => <MetadataRow key={`${field.key}-${field.value}`} label={field.label} value={field.value} />)
                : <p className="py-2 text-[11px] leading-5 text-white/35">这张图片没有可读取的 EXIF 拍摄信息。</p>}
            </MetadataSection>
          </>
        )}
      </div>
    </aside>
  );
}

function MetadataSection({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <section className="mb-6 last:mb-0">
      <h5 className="mb-2 text-[10px] font-semibold uppercase tracking-[0.16em] text-white/35">{title}</h5>
      <dl className="divide-y divide-white/[0.06]">{children}</dl>
    </section>
  );
}

function MetadataRow({ label, value }: { label: string; value: string }) {
  return (
    <div className="grid grid-cols-[5.25rem_minmax(0,1fr)] gap-2 py-2 text-[11px] leading-[1.45]">
      <dt className="text-white/42">{label}</dt>
      <dd className="break-words text-right text-white/82" title={value}>{value}</dd>
    </div>
  );
}

function formatMetadataDate(value: string): string {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value || '未知';
  return new Intl.DateTimeFormat('zh-CN', {
    year: 'numeric', month: '2-digit', day: '2-digit',
    hour: '2-digit', minute: '2-digit', second: '2-digit',
  }).format(date);
}

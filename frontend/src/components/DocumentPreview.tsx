import React, { useEffect, useRef, useState } from 'react';
import {
  AlertTriangle,
  FileDown,
  FileSpreadsheet,
  Loader2,
  Presentation,
  RefreshCw,
  ScrollText,
  ShieldCheck,
  FileCode2,
} from 'lucide-react';
import type { WorkBook, WorkSheet } from 'xlsx';
import { formatBytes } from '../lib/utils';
import { DOCUMENT_PREVIEW_MAX_BYTES, useDocumentPreview } from '../lib/useDocumentPreview';
import { MarkdownPreview } from './MarkdownPreview';

export type RichDocumentKind = 'markdown' | 'csv' | 'spreadsheet' | 'document' | 'presentation';

const MAX_GRID_CELLS = 50_000;
const MAX_GRID_COLUMNS = 120;
const MAX_GRID_ROWS = 2_000;

export function DocumentPreview({
  kind,
  path,
  name,
  size,
  downloadUrl,
  compact = false,
}: {
  kind: RichDocumentKind;
  path: string;
  name: string;
  size: number;
  downloadUrl: string;
  compact?: boolean;
}) {
  const loaded = useDocumentPreview(path, size);
  const descriptor = documentDescriptor(kind);
  const DescriptorIcon = descriptor.icon;

  return (
    <div className={`flex ${compact ? 'h-[63vh]' : 'h-[68vh]'} min-h-[280px] flex-col bg-slate-100 dark:bg-[#11151b]`}>
      <div className="flex h-11 shrink-0 items-center justify-between border-b border-slate-200 bg-white px-4 dark:border-white/8 dark:bg-[#191e26]">
        <div className="flex min-w-0 items-center gap-2.5">
          <DescriptorIcon className={`h-4 w-4 shrink-0 ${descriptor.color}`} />
          <span className="truncate text-xs font-semibold text-slate-700 dark:text-slate-200">{descriptor.label}</span>
          <span className="hidden rounded-full bg-emerald-50 px-2 py-0.5 text-[9px] font-semibold uppercase tracking-wider text-emerald-700 dark:bg-emerald-500/10 dark:text-emerald-400 sm:inline-flex">
            <ShieldCheck className="mr-1 h-3 w-3" />仅本地处理
          </span>
        </div>
        <span className="shrink-0 font-mono text-[10px] text-slate-400">{formatBytes(size)} · 上限 20 MiB</span>
      </div>

      <div className="relative min-h-0 flex-1">
        {loaded.status === 'loading' && <DocumentLoading loaded={loaded.loaded} total={loaded.total} label={descriptor.loadingLabel} />}
        {loaded.status === 'too-large' && <PreviewTooLarge size={size} downloadUrl={downloadUrl} name={name} />}
        {loaded.status === 'error' && <PreviewError message={loaded.message} onRetry={loaded.retry} downloadUrl={downloadUrl} name={name} />}
        {loaded.status === 'ready' && kind === 'markdown' && <MarkdownPreview content={decodeText(loaded.data)} path={path} />}
        {loaded.status === 'ready' && kind === 'csv' && <CsvRenderer data={loaded.data} />}
        {loaded.status === 'ready' && kind === 'spreadsheet' && <SpreadsheetRenderer data={loaded.data} />}
        {loaded.status === 'ready' && kind === 'document' && <DocxRenderer data={loaded.data} />}
        {loaded.status === 'ready' && kind === 'presentation' && <PptxRenderer data={loaded.data} />}
      </div>
    </div>
  );
}

function DocumentLoading({ loaded, total, label }: { loaded: number; total: number; label: string }) {
  const percent = total > 0 ? Math.min(100, Math.round((loaded / total) * 100)) : 0;
  return (
    <div className="absolute inset-0 flex items-center justify-center overflow-hidden bg-slate-50 dark:bg-[#12161c]">
      <div className="absolute inset-0 opacity-[0.035] [background-image:linear-gradient(to_right,currentColor_1px,transparent_1px),linear-gradient(to_bottom,currentColor_1px,transparent_1px)] [background-size:32px_32px]" />
      <div className="relative w-72 text-center">
        <div className="mx-auto mb-5 flex h-14 w-14 items-center justify-center rounded-2xl border border-blue-100 bg-white shadow-lg shadow-blue-950/5 dark:border-white/8 dark:bg-white/5">
          <Loader2 className="h-6 w-6 animate-spin text-blue-600 dark:text-sky-400" />
        </div>
        <p className="text-sm font-semibold text-slate-700 dark:text-slate-200">{label}</p>
        <p className="mt-1.5 text-[11px] text-slate-400">
          {loaded > 0 ? `${formatBytes(loaded)} / ${formatBytes(total || loaded)}` : '正在建立安全的本地预览…'}
        </p>
        <div className="mt-4 h-1 overflow-hidden rounded-full bg-slate-200 dark:bg-white/8">
          <div className={`h-full rounded-full bg-blue-500 transition-[width] duration-200 ${total <= 0 ? 'w-1/3 animate-indeterminate' : ''}`} style={total > 0 ? { width: `${percent}%` } : undefined} />
        </div>
        <p className="mt-3 text-[10px] text-slate-400">文件只下载到当前浏览器，不会发送到第三方服务</p>
      </div>
    </div>
  );
}

function PreviewTooLarge({ size, downloadUrl, name }: { size: number; downloadUrl: string; name: string }) {
  return (
    <StateMessage
      icon={<AlertTriangle className="h-6 w-6 text-amber-500" />}
      title="文件太大，未载入预览"
      detail={`当前文件 ${formatBytes(size)}（${size.toLocaleString('zh-CN')} 字节），浏览器端预览上限为 ${formatBytes(DOCUMENT_PREVIEW_MAX_BYTES)}。`}
      action={<DownloadAction downloadUrl={downloadUrl} name={name} />}
    />
  );
}

function PreviewError({ message, onRetry, downloadUrl, name }: { message: string; onRetry: () => void; downloadUrl: string; name: string }) {
  return (
    <StateMessage
      icon={<AlertTriangle className="h-6 w-6 text-rose-500" />}
      title="无法生成在线预览"
      detail={message}
      action={(
        <div className="flex items-center gap-2">
          <button type="button" onClick={onRetry} className="inline-flex items-center gap-1.5 rounded-lg bg-blue-600 px-3 py-2 text-xs font-medium text-white hover:bg-blue-700">
            <RefreshCw className="h-3.5 w-3.5" />重试
          </button>
          <DownloadAction downloadUrl={downloadUrl} name={name} />
        </div>
      )}
    />
  );
}

function StateMessage({ icon, title, detail, action }: { icon: React.ReactNode; title: string; detail: string; action: React.ReactNode }) {
  return (
    <div className="absolute inset-0 flex items-center justify-center bg-slate-50 px-6 dark:bg-[#12161c]">
      <div className="max-w-md rounded-2xl border border-slate-200 bg-white p-7 text-center shadow-sm dark:border-white/8 dark:bg-white/[0.035]">
        <div className="mx-auto mb-4 flex h-12 w-12 items-center justify-center rounded-xl bg-slate-100 dark:bg-white/6">{icon}</div>
        <h4 className="text-sm font-semibold text-slate-800 dark:text-slate-100">{title}</h4>
        <p className="mt-2 text-xs leading-5 text-slate-500 dark:text-slate-400">{detail}</p>
        <div className="mt-5 flex justify-center">{action}</div>
      </div>
    </div>
  );
}

function DownloadAction({ downloadUrl, name }: { downloadUrl: string; name: string }) {
  return (
    <a href={downloadUrl} download={name} className="inline-flex items-center gap-1.5 rounded-lg bg-slate-100 px-3 py-2 text-xs font-medium text-slate-700 hover:bg-slate-200 dark:bg-white/8 dark:text-slate-200 dark:hover:bg-white/12">
      <FileDown className="h-3.5 w-3.5" />下载文件
    </a>
  );
}

function CsvRenderer({ data }: { data: ArrayBuffer }) {
  const [state, setState] = useState<{ rows: string[][]; delimiter: string; truncated: boolean } | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    const parse = async () => {
      try {
        const Papa = (await import('papaparse')).default;
        const text = decodeText(data);
        const result = Papa.parse<string[]>(text, {
          skipEmptyLines: 'greedy',
          preview: MAX_GRID_ROWS + 1,
        });
        if (cancelled) return;
        if (result.errors.length > 0 && result.data.length === 0) throw new Error(result.errors[0].message);
        const rawRows = result.data.map((row) => row.map((cell) => String(cell ?? '')));
        const columnCount = Math.min(MAX_GRID_COLUMNS, Math.max(1, ...rawRows.map((row) => row.length)));
        const rowLimit = Math.max(1, Math.min(MAX_GRID_ROWS, Math.floor(MAX_GRID_CELLS / columnCount)));
        setState({
          rows: rawRows.slice(0, rowLimit).map((row) => row.slice(0, columnCount)),
          delimiter: result.meta.delimiter || ',',
          truncated: rawRows.length > rowLimit || rawRows.some((row) => row.length > columnCount),
        });
      } catch (e) {
        if (!cancelled) setError(e instanceof Error ? e.message : 'CSV 解析失败');
      }
    };
    void parse();
    return () => { cancelled = true; };
  }, [data]);

  if (error) return <InlineRenderError message={error} />;
  if (!state) return <RenderLoading label="正在解析 CSV 行列…" />;
  return <DataGrid rows={state.rows} note={`分隔符 ${visibleDelimiter(state.delimiter)}${state.truncated ? ' · 数据较多，仅展示前 50,000 个单元格' : ''}`} />;
}

function SpreadsheetRenderer({ data }: { data: ArrayBuffer }) {
  const workbookRef = useRef<WorkBook | null>(null);
  const xlsxRef = useRef<typeof import('xlsx') | null>(null);
  const [sheetNames, setSheetNames] = useState<string[]>([]);
  const [activeSheet, setActiveSheet] = useState('');
  const [rows, setRows] = useState<string[][] | null>(null);
  const [note, setNote] = useState('');
  const [error, setError] = useState<string | null>(null);

  const renderSheet = (name: string) => {
    const XLSX = xlsxRef.current;
    const workbook = workbookRef.current;
    if (!XLSX || !workbook) return;
    setActiveSheet(name);
    setRows(null);
    queueMicrotask(() => {
      try {
        const sheet = workbook.Sheets[name];
        const sourceRange = sheet?.['!ref'] ? XLSX.utils.decode_range(sheet['!ref']) : { s: { r: 0, c: 0 }, e: { r: 0, c: 0 } };
        const totalRows = sourceRange.e.r - sourceRange.s.r + 1;
        const totalColumns = sourceRange.e.c - sourceRange.s.c + 1;
        const columns = Math.max(1, Math.min(MAX_GRID_COLUMNS, totalColumns));
        const rowLimit = Math.max(1, Math.min(MAX_GRID_ROWS, Math.floor(MAX_GRID_CELLS / columns)));
        const range = {
          s: sourceRange.s,
          e: {
            r: Math.min(sourceRange.e.r, sourceRange.s.r + rowLimit - 1),
            c: Math.min(sourceRange.e.c, sourceRange.s.c + columns - 1),
          },
        };
        setRows(readSheetRows(XLSX, sheet, range));
        setNote(totalRows > rowLimit || totalColumns > columns
          ? `${totalRows.toLocaleString()} 行 × ${totalColumns.toLocaleString()} 列 · 为保证流畅，仅展示前 ${formatBytesAsCount(rowLimit * columns)} 个单元格`
          : `${totalRows.toLocaleString()} 行 × ${totalColumns.toLocaleString()} 列`);
      } catch (e) {
        setError(e instanceof Error ? e.message : '工作表渲染失败');
      }
    });
  };

  useEffect(() => {
    let cancelled = false;
    const parse = async () => {
      try {
        const XLSX = await import('xlsx');
        const workbook = XLSX.read(data, { type: 'array', cellDates: true, cellStyles: false });
        if (cancelled) return;
        xlsxRef.current = XLSX;
        workbookRef.current = workbook;
        setSheetNames(workbook.SheetNames);
        if (workbook.SheetNames.length === 0) throw new Error('工作簿中没有可预览的工作表');
        renderSheetWith(XLSX, workbook, workbook.SheetNames[0], setActiveSheet, setRows, setNote);
      } catch (e) {
        if (!cancelled) setError(e instanceof Error ? e.message : 'Excel 解析失败');
      }
    };
    void parse();
    return () => {
      cancelled = true;
      workbookRef.current = null;
      xlsxRef.current = null;
    };
  }, [data]);

  if (error) return <InlineRenderError message={error} />;
  if (sheetNames.length === 0 || !rows) return <RenderLoading label="正在解析工作簿与单元格…" />;
  return (
    <div className="flex h-full min-h-0 flex-col bg-white dark:bg-[#151a21]">
      <DataGrid rows={rows} note={note} />
      <div className="flex h-10 shrink-0 items-end gap-0.5 overflow-x-auto border-t border-slate-200 bg-slate-100 px-2 dark:border-white/8 dark:bg-[#11151b]">
        {sheetNames.map((sheet) => (
          <button
            key={sheet}
            type="button"
            onClick={() => renderSheet(sheet)}
            className={`h-8 max-w-48 shrink-0 truncate rounded-t-md border-x border-t px-3 text-[11px] transition-colors ${sheet === activeSheet ? 'border-slate-200 bg-white font-semibold text-emerald-700 dark:border-white/8 dark:bg-[#1c222b] dark:text-emerald-400' : 'border-transparent text-slate-500 hover:bg-white/60 dark:text-slate-400 dark:hover:bg-white/5'}`}
            title={sheet}
          >
            {sheet}
          </button>
        ))}
      </div>
    </div>
  );
}

function DocxRenderer({ data }: { data: ArrayBuffer }) {
  const hostRef = useRef<HTMLDivElement>(null);
  const [status, setStatus] = useState<'loading' | 'ready'>('loading');
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    const host = hostRef.current;
    if (!host) return;
    let cancelled = false;
    host.replaceChildren();
    const render = async () => {
      try {
        const { renderAsync } = await import('docx-preview');
        if (cancelled) return;
        await renderAsync(data.slice(0), host, host, {
          className: 'flist-docx',
          inWrapper: true,
          breakPages: true,
          renderHeaders: true,
          renderFooters: true,
          renderFootnotes: true,
          renderEndnotes: true,
          renderComments: true,
          renderChanges: true,
          renderAltChunks: false,
          useBase64URL: true,
          ignoreLastRenderedPageBreak: false,
        });
        if (!cancelled) setStatus('ready');
      } catch (e) {
        if (!cancelled) setError(e instanceof Error ? e.message : 'Word 文档渲染失败');
      }
    };
    void render();
    return () => {
      cancelled = true;
      host.replaceChildren();
    };
  }, [data]);

  return (
    <div className="relative h-full overflow-auto bg-[#d9dde3] dark:bg-[#0f1318]">
      {status === 'loading' && !error && <RenderLoading label="正在排版 Word 页面…" overlay />}
      {error && <InlineRenderError message={error} />}
      <div ref={hostRef} className={`flist-docx-preview min-h-full py-6 ${status === 'ready' && !error ? 'block' : 'invisible'}`} />
    </div>
  );
}

function PptxRenderer({ data }: { data: ArrayBuffer }) {
  const hostRef = useRef<HTMLDivElement>(null);
  const [status, setStatus] = useState<'loading' | 'ready'>('loading');
  const [slideCount, setSlideCount] = useState(0);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    const host = hostRef.current;
    if (!host) return;
    const controller = new AbortController();
    let viewer: { destroy: () => void; slideCount: number } | null = null;
    let cancelled = false;
    host.replaceChildren();
    const render = async () => {
      try {
        const { PptxViewer, RECOMMENDED_ZIP_LIMITS } = await import('@aiden0z/pptx-renderer/browser');
        if (cancelled) return;
        viewer = await PptxViewer.open(data.slice(0), host, {
          renderMode: 'list',
          fitMode: 'contain',
          scrollContainer: host,
          zipLimits: RECOMMENDED_ZIP_LIMITS,
          lazyMedia: true,
          lazySlides: true,
          pdfjs: false,
          signal: controller.signal,
          listOptions: { windowed: true, initialSlides: 4, batchSize: 6, overscanViewport: 1.25, showSlideLabels: true },
        });
        if (cancelled) {
          viewer.destroy();
          return;
        }
        setSlideCount(viewer.slideCount);
        setStatus('ready');
      } catch (e) {
        if (!cancelled && !controller.signal.aborted) setError(e instanceof Error ? e.message : 'PPTX 渲染失败');
      }
    };
    void render();
    return () => {
      cancelled = true;
      controller.abort();
      viewer?.destroy();
      host.replaceChildren();
    };
  }, [data]);

  return (
    <div className="relative h-full bg-[#1a1d23]">
      {status === 'loading' && !error && <RenderLoading label="正在解析并绘制幻灯片…" overlay dark />}
      {error && <InlineRenderError message={error} dark />}
      {status === 'ready' && <span className="pointer-events-none absolute right-4 top-3 z-20 rounded-full bg-black/55 px-2.5 py-1 font-mono text-[10px] text-white/70 backdrop-blur">{slideCount} 张幻灯片</span>}
      <div ref={hostRef} className={`flist-pptx-preview h-full overflow-x-hidden overflow-y-auto px-4 py-6 ${status === 'ready' && !error ? 'block' : 'invisible'}`} />
    </div>
  );
}

function DataGrid({ rows, note }: { rows: string[][]; note: string }) {
  const columnCount = Math.max(1, ...rows.map((row) => row.length));
  return (
    <div className="flex h-full min-h-0 flex-1 flex-col bg-white dark:bg-[#151a21]">
      <div className="min-h-0 flex-1 overflow-auto">
        <table className="border-separate border-spacing-0 font-mono text-[11px] text-slate-700 dark:text-slate-300">
          <thead className="sticky top-0 z-20">
            <tr>
              <th className="sticky left-0 z-30 h-7 min-w-11 border-b border-r border-slate-300 bg-slate-100 dark:border-white/10 dark:bg-[#222831]" />
              {Array.from({ length: columnCount }, (_, index) => (
                <th key={index} className="h-7 min-w-28 border-b border-r border-slate-300 bg-slate-100 px-2 text-center font-semibold text-slate-500 dark:border-white/10 dark:bg-[#222831] dark:text-slate-400">
                  {columnLabel(index)}
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            {rows.map((row, rowIndex) => (
              <tr key={rowIndex} className="group">
                <th className="sticky left-0 z-10 h-7 border-b border-r border-slate-200 bg-slate-100 px-2 text-right font-normal text-slate-400 group-hover:bg-blue-50 dark:border-white/8 dark:bg-[#222831] dark:group-hover:bg-sky-500/10">{rowIndex + 1}</th>
                {Array.from({ length: columnCount }, (_, columnIndex) => (
                  <td key={columnIndex} className={`h-7 max-w-80 border-b border-r border-slate-200 px-2 whitespace-nowrap group-hover:bg-blue-50/40 dark:border-white/[0.06] dark:group-hover:bg-sky-500/[0.06] ${rowIndex === 0 ? 'bg-slate-50 font-semibold text-slate-700 dark:bg-[#1c222b] dark:text-slate-200' : 'bg-white dark:bg-[#171c23]'}`} title={row[columnIndex] ?? ''}>
                    <span className="block min-w-24 max-w-72 overflow-hidden text-ellipsis">{row[columnIndex] ?? ''}</span>
                  </td>
                ))}
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      <div className="h-7 shrink-0 border-t border-slate-200 bg-slate-50 px-3 py-1 text-[10px] text-slate-400 dark:border-white/8 dark:bg-[#11151b]">{note}</div>
    </div>
  );
}

function RenderLoading({ label, overlay = false, dark = false }: { label: string; overlay?: boolean; dark?: boolean }) {
  return (
    <div className={`${overlay ? 'absolute inset-0 z-20' : 'h-full'} flex items-center justify-center ${dark ? 'bg-[#15191f] text-slate-300' : 'bg-slate-50 text-slate-500 dark:bg-[#131820] dark:text-slate-300'}`}>
      <div className="flex items-center gap-2.5 text-xs"><Loader2 className="h-4 w-4 animate-spin text-blue-500" />{label}</div>
    </div>
  );
}

function InlineRenderError({ message, dark = false }: { message: string; dark?: boolean }) {
  return (
    <div className={`absolute inset-0 z-30 flex items-center justify-center px-6 ${dark ? 'bg-[#15191f]' : 'bg-slate-50 dark:bg-[#131820]'}`}>
      <div className="max-w-md text-center"><AlertTriangle className="mx-auto mb-3 h-7 w-7 text-rose-500" /><p className="text-sm font-medium text-slate-700 dark:text-slate-200">文档解析失败</p><p className="mt-2 text-xs leading-5 text-slate-500 dark:text-slate-400">{message}</p></div>
    </div>
  );
}

function renderSheetWith(
  XLSX: typeof import('xlsx'),
  workbook: WorkBook,
  name: string,
  setActiveSheet: (name: string) => void,
  setRows: (rows: string[][]) => void,
  setNote: (note: string) => void,
) {
  const sheet = workbook.Sheets[name];
  const sourceRange = sheet?.['!ref'] ? XLSX.utils.decode_range(sheet['!ref']) : { s: { r: 0, c: 0 }, e: { r: 0, c: 0 } };
  const totalRows = sourceRange.e.r - sourceRange.s.r + 1;
  const totalColumns = sourceRange.e.c - sourceRange.s.c + 1;
  const columns = Math.max(1, Math.min(MAX_GRID_COLUMNS, totalColumns));
  const rowLimit = Math.max(1, Math.min(MAX_GRID_ROWS, Math.floor(MAX_GRID_CELLS / columns)));
  const range = { s: sourceRange.s, e: { r: Math.min(sourceRange.e.r, sourceRange.s.r + rowLimit - 1), c: Math.min(sourceRange.e.c, sourceRange.s.c + columns - 1) } };
  setActiveSheet(name);
  setRows(readSheetRows(XLSX, sheet, range));
  setNote(totalRows > rowLimit || totalColumns > columns
    ? `${totalRows.toLocaleString()} 行 × ${totalColumns.toLocaleString()} 列 · 为保证流畅，仅展示前 ${formatBytesAsCount(rowLimit * columns)} 个单元格`
    : `${totalRows.toLocaleString()} 行 × ${totalColumns.toLocaleString()} 列`);
}

function documentDescriptor(kind: RichDocumentKind) {
  switch (kind) {
    case 'markdown': return { label: 'Markdown 阅读视图', loadingLabel: '正在载入 Markdown 文档…', icon: FileCode2, color: 'text-sky-500' };
    case 'csv': return { label: 'CSV 数据预览', loadingLabel: '正在载入 CSV 数据…', icon: FileSpreadsheet, color: 'text-emerald-500' };
    case 'spreadsheet': return { label: 'Excel 工作簿预览', loadingLabel: '正在载入工作簿…', icon: FileSpreadsheet, color: 'text-emerald-500' };
    case 'document': return { label: 'Word 文档预览', loadingLabel: '正在载入 Word 文档…', icon: ScrollText, color: 'text-blue-500' };
    case 'presentation': return { label: 'PowerPoint 演示文稿', loadingLabel: '正在载入演示文稿…', icon: Presentation, color: 'text-orange-500' };
  }
}

function decodeText(data: ArrayBuffer): string {
  try {
    return new TextDecoder('utf-8', { fatal: true }).decode(data);
  } catch {
    try {
      return new TextDecoder('gb18030').decode(data);
    } catch {
      return new TextDecoder().decode(data);
    }
  }
}

function visibleDelimiter(value: string): string {
  if (value === '\t') return 'Tab';
  if (value === ';') return '分号';
  if (value === '|') return '竖线';
  return `“${value}”`;
}

function formatCell(value: unknown): string {
  if (value == null) return '';
  if (value instanceof Date) return new Intl.DateTimeFormat('zh-CN', { dateStyle: 'medium', timeStyle: 'short' }).format(value);
  return String(value);
}

function readSheetRows(XLSX: typeof import('xlsx'), sheet: WorkSheet, range: { s: { r: number; c: number }; e: { r: number; c: number } }): string[][] {
  const rows: string[][] = [];
  for (let row = range.s.r; row <= range.e.r; row++) {
    const values: string[] = [];
    for (let column = range.s.c; column <= range.e.c; column++) {
      const cell = sheet[XLSX.utils.encode_cell({ r: row, c: column })];
      if (!cell) {
        values.push('');
      } else if (cell.w != null) {
        values.push(String(cell.w));
      } else if (cell.f && cell.v == null) {
        values.push(`=${cell.f}`);
      } else {
        values.push(formatCell(cell.v));
      }
    }
    rows.push(values);
  }
  return rows;
}

function columnLabel(index: number): string {
  let label = '';
  for (let value = index + 1; value > 0; value = Math.floor((value - 1) / 26)) {
    label = String.fromCharCode(65 + ((value - 1) % 26)) + label;
  }
  return label;
}

function formatBytesAsCount(value: number): string {
  return value.toLocaleString('zh-CN');
}

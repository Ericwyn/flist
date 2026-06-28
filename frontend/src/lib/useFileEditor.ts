import { useCallback, useEffect, useRef, useState } from 'react';
import type { RefObject } from 'react';
import { EditorState, Compartment } from '@codemirror/state';
import {
  EditorView,
  keymap,
  lineNumbers,
  highlightActiveLine,
  highlightActiveLineGutter,
} from '@codemirror/view';
import { defaultKeymap, history, historyKeymap, indentWithTab } from '@codemirror/commands';
import { syntaxHighlighting, defaultHighlightStyle, indentOnInput, bracketMatching } from '@codemirror/language';
import { oneDark } from '@codemirror/theme-one-dark';
import { api, ApiError } from './api';
import { useStore } from '../store';
import { FileContent, SaveConflict } from '../types';
import { languageForName } from './editorLang';

// SaveAsResult 是「另存为」一次尝试的结果：ok 成功；exists 表示目标已存在需用户确认覆盖；error 为其他失败原因。
export interface SaveAsResult {
  ok: boolean;
  exists?: boolean;
  error?: string;
}

// UseFileEditor 封装文本编辑器的完整生命周期：加载内容、挂载 CodeMirror、脏标记、
// 乐观锁保存、冲突处理、恢复、另存为。整页编辑器（Editor.tsx）与预览模态框共用同一套逻辑。
export interface UseFileEditor {
  meta: FileContent | null;
  loading: boolean;
  loadError: string | null;
  // loadErrorCode 透传加载失败的业务错误码（如 2013 非文本 / 2014 过大），供调用方做降级展示。
  loadErrorCode: number | null;
  dirty: boolean;
  saving: boolean;
  saveError: string | null;
  conflict: SaveConflict | null;
  savedAt: number | null;
  notEditable: boolean;
  hostRef: RefObject<HTMLDivElement>;
  getContent: () => string;
  doSave: (force?: boolean) => Promise<void>;
  reloadRemote: () => Promise<void>;
  restore: () => void;
  saveAs: (newPath: string, overwrite?: boolean) => Promise<SaveAsResult>;
  dismissConflict: () => void;
  copyContentToClipboard: () => Promise<void>;
}

// useFileEditor 接收目标文件 API 路径，返回编辑器状态与操作。host 元素由调用方渲染并挂载 hostRef。
export function useFileEditor(path: string): UseFileEditor {
  const theme = useStore((s) => s.theme);
  const [meta, setMeta] = useState<FileContent | null>(null);
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [loadErrorCode, setLoadErrorCode] = useState<number | null>(null);
  const [dirty, setDirty] = useState(false);
  const [saving, setSaving] = useState(false);
  const [saveError, setSaveError] = useState<string | null>(null);
  const [conflict, setConflict] = useState<SaveConflict | null>(null);
  const [savedAt, setSavedAt] = useState<number | null>(null);

  const hostRef = useRef<HTMLDivElement>(null);
  const viewRef = useRef<EditorView | null>(null);
  const themeCompartment = useRef(new Compartment());
  // revisionRef 持有当前保存基准的 revision token，保存成功后更新（避免闭包旧值）。
  const revisionRef = useRef<string>('');
  // doSaveRef 让编辑器内 Mod-s 快捷键始终调用最新的保存实现。
  const doSaveRef = useRef<(force?: boolean) => void>(() => {});

  const getContent = useCallback(() => viewRef.current?.state.doc.toString() ?? '', []);

  // 加载文件内容。
  useEffect(() => {
    if (!path) {
      setLoadError('缺少 path 参数');
      setLoading(false);
      return;
    }
    let cancelled = false;
    setLoading(true);
    setLoadError(null);
    setLoadErrorCode(null);
    setDirty(false);
    setSaveError(null);
    setConflict(null);
    setSavedAt(null);
    api.fs
      .content(path)
      .then((res) => {
        if (cancelled) return;
        setMeta(res);
        revisionRef.current = res.revision.token;
      })
      .catch((e: unknown) => {
        if (cancelled) return;
        setLoadError(editLoadError(e));
        setLoadErrorCode(e instanceof ApiError ? e.code : null);
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [path]);

  // meta 就绪后创建 CodeMirror 实例（仅文本可编辑时）。meta 变化（如重新加载远端）会重建视图。
  useEffect(() => {
    if (!meta || !hostRef.current || !meta.editable) return;
    const dark = theme === 'dark';

    const state = EditorState.create({
      doc: meta.content,
      extensions: [
        lineNumbers(),
        highlightActiveLineGutter(),
        highlightActiveLine(),
        history(),
        indentOnInput(),
        bracketMatching(),
        syntaxHighlighting(defaultHighlightStyle, { fallback: true }),
        languageForName(meta.name),
        keymap.of([
          { key: 'Mod-s', preventDefault: true, run: () => (doSaveRef.current(false), true) },
          ...defaultKeymap,
          ...historyKeymap,
          indentWithTab,
        ]),
        EditorView.lineWrapping,
        themeCompartment.current.of(dark ? oneDark : []),
        EditorView.updateListener.of((u) => {
          if (u.docChanged) setDirty(true);
        }),
      ],
    });
    const view = new EditorView({ state, parent: hostRef.current });
    viewRef.current = view;
    view.focus();
    return () => {
      view.destroy();
      viewRef.current = null;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [meta]);

  // 主题切换时热更新编辑器配色。
  useEffect(() => {
    const view = viewRef.current;
    if (!view) return;
    view.dispatch({
      effects: themeCompartment.current.reconfigure(theme === 'dark' ? oneDark : []),
    });
  }, [theme]);

  // doSave 保存当前内容；force=true 时绕过乐观锁强制覆盖。
  const doSave = useCallback(
    async (force = false) => {
      if (!meta || saving) return;
      setSaving(true);
      setSaveError(null);
      try {
        const res = await api.fs.saveContent({
          path: meta.path,
          content: getContent(),
          expectedRevision: revisionRef.current,
          encoding: meta.encoding,
          lineEnding: meta.lineEnding,
          force,
        });
        revisionRef.current = res.revision.token;
        setDirty(false);
        setConflict(null);
        setSavedAt(Date.now());
      } catch (e: unknown) {
        if (e instanceof ApiError && e.code === 2012) {
          setConflict(
            extractConflict(e as ApiError & { data?: SaveConflict }) ?? {
              path: meta.path,
              currentModTime: '',
              currentRevision: { token: '', weak: false },
            },
          );
        } else {
          setSaveError(e instanceof Error ? e.message : '保存失败');
        }
      } finally {
        setSaving(false);
      }
    },
    [meta, saving, getContent],
  );

  doSaveRef.current = (force?: boolean) => void doSave(force);

  // reloadRemote 丢弃本地修改、重新加载远端内容（meta 变化触发视图重建）。
  const reloadRemote = useCallback(async () => {
    if (!meta) return;
    try {
      const res = await api.fs.content(meta.path);
      revisionRef.current = res.revision.token;
      setMeta(res);
      setDirty(false);
      setConflict(null);
      setSaveError(null);
    } catch (e: unknown) {
      setSaveError(e instanceof Error ? e.message : '重新加载失败');
    }
  }, [meta]);

  // restore 把编辑器内容恢复为打开时的初始内容（不发请求，仅重置本地编辑）。
  const restore = useCallback(() => {
    const view = viewRef.current;
    if (!view || !meta) return;
    view.dispatch({
      changes: { from: 0, to: view.state.doc.length, insert: meta.content },
    });
    setDirty(false);
    setSaveError(null);
  }, [meta]);

  // saveAs 把当前内容另存到 newPath。新文件先 touch 再 force 写入；
  // 目标已存在且未确认覆盖时返回 { exists: true } 交由调用方二次确认。
  const saveAs = useCallback(
    async (newPath: string, overwrite = false): Promise<SaveAsResult> => {
      if (!meta) return { ok: false, error: '内容未就绪' };
      const content = getContent();
      try {
        if (!overwrite) {
          try {
            await api.fs.touch(newPath);
          } catch (e: unknown) {
            if (e instanceof ApiError && e.code === 2004) {
              return { ok: false, exists: true };
            }
            throw e;
          }
        }
        await api.fs.saveContent({
          path: newPath,
          content,
          encoding: meta.encoding,
          lineEnding: meta.lineEnding,
          force: true,
        });
        return { ok: true };
      } catch (e: unknown) {
        return { ok: false, error: e instanceof Error ? e.message : '另存为失败' };
      }
    },
    [meta, getContent],
  );

  const dismissConflict = useCallback(() => setConflict(null), []);

  const copyContentToClipboard = useCallback(async () => {
    try {
      await navigator.clipboard.writeText(getContent());
    } catch {
      /* 忽略剪贴板失败 */
    }
  }, [getContent]);

  const notEditable = !!meta && (!meta.editable || meta.readonly);

  return {
    meta,
    loading,
    loadError,
    loadErrorCode,
    dirty,
    saving,
    saveError,
    conflict,
    savedAt,
    notEditable,
    hostRef,
    getContent,
    doSave,
    reloadRemote,
    restore,
    saveAs,
    dismissConflict,
    copyContentToClipboard,
  };
}

// editLoadError 把读取内容的错误翻译为可读中文。
export function editLoadError(e: unknown): string {
  if (e instanceof ApiError) {
    switch (e.code) {
      case 2001:
        return '文件不存在';
      case 2007:
        return '目标不是文件';
      case 2013:
        return '该文件不是可编辑的文本';
      case 2014:
        return '文件过大，无法在线编辑';
      case 4000:
        return '当前存储不支持文本编辑';
      default:
        return e.message;
    }
  }
  return e instanceof Error ? e.message : '加载失败';
}

// extractConflict 从 ApiError 上尝试取 data 字段（保存冲突 2012 时透传当前最新 revision）。
function extractConflict(e: ApiError & { data?: SaveConflict }): SaveConflict | null {
  const d = e.data;
  if (d && typeof d === 'object' && 'currentRevision' in d) {
    return d as SaveConflict;
  }
  return null;
}

import { useCallback, useEffect, useState } from 'react';
import { api, getToken } from './api';

export const DOCUMENT_PREVIEW_MAX_BYTES = 20 * 1024 * 1024;

export type DocumentLoadState =
  | { status: 'loading'; loaded: number; total: number }
  | { status: 'ready'; data: ArrayBuffer }
  | { status: 'too-large' }
  | { status: 'error'; message: string };

export function useDocumentPreview(path: string, size: number): DocumentLoadState & { retry: () => void } {
  const [attempt, setAttempt] = useState(0);
  const [state, setState] = useState<DocumentLoadState>(() => (
    size > DOCUMENT_PREVIEW_MAX_BYTES
      ? { status: 'too-large' }
      : { status: 'loading', loaded: 0, total: size }
  ));
  const retry = useCallback(() => setAttempt((value) => value + 1), []);

  useEffect(() => {
    if (size > DOCUMENT_PREVIEW_MAX_BYTES) {
      setState({ status: 'too-large' });
      return;
    }

    const controller = new AbortController();
    let lastReportedPercent = -1;
    setState({ status: 'loading', loaded: 0, total: size });

    const load = async () => {
      try {
        const headers: Record<string, string> = {};
        const token = getToken();
        if (token) headers.Authorization = `Bearer ${token}`;
        const response = await fetch(api.fs.documentPreviewUrl(path), {
          headers,
          credentials: 'same-origin',
          signal: controller.signal,
        });
        if (!response.ok) throw new Error(await previewResponseError(response));

        const headerTotal = Number(response.headers.get('Content-Length'));
        const total = Number.isFinite(headerTotal) && headerTotal > 0 ? headerTotal : size;
        if (total > DOCUMENT_PREVIEW_MAX_BYTES) throw new Error('文件超过 20 MiB，已停止载入');

        if (!response.body) {
          const data = await response.arrayBuffer();
          if (data.byteLength > DOCUMENT_PREVIEW_MAX_BYTES) throw new Error('文件超过 20 MiB，已停止载入');
          setState({ status: 'ready', data });
          return;
        }

        const reader = response.body.getReader();
        const chunks: Uint8Array[] = [];
        let loaded = 0;
        for (;;) {
          const { done, value } = await reader.read();
          if (done) break;
          if (!value) continue;
          loaded += value.byteLength;
          if (loaded > DOCUMENT_PREVIEW_MAX_BYTES) {
            await reader.cancel();
            throw new Error('文件超过 20 MiB，已停止载入');
          }
          chunks.push(value);
          const percent = total > 0 ? Math.floor((loaded / total) * 100) : 0;
          if (percent !== lastReportedPercent) {
            lastReportedPercent = percent;
            setState({ status: 'loading', loaded, total });
          }
        }

        const blob = new Blob(chunks);
        setState({ status: 'ready', data: await blob.arrayBuffer() });
      } catch (error) {
        if (controller.signal.aborted) return;
        setState({ status: 'error', message: error instanceof Error ? error.message : '文档载入失败' });
      }
    };

    void load();
    return () => controller.abort();
  }, [attempt, path, size]);

  return { ...state, retry };
}

async function previewResponseError(response: Response): Promise<string> {
  if (response.status === 413) return '文件超过 20 MiB，无法在线预览';
  try {
    const body = await response.json() as { message?: string };
    if (body.message) return body.message;
  } catch {
    // 非 JSON 响应使用 HTTP 状态兜底。
  }
  return `文档载入失败 (HTTP ${response.status})`;
}

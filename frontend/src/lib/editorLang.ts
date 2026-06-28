import { Extension } from '@codemirror/state';
import { javascript } from '@codemirror/lang-javascript';
import { json } from '@codemirror/lang-json';
import { markdown } from '@codemirror/lang-markdown';
import { html } from '@codemirror/lang-html';
import { css } from '@codemirror/lang-css';
import { python } from '@codemirror/lang-python';
import { go } from '@codemirror/lang-go';

// languageForName 按文件扩展名返回对应的 CodeMirror 语言扩展（用于语法高亮）。
// 未识别的扩展名返回空扩展数组（纯文本，仅基础高亮）。
export function languageForName(name: string): Extension {
  const idx = name.lastIndexOf('.');
  const ext = idx >= 0 ? name.slice(idx + 1).toLowerCase() : '';
  switch (ext) {
    case 'js':
    case 'jsx':
    case 'mjs':
    case 'cjs':
      return javascript();
    case 'ts':
      return javascript({ typescript: true });
    case 'tsx':
      return javascript({ typescript: true, jsx: true });
    case 'json':
      return json();
    case 'md':
    case 'markdown':
      return markdown();
    case 'html':
    case 'htm':
    case 'vue':
    case 'svelte':
      return html();
    case 'css':
    case 'scss':
    case 'less':
      return css();
    case 'py':
      return python();
    case 'go':
      return go();
    default:
      return [];
  }
}

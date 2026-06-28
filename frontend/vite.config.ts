import tailwindcss from '@tailwindcss/vite';
import react from '@vitejs/plugin-react';
import path from 'path';
import {defineConfig} from 'vite';

export default defineConfig(() => {
  return {
    plugins: [react(), tailwindcss()],
    resolve: {
      alias: {
        '@': path.resolve(__dirname, '.'),
      },
    },
    build: {
      rollupOptions: {
        output: {
          manualChunks(id) {
            if (!id.includes('node_modules')) return;
            if (id.includes('/react/') || id.includes('/react-dom/') || id.includes('/scheduler/')) {
              return 'vendor-react';
            }
            if (id.includes('/@codemirror/lang-javascript/') || id.includes('/@lezer/javascript/')) {
              return 'editor-lang-javascript';
            }
            if (id.includes('/@codemirror/lang-markdown/') || id.includes('/@lezer/markdown/')) {
              return 'editor-lang-markdown';
            }
            if (id.includes('/@codemirror/lang-html/') || id.includes('/@lezer/html/')) {
              return 'editor-lang-html';
            }
            if (id.includes('/@codemirror/lang-css/') || id.includes('/@lezer/css/')) {
              return 'editor-lang-css';
            }
            if (id.includes('/@codemirror/lang-python/') || id.includes('/@lezer/python/')) {
              return 'editor-lang-python';
            }
            if (id.includes('/@codemirror/lang-go/') || id.includes('/@lezer/go/')) {
              return 'editor-lang-go';
            }
            if (id.includes('/@codemirror/') || id.includes('/@lezer/')) {
              return 'editor-core';
            }
            if (id.includes('/lucide-react/')) {
              return 'vendor-icons';
            }
            if (id.includes('/@google/genai/')) {
              return 'vendor-ai';
            }
            return 'vendor';
          },
        },
      },
    },
    server: {
      // HMR is disabled in AI Studio via DISABLE_HMR env var.
      // Do not modifyâfile watching is disabled to prevent flickering during agent edits.
      hmr: process.env.DISABLE_HMR !== 'true',
      // Disable file watching when DISABLE_HMR is true to save CPU during agent edits.
      watch: process.env.DISABLE_HMR === 'true' ? null : {},
      // 开发期将 /api 代理到后端，避免跨域并复用同源 Cookie。
      proxy: {
        '/api': {
          target: 'http://localhost:16550',
          changeOrigin: true,
        },
      },
    },
  };
});

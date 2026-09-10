import React from 'react';
import ReactMarkdown, { defaultUrlTransform } from 'react-markdown';
import remarkGfm from 'remark-gfm';
import { ExternalLink } from 'lucide-react';
import { api } from '../lib/api';

export function MarkdownPreview({ content, path }: { content: string; path: string }) {
  return (
    <div className="h-full overflow-y-auto bg-[#f7f5f0] px-5 py-8 dark:bg-[#12151a] sm:px-10">
      <article className="mx-auto min-h-full max-w-4xl rounded-sm border border-stone-200/80 bg-white px-6 py-9 shadow-[0_18px_50px_rgba(51,45,36,0.08)] dark:border-white/8 dark:bg-[#191d24] dark:shadow-[0_18px_50px_rgba(0,0,0,0.24)] sm:px-12 sm:py-12">
        <ReactMarkdown
          remarkPlugins={[remarkGfm]}
          skipHtml
          urlTransform={(url, key) => resolveMarkdownUrl(path, url, key === 'src')}
          components={{
            h1: ({ children }) => <h1 className="mb-7 border-b border-stone-200 pb-4 font-serif text-3xl font-semibold tracking-tight text-stone-900 dark:border-white/10 dark:text-stone-50">{children}</h1>,
            h2: ({ children }) => <h2 className="mb-4 mt-10 font-serif text-2xl font-semibold tracking-tight text-stone-900 dark:text-stone-100">{children}</h2>,
            h3: ({ children }) => <h3 className="mb-3 mt-8 text-lg font-semibold text-stone-800 dark:text-stone-100">{children}</h3>,
            h4: ({ children }) => <h4 className="mb-2 mt-6 text-base font-semibold text-stone-800 dark:text-stone-200">{children}</h4>,
            p: ({ children }) => <p className="my-4 text-[15px] leading-7 text-stone-700 dark:text-stone-300">{children}</p>,
            a: ({ href, children }) => (
              <a href={href} target="_blank" rel="noreferrer" className="inline-flex items-baseline gap-1 text-blue-600 underline decoration-blue-300 underline-offset-4 hover:text-blue-700 dark:text-sky-400 dark:decoration-sky-700 dark:hover:text-sky-300">
                {children}<ExternalLink className="inline h-3 w-3 shrink-0" aria-hidden="true" />
              </a>
            ),
            blockquote: ({ children }) => <blockquote className="my-6 border-l-4 border-amber-400 bg-amber-50/70 px-5 py-1 italic text-stone-600 dark:border-amber-500 dark:bg-amber-500/8 dark:text-stone-300">{children}</blockquote>,
            ul: ({ children }) => <ul className="my-4 list-disc space-y-1.5 pl-6 text-[15px] leading-7 text-stone-700 marker:text-stone-400 dark:text-stone-300">{children}</ul>,
            ol: ({ children }) => <ol className="my-4 list-decimal space-y-1.5 pl-6 text-[15px] leading-7 text-stone-700 marker:font-mono marker:text-stone-400 dark:text-stone-300">{children}</ol>,
            li: ({ children }) => <li className="pl-1">{children}</li>,
            hr: () => <hr className="my-10 border-0 border-t border-stone-200 dark:border-white/10" />,
            pre: ({ children }) => <pre className="my-6 overflow-x-auto rounded-xl border border-slate-800 bg-[#11151b] p-5 text-[13px] leading-6 text-slate-200 shadow-inner">{children}</pre>,
            code: ({ className, children }) => className
              ? <code className={`${className} font-mono`}>{children}</code>
              : <code className="rounded bg-stone-100 px-1.5 py-0.5 font-mono text-[0.88em] text-rose-700 dark:bg-white/8 dark:text-rose-300">{children}</code>,
            table: ({ children }) => <div className="my-6 overflow-x-auto rounded-lg border border-stone-200 dark:border-white/10"><table className="w-full border-collapse text-left text-sm">{children}</table></div>,
            thead: ({ children }) => <thead className="bg-stone-100 text-stone-700 dark:bg-white/6 dark:text-stone-200">{children}</thead>,
            th: ({ children }) => <th className="border-b border-r border-stone-200 px-3 py-2.5 font-semibold last:border-r-0 dark:border-white/10">{children}</th>,
            td: ({ children }) => <td className="border-b border-r border-stone-100 px-3 py-2.5 align-top text-stone-600 last:border-r-0 dark:border-white/6 dark:text-stone-300">{children}</td>,
            img: ({ src, alt }) => <img src={src} alt={alt ?? ''} loading="lazy" className="mx-auto my-7 max-h-[60vh] max-w-full rounded-lg border border-stone-200 object-contain shadow-sm dark:border-white/10" />,
            input: ({ node: _node, ...props }) => <input {...props} className="mr-2 accent-blue-600" />,
          }}
        >
          {content}
        </ReactMarkdown>
      </article>
    </div>
  );
}

function resolveMarkdownUrl(sourcePath: string, value: string, isImage: boolean): string {
  const safe = defaultUrlTransform(value);
  if (!safe || safe.startsWith('#') || /^[a-z][a-z\d+.-]*:/i.test(safe) || safe.startsWith('//')) return safe;

  const suffixIndex = safe.search(/[?#]/u);
  const pathPart = suffixIndex >= 0 ? safe.slice(0, suffixIndex) : safe;
  const hashIndex = safe.indexOf('#');
  const hash = hashIndex >= 0 ? safe.slice(hashIndex) : '';
  const absolute = pathPart.startsWith('/')
    ? normalizeAPIPath(pathPart)
    : normalizeAPIPath(`${sourcePath.slice(0, sourcePath.lastIndexOf('/') + 1)}${pathPart}`);
  const previewUrl = api.fs.downloadUrl(absolute);
  return isImage ? previewUrl : `${previewUrl}${hash}`;
}

function normalizeAPIPath(value: string): string {
  const parts: string[] = [];
  for (const segment of value.split('/')) {
    if (!segment || segment === '.') continue;
    if (segment === '..') parts.pop();
    else parts.push(segment);
  }
  return `/${parts.join('/')}`;
}

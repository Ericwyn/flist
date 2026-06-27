import React from 'react';
import { FileKind } from '../types';
import {
  Folder,
  FileText,
  Image as ImageIcon,
  Film,
  Music,
  File,
} from 'lucide-react';
import { cn } from '../lib/utils';

export function FileIcon({ kind, className }: { kind: FileKind; className?: string }) {
  switch (kind) {
    case 'folder':
      return <Folder className={cn('text-amber-500 fill-amber-500/20', className)} />;
    case 'text':
      return <FileText className={cn('text-slate-400', className)} />;
    case 'image':
      return <ImageIcon className={cn('text-blue-500', className)} />;
    case 'video':
      return <Film className={cn('text-rose-500', className)} />;
    case 'audio':
      return <Music className={cn('text-purple-500', className)} />;
    default:
      return <File className={cn('text-slate-400', className)} />;
  }
}

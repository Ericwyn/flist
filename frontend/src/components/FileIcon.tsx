import React from 'react';
import { FileKind } from '../types';
import {
  Folder,
  FileText,
  Image as ImageIcon,
  Film,
  Music,
  File,
  FileType,
} from 'lucide-react';
import { cn } from '../lib/utils';

interface FileIconProps {
  kind: FileKind;
  className?: string;
  style?: React.CSSProperties;
}

export function FileIcon({ kind, className, style }: FileIconProps) {
  switch (kind) {
    case 'folder':
      return <Folder className={cn('text-amber-500 fill-amber-500/20', className)} style={style} />;
    case 'text':
      return <FileText className={cn('text-slate-400', className)} style={style} />;
    case 'image':
      return <ImageIcon className={cn('text-blue-500', className)} style={style} />;
    case 'video':
      return <Film className={cn('text-rose-500', className)} style={style} />;
    case 'audio':
      return <Music className={cn('text-purple-500', className)} style={style} />;
    case 'pdf':
      return <FileType className={cn('text-red-500', className)} style={style} />;
    default:
      return <File className={cn('text-slate-400', className)} style={style} />;
  }
}

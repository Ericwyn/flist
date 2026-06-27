export type FileType = 'folder' | 'text' | 'image' | 'video' | 'audio' | 'unknown';

export interface FileItem {
  id: string;
  name: string;
  type: FileType;
  size: number; // in bytes, 0 for folder
  modifiedAt: number; // timestamp
  accessedAt?: number; // timestamp for recent access
  path: string; // full path including the file itself, e.g., /home/user/document.txt
  parentId: string | null; // id of parent folder, null for root
  
  // Mock content for previews
  content?: string; 
  url?: string; 
}

export interface ClipboardState {
  action: 'copy' | 'cut';
  fileIds: string[];
}

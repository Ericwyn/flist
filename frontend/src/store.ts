import { create } from 'zustand';
import { FileItem, ClipboardState } from './types';

// Helper to generate IDs
const generateId = () => Math.random().toString(36).substring(2, 9);

// Initial Mock Data
const initialFiles: FileItem[] = [
  {
    id: 'root',
    name: 'Root',
    type: 'folder',
    size: 0,
    modifiedAt: Date.now(),
    path: '/',
    parentId: null,
  },
  {
    id: 'docs',
    name: 'Documents',
    type: 'folder',
    size: 0,
    modifiedAt: Date.now() - 100000,
    path: '/Documents',
    parentId: 'root',
  },
  {
    id: 'pics',
    name: 'Pictures',
    type: 'folder',
    size: 0,
    modifiedAt: Date.now() - 200000,
    path: '/Pictures',
    parentId: 'root',
  },
  {
    id: 'media',
    name: 'Media',
    type: 'folder',
    size: 0,
    modifiedAt: Date.now() - 300000,
    path: '/Media',
    parentId: 'root',
  },
  {
    id: 'readme',
    name: 'README.md',
    type: 'text',
    size: 1024,
    modifiedAt: Date.now() - 5000,
    path: '/README.md',
    parentId: 'root',
    content: '# Welcome to Flist\n\nThis is a modern web-based file manager prototype.\n\n- Browse files\n- Preview media\n- Edit text files',
  },
  {
    id: 'notes',
    name: 'meeting-notes.txt',
    type: 'text',
    size: 512,
    modifiedAt: Date.now() - 50000,
    path: '/Documents/meeting-notes.txt',
    parentId: 'docs',
    content: 'Meeting Notes:\n- Discuss new frontend architecture.\n- Review mockups.',
  },
  {
    id: 'photo1',
    name: 'vacation.jpg',
    type: 'image',
    size: 2048576, // 2MB
    modifiedAt: Date.now() - 80000,
    path: '/Pictures/vacation.jpg',
    parentId: 'pics',
    url: 'https://images.unsplash.com/photo-1476514525535-07fb3b4ae5f1?auto=format&fit=crop&w=1000&q=80',
  },
  {
    id: 'photo2',
    name: 'workspace.png',
    type: 'image',
    size: 1048576, // 1MB
    modifiedAt: Date.now() - 90000,
    path: '/Pictures/workspace.png',
    parentId: 'pics',
    url: 'https://images.unsplash.com/photo-1497366216548-37526070297c?auto=format&fit=crop&w=1000&q=80',
  },
  {
    id: 'vid1',
    name: 'demo-video.mp4',
    type: 'video',
    size: 15000000, // 15MB
    modifiedAt: Date.now() - 120000,
    path: '/Media/demo-video.mp4',
    parentId: 'media',
    url: 'https://commondatastorage.googleapis.com/gtv-videos-bucket/sample/BigBuckBunny.mp4',
  },
  {
    id: 'audio1',
    name: 'background-music.mp3',
    type: 'audio',
    size: 4000000, // 4MB
    modifiedAt: Date.now() - 150000,
    path: '/Media/background-music.mp3',
    parentId: 'media',
    url: 'https://commondatastorage.googleapis.com/codeskulptor-assets/Epoq-Lepidoptera.ogg',
  }
];

interface FileSystemState {
  files: FileItem[];
  currentFolderId: string;
  history: string[];
  historyIndex: number;
  selectedFileIds: string[];
  clipboard: ClipboardState | null;
  favorites: string[]; // array of folder IDs
  theme: 'light' | 'dark';
  previewFileId: string | null;
  
  viewMode: 'grid' | 'list';
  iconSize: 'small' | 'medium' | 'large';
  contextMenu: { x: number, y: number, fileId: string | null } | null;
  
  // Actions
  setCurrentFolder: (folderId: string) => void;
  goBack: () => void;
  goForward: () => void;
  restoreHistoryState: (folderId: string, index: number) => void;
  selectFile: (fileId: string, multi?: boolean) => void;
  selectFiles: (fileIds: string[], append?: boolean) => void;
  clearSelection: () => void;
  selectAll: () => void;
  
  createFolder: (name: string) => void;
  deleteSelected: () => void;
  renameFile: (fileId: string, newName: string) => void;
  copySelected: () => void;
  cutSelected: () => void;
  paste: () => void;
  
  toggleFavorite: (folderId: string) => void;
  toggleTheme: () => void;
  
  setViewMode: (mode: 'grid' | 'list') => void;
  setIconSize: (size: 'small' | 'medium' | 'large') => void;
  openContextMenu: (x: number, y: number, fileId: string | null) => void;
  closeContextMenu: () => void;
  
  openPreview: (fileId: string) => void;
  closePreview: () => void;
  saveFileContent: (fileId: string, content: string) => void;
  removeRecentAccess: (fileId: string) => void;
}

export const useStore = create<FileSystemState>((set, get) => ({
  files: initialFiles,
  currentFolderId: 'root',
  history: ['root'],
  historyIndex: 0,
  selectedFileIds: [],
  clipboard: null,
  favorites: ['docs', 'pics'],
  theme: 'light',
  previewFileId: null,
  viewMode: 'grid',
  iconSize: 'medium',
  contextMenu: null,

  setCurrentFolder: (folderId) => set((state) => {
    if (state.currentFolderId === folderId) return state;
    const newHistory = state.history.slice(0, state.historyIndex + 1);
    newHistory.push(folderId);
    return { 
      currentFolderId: folderId, 
      selectedFileIds: [],
      history: newHistory,
      historyIndex: newHistory.length - 1,
      files: state.files.map(f => f.id === folderId ? { ...f, accessedAt: Date.now() } : f)
    };
  }),

  goBack: () => set((state) => {
    if (state.historyIndex > 0) {
      const newIndex = state.historyIndex - 1;
      return {
        currentFolderId: state.history[newIndex],
        historyIndex: newIndex,
        selectedFileIds: []
      };
    }
    return state;
  }),

  goForward: () => set((state) => {
    if (state.historyIndex < state.history.length - 1) {
      const newIndex = state.historyIndex + 1;
      return {
        currentFolderId: state.history[newIndex],
        historyIndex: newIndex,
        selectedFileIds: []
      };
    }
    return state;
  }),
  
  restoreHistoryState: (folderId, index) => set({
    currentFolderId: folderId,
    historyIndex: index,
    selectedFileIds: []
  }),
  
  selectFile: (fileId, multi = false) => set((state) => {
    if (multi) {
      const isSelected = state.selectedFileIds.includes(fileId);
      if (isSelected) {
        return { selectedFileIds: state.selectedFileIds.filter(id => id !== fileId) };
      } else {
        return { selectedFileIds: [...state.selectedFileIds, fileId] };
      }
    } else {
      return { selectedFileIds: [fileId] };
    }
  }),
  
  selectFiles: (fileIds, append = false) => set((state) => {
    if (append) {
      const newSelections = fileIds.filter(id => !state.selectedFileIds.includes(id));
      return { selectedFileIds: [...state.selectedFileIds, ...newSelections] };
    } else {
      return { selectedFileIds: fileIds };
    }
  }),
  
  clearSelection: () => set({ selectedFileIds: [] }),
  
  selectAll: () => set((state) => {
    const currentFiles = state.files.filter(f => f.parentId === state.currentFolderId);
    return { selectedFileIds: currentFiles.map(f => f.id) };
  }),

  createFolder: (name) => set((state) => {
    const parentFolder = state.files.find(f => f.id === state.currentFolderId);
    const newPath = parentFolder?.path === '/' ? `/${name}` : `${parentFolder?.path}/${name}`;
    const newFolder: FileItem = {
      id: generateId(),
      name,
      type: 'folder',
      size: 0,
      modifiedAt: Date.now(),
      path: newPath,
      parentId: state.currentFolderId,
    };
    return { files: [...state.files, newFolder] };
  }),

  deleteSelected: () => set((state) => {
    // Basic deletion, doesn't handle recursive folder deletion in this simple prototype
    const idsToDelete = new Set(state.selectedFileIds);
    return {
      files: state.files.filter(f => !idsToDelete.has(f.id)),
      selectedFileIds: [],
    };
  }),

  renameFile: (fileId, newName) => set((state) => {
    return {
      files: state.files.map(f => {
        if (f.id === fileId) {
          // Simplistic path update, a real app would update all children paths
          const pathParts = f.path.split('/');
          pathParts[pathParts.length - 1] = newName;
          return { ...f, name: newName, path: pathParts.join('/'), modifiedAt: Date.now() };
        }
        return f;
      })
    };
  }),

  copySelected: () => set((state) => ({
    clipboard: { action: 'copy', fileIds: [...state.selectedFileIds] },
    selectedFileIds: []
  })),

  cutSelected: () => set((state) => ({
    clipboard: { action: 'cut', fileIds: [...state.selectedFileIds] },
    selectedFileIds: []
  })),

  paste: () => set((state) => {
    if (!state.clipboard) return state;

    const newFiles = [...state.files];
    const parentFolder = state.files.find(f => f.id === state.currentFolderId);
    const basePath = parentFolder?.path === '/' ? '' : parentFolder?.path;

    state.clipboard.fileIds.forEach(id => {
      const fileToPaste = state.files.find(f => f.id === id);
      if (fileToPaste) {
        if (state.clipboard!.action === 'copy') {
          // Clone
          const newId = generateId();
          let newName = fileToPaste.name;
          // Basic copy naming
          if (newFiles.some(f => f.parentId === state.currentFolderId && f.name === newName)) {
             newName = `Copy of ${newName}`;
          }
          newFiles.push({
            ...fileToPaste,
            id: newId,
            name: newName,
            parentId: state.currentFolderId,
            path: `${basePath}/${newName}`,
            modifiedAt: Date.now()
          });
        } else if (state.clipboard!.action === 'cut') {
          // Move
          const index = newFiles.findIndex(f => f.id === id);
          if (index !== -1) {
             newFiles[index] = {
               ...newFiles[index],
               parentId: state.currentFolderId,
               path: `${basePath}/${newFiles[index].name}`,
               modifiedAt: Date.now()
             };
          }
        }
      }
    });

    return {
      files: newFiles,
      clipboard: state.clipboard.action === 'cut' ? null : state.clipboard // clear if cut
    };
  }),

  toggleFavorite: (folderId) => set((state) => {
    if (state.favorites.includes(folderId)) {
      return { favorites: state.favorites.filter(id => id !== folderId) };
    } else {
      return { favorites: [...state.favorites, folderId] };
    }
  }),

  toggleTheme: () => set((state) => {
    const newTheme = state.theme === 'light' ? 'dark' : 'light';
    if (newTheme === 'dark') {
      document.documentElement.classList.add('dark');
    } else {
      document.documentElement.classList.remove('dark');
    }
    return { theme: newTheme };
  }),

  setViewMode: (mode) => set({ viewMode: mode }),
  setIconSize: (size) => set({ iconSize: size }),
  openContextMenu: (x, y, fileId) => set({ contextMenu: { x, y, fileId } }),
  closeContextMenu: () => set({ contextMenu: null }),

  openPreview: (fileId) => set((state) => ({ 
    previewFileId: fileId,
    files: state.files.map(f => f.id === fileId ? { ...f, accessedAt: Date.now() } : f)
  })),
  closePreview: () => set({ previewFileId: null }),
  
  saveFileContent: (fileId, content) => set((state) => ({
    files: state.files.map(f => f.id === fileId ? { ...f, content, modifiedAt: Date.now() } : f)
  })),
  
  removeRecentAccess: (fileId) => set((state) => ({
    files: state.files.map(f => f.id === fileId ? { ...f, accessedAt: undefined } : f)
  }))
}));

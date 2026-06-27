import React, { useState, useEffect } from 'react';
import { useStore } from '../store';
import { FileIcon } from './FileIcon';
import { format } from 'date-fns';
import { cn } from '../lib/utils';
import { 
  FolderPlus, Trash2, Copy, Scissors, ClipboardPaste, 
  ChevronRight, Star, StarOff, MoreVertical,
  ArrowLeft, ArrowRight, LayoutGrid, List, Plus, Minus, RefreshCw, Info
} from 'lucide-react';
import { motion, AnimatePresence } from 'motion/react';
import { Modal } from './Modal';

const formatBytes = (bytes: number, decimals = 2) => {
  if (!+bytes) return '0 Bytes';
  const k = 1024;
  const dm = decimals < 0 ? 0 : decimals;
  const sizes = ['Bytes', 'KB', 'MB', 'GB', 'TB', 'PB', 'EB', 'ZB', 'YB'];
  const i = Math.floor(Math.log(bytes) / Math.log(k));
  return `${parseFloat((bytes / Math.pow(k, i)).toFixed(dm))} ${sizes[i]}`;
};

export function FileBrowser() {
  const { 
    files, currentFolderId, selectedFileIds, clipboard, favorites, previewFileId,
    history, historyIndex, viewMode, iconSize, contextMenu,
    setCurrentFolder, goBack, goForward, selectFile, clearSelection, selectAll,
    createFolder, deleteSelected, copySelected, cutSelected, paste,
    toggleFavorite, openPreview, setViewMode, setIconSize, openContextMenu, closeContextMenu,
    renameFile
  } = useStore();

  const [lastSelectedFileId, setLastSelectedFileId] = useState<string | null>(null);
  const [actionModal, setActionModal] = useState<{
    type: 'delete' | 'rename' | 'createFolder';
    fileId?: string;
  } | null>(null);
  const [modalInputValue, setModalInputValue] = useState('');
  const [infoModalFileId, setInfoModalFileId] = useState<string | null>(null);

  const currentFolder = files.find(f => f.id === currentFolderId);
  const currentFiles = files.filter(f => f.parentId === currentFolderId);
  const isFavorite = favorites.includes(currentFolderId);

  // Sync browser history with app history
  useEffect(() => {
    const newUrl = new URL(window.location.href);
    if (newUrl.hash !== `#${currentFolderId}`) {
      newUrl.hash = currentFolderId;
      window.history.pushState({ historyIndex, folderId: currentFolderId }, '', newUrl.toString());
    }
  }, [currentFolderId, historyIndex]);

  useEffect(() => {
    const handlePopState = (e: PopStateEvent) => {
      if (e.state && typeof e.state.folderId === 'string') {
        useStore.getState().restoreHistoryState(e.state.folderId, e.state.historyIndex || 0);
      } else {
        const hashFolder = window.location.hash.replace('#', '');
        if (hashFolder && hashFolder !== useStore.getState().currentFolderId) {
          useStore.getState().setCurrentFolder(hashFolder);
        }
      }
    };
    window.addEventListener('popstate', handlePopState);
    
    // Initial load from URL hash
    const hashFolder = window.location.hash.replace('#', '');
    if (hashFolder && hashFolder !== 'root') {
      // Small timeout to ensure store is ready
      setTimeout(() => useStore.getState().setCurrentFolder(hashFolder), 0);
    }
    
    return () => window.removeEventListener('popstate', handlePopState);
  }, []);

  // Generate breadcrumbs
  const getBreadcrumbs = () => {
    const crumbs = [];
    let curr = currentFolder;
    while (curr) {
      crumbs.unshift(curr);
      curr = files.find(f => f.id === curr!.parentId);
    }
    return crumbs;
  };

  const breadcrumbs = getBreadcrumbs();

  // Keyboard shortcuts
  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      // Don't trigger if typing in input or if preview is open
      if (e.target instanceof HTMLInputElement || e.target instanceof HTMLTextAreaElement || previewFileId) return;

      if ((e.metaKey || e.ctrlKey) && e.key === 'a') {
        e.preventDefault();
        selectAll();
      } else if ((e.metaKey || e.ctrlKey) && e.key === 'c') {
        if (selectedFileIds.length > 0) copySelected();
      } else if ((e.metaKey || e.ctrlKey) && e.key === 'x') {
        if (selectedFileIds.length > 0) cutSelected();
      } else if ((e.metaKey || e.ctrlKey) && e.key === 'v') {
        if (clipboard) paste();
      } else if (e.key === 'Backspace' || e.key === 'Delete') {
        if (selectedFileIds.length > 0) setActionModal({ type: 'delete' });
      }
    };

    window.addEventListener('keydown', handleKeyDown);
    return () => window.removeEventListener('keydown', handleKeyDown);
  }, [selectedFileIds, clipboard, previewFileId, selectAll, copySelected, cutSelected, paste, deleteSelected]);

  const handleFileClick = (e: React.MouseEvent, fileId: string) => {
    e.stopPropagation();
    closeContextMenu();
    
    if (e.shiftKey && lastSelectedFileId) {
      const currentIndex = currentFiles.findIndex(f => f.id === fileId);
      const lastIndex = currentFiles.findIndex(f => f.id === lastSelectedFileId);
      
      if (currentIndex !== -1 && lastIndex !== -1) {
        const start = Math.min(currentIndex, lastIndex);
        const end = Math.max(currentIndex, lastIndex);
        const rangeIds = currentFiles.slice(start, end + 1).map(f => f.id);
        useStore.getState().selectFiles(rangeIds, e.metaKey || e.ctrlKey);
      }
    } else if (e.metaKey || e.ctrlKey) {
      selectFile(fileId, true);
      setLastSelectedFileId(fileId);
    } else {
      selectFile(fileId, false);
      setLastSelectedFileId(fileId);
    }
  };

  const handleFileDoubleClick = (fileId: string) => {
    const file = files.find(f => f.id === fileId);
    if (file?.type === 'folder') {
      setCurrentFolder(fileId);
    } else {
      openPreview(fileId);
    }
  };

  const handleBackgroundClick = (e: React.MouseEvent) => {
    clearSelection();
    closeContextMenu();
  };

  const handleContextMenu = (e: React.MouseEvent, fileId: string | null) => {
    e.preventDefault();
    e.stopPropagation();
    if (fileId && !selectedFileIds.includes(fileId)) {
      selectFile(fileId, false);
    }
    openContextMenu(e.clientX, e.clientY, fileId);
  };

  const handleCreateFolder = () => {
    setActionModal({ type: 'createFolder' });
    setModalInputValue('');
    closeContextMenu();
  };

  const handleRename = (fileId: string) => {
    const file = files.find(f => f.id === fileId);
    if (!file) return;
    setActionModal({ type: 'rename', fileId });
    setModalInputValue(file.name);
    closeContextMenu();
  };
  
  const handleDelete = () => {
    setActionModal({ type: 'delete' });
    closeContextMenu();
  };

  const confirmAction = () => {
    if (actionModal?.type === 'createFolder' && modalInputValue.trim()) {
      createFolder(modalInputValue.trim());
    } else if (actionModal?.type === 'rename' && actionModal.fileId && modalInputValue.trim()) {
      renameFile(actionModal.fileId, modalInputValue.trim());
    } else if (actionModal?.type === 'delete') {
      deleteSelected();
    }
    setActionModal(null);
  };

  const canGoBack = historyIndex > 0;
  const canGoForward = historyIndex < history.length - 1;

  return (
    <div className="flex-1 flex flex-col h-full bg-white dark:bg-slate-950 transition-colors duration-200" onClick={handleBackgroundClick} onContextMenu={(e) => handleContextMenu(e, null)}>
      {/* Top Bar: Breadcrumbs & Toolbar */}
      <div className="border-b border-slate-200 dark:border-slate-800 bg-white dark:bg-slate-950 sticky top-0 z-10 shrink-0">
        <div className="h-14 flex items-center justify-between px-6">
          {/* Breadcrumbs */}
          <div className="flex items-center text-sm">
            <div className="flex items-center space-x-1 mr-4 text-slate-400">
              <button 
                onClick={() => window.history.back()} 
                disabled={!canGoBack}
                className="p-1 rounded hover:bg-slate-100 dark:hover:bg-slate-800 disabled:opacity-30 disabled:hover:bg-transparent"
              >
                <ArrowLeft className="w-4 h-4" />
              </button>
              <button 
                onClick={() => window.history.forward()} 
                disabled={!canGoForward}
                className="p-1 rounded hover:bg-slate-100 dark:hover:bg-slate-800 disabled:opacity-30 disabled:hover:bg-transparent"
              >
                <ArrowRight className="w-4 h-4" />
              </button>
            </div>
            {breadcrumbs.map((crumb, idx) => (
              <React.Fragment key={crumb.id}>
                {idx > 0 && <span className="text-slate-400 dark:text-slate-500 mx-2">/</span>}
                <button
                  onClick={() => setCurrentFolder(crumb.id)}
                  className={cn(
                    "hover:text-blue-600 dark:hover:text-blue-400 transition-colors max-w-[150px] truncate",
                    idx === breadcrumbs.length - 1 ? "text-slate-900 dark:text-slate-100 font-medium" : "text-slate-500 dark:text-slate-400"
                  )}
                >
                  {crumb.name}
                </button>
              </React.Fragment>
            ))}
            
            {currentFolderId !== 'root' && (
              <button 
                onClick={() => toggleFavorite(currentFolderId)}
                className="ml-3 p-1 rounded hover:bg-slate-100 dark:hover:bg-slate-800 text-slate-400 hover:text-amber-500 transition-colors"
                title={isFavorite ? "Remove from favorites" : "Add to favorites"}
              >
                {isFavorite ? <Star className="w-4 h-4 fill-amber-500 text-amber-500" /> : <StarOff className="w-4 h-4" />}
              </button>
            )}
          </div>

          {/* Action Toolbar */}
          <div className="flex items-center space-x-2">
            <button
              onClick={() => {
                if (selectedFileIds.length === 1) {
                  setInfoModalFileId(selectedFileIds[0]);
                }
              }}
              disabled={selectedFileIds.length !== 1}
              className="p-1.5 text-slate-500 hover:text-slate-700 hover:bg-slate-100 dark:hover:bg-slate-800 dark:hover:text-slate-300 rounded-lg transition-colors mr-1 disabled:opacity-30"
              title="属性"
            >
              <Info className="w-4 h-4" />
            </button>

            <button
              onClick={() => {
                // simulate refresh for UI feedback
                const el = document.getElementById('refresh-icon');
                if (el) {
                  el.classList.add('animate-spin');
                  setTimeout(() => el.classList.remove('animate-spin'), 500);
                }
              }}
              className="p-1.5 text-slate-500 hover:text-slate-700 hover:bg-slate-100 dark:hover:bg-slate-800 dark:hover:text-slate-300 rounded-lg transition-colors mr-2"
              title="刷新"
            >
              <RefreshCw id="refresh-icon" className="w-4 h-4" />
            </button>
            
            {viewMode === 'grid' && (
              <div className="flex items-center space-x-1 bg-slate-100 dark:bg-slate-800/50 p-1 rounded-lg mr-2">
                <button
                  onClick={() => setIconSize(iconSize === 'large' ? 'medium' : 'small')}
                  disabled={iconSize === 'small'}
                  className="p-1 rounded text-slate-500 hover:text-slate-700 dark:hover:text-slate-300 disabled:opacity-30 disabled:hover:text-slate-500"
                  title="缩小图标"
                >
                  <Minus className="w-4 h-4" />
                </button>
                <span className="text-[10px] text-slate-400 font-medium px-1 w-8 text-center">
                  {iconSize === 'small' ? '小' : iconSize === 'medium' ? '中' : '大'}
                </span>
                <button
                  onClick={() => setIconSize(iconSize === 'small' ? 'medium' : 'large')}
                  disabled={iconSize === 'large'}
                  className="p-1 rounded text-slate-500 hover:text-slate-700 dark:hover:text-slate-300 disabled:opacity-30 disabled:hover:text-slate-500"
                  title="放大图标"
                >
                  <Plus className="w-4 h-4" />
                </button>
              </div>
            )}

            <div className="flex items-center space-x-1 bg-slate-100 dark:bg-slate-800/50 p-1 rounded-lg">
              <button
                onClick={() => setViewMode('grid')}
                className={cn(
                  "p-1.5 rounded",
                  viewMode === 'grid' ? "bg-white dark:bg-slate-700 shadow-sm text-blue-600 dark:text-blue-400" : "text-slate-500 hover:text-slate-700 dark:hover:text-slate-300"
                )}
                title="网格视图"
              >
                <LayoutGrid className="w-4 h-4" />
              </button>
              <button
                onClick={() => setViewMode('list')}
                className={cn(
                  "p-1.5 rounded",
                  viewMode === 'list' ? "bg-white dark:bg-slate-700 shadow-sm text-blue-600 dark:text-blue-400" : "text-slate-500 hover:text-slate-700 dark:hover:text-slate-300"
                )}
                title="列表视图"
              >
                <List className="w-4 h-4" />
              </button>
            </div>
          </div>
        </div>
      </div>

      {/* File List/Grid Area */}
      <div className={cn(
        "flex-1 overflow-y-auto relative",
        viewMode === 'grid' ? "p-8" : "p-0"
      )}>
        {currentFiles.length === 0 ? (
          <div className="h-full flex flex-col items-center justify-center text-slate-400 dark:text-slate-600">
            <FolderPlus className="w-16 h-16 mb-4 opacity-50" />
            <p>此文件夹为空</p>
            <p className="text-sm mt-1">请拖拽文件到这里或新建文件夹</p>
          </div>
        ) : viewMode === 'grid' ? (
          <div className={cn(
            "grid",
            iconSize === 'small' ? "grid-cols-4 sm:grid-cols-6 md:grid-cols-8 lg:grid-cols-10 gap-4" :
            iconSize === 'large' ? "grid-cols-2 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5 xl:grid-cols-6 gap-8" :
            "grid-cols-3 sm:grid-cols-4 md:grid-cols-5 lg:grid-cols-6 xl:grid-cols-8 gap-8"
          )}>
            {currentFiles.map(file => {
              const isSelected = selectedFileIds.includes(file.id);
              const isCut = clipboard?.action === 'cut' && clipboard.fileIds.includes(file.id);
              
              const typeColorClasses = file.type === 'folder' ? "bg-amber-50 border-amber-100 group-hover:bg-amber-100 dark:bg-amber-950/30 dark:border-amber-900/50 dark:group-hover:bg-amber-900/40 text-amber-500" :
                file.type === 'image' ? "bg-blue-50 border-blue-100 group-hover:bg-blue-100 dark:bg-blue-950/30 dark:border-blue-900/50 dark:group-hover:bg-blue-900/40 text-blue-500" :
                file.type === 'video' ? "bg-rose-50 border-rose-100 group-hover:bg-rose-100 dark:bg-rose-950/30 dark:border-rose-900/50 dark:group-hover:bg-rose-900/40 text-rose-500" :
                file.type === 'audio' ? "bg-purple-50 border-purple-100 group-hover:bg-purple-100 dark:bg-purple-950/30 dark:border-purple-900/50 dark:group-hover:bg-purple-900/40 text-purple-500" :
                "bg-slate-50 border-slate-100 group-hover:bg-slate-100 dark:bg-slate-900/50 dark:border-slate-800 dark:group-hover:bg-slate-800 text-slate-400";

              return (
                <motion.div
                  layoutId={`file-${file.id}`}
                  key={file.id}
                  onClick={(e) => handleFileClick(e, file.id)}
                  onDoubleClick={() => handleFileDoubleClick(file.id)}
                  onContextMenu={(e) => handleContextMenu(e, file.id)}
                  className={cn(
                    "flex flex-col items-center group cursor-pointer select-none",
                    isCut && "opacity-50"
                  )}
                >
                  <div className={cn(
                    "rounded-2xl flex items-center justify-center border group-active:scale-95 transition-all relative",
                    iconSize === 'small' ? "w-16 h-16" : iconSize === 'large' ? "w-32 h-32" : "w-20 h-20",
                    typeColorClasses,
                    isSelected && "ring-4 ring-blue-500/30 dark:ring-blue-500/40"
                  )}>
                    {file.type === 'image' && file.url ? (
                      <img src={file.url} alt={file.name} className="w-full h-full object-cover rounded-2xl shadow-sm" />
                    ) : (
                      <FileIcon type={file.type} className={cn(
                        iconSize === 'small' ? "w-8 h-8" : iconSize === 'large' ? "w-16 h-16" : "w-10 h-10"
                      )} />
                    )}

                    <button 
                      onClick={(e) => handleContextMenu(e, file.id)}
                      className="absolute -top-2 -right-2 p-1 rounded-full bg-white dark:bg-slate-800 shadow-sm opacity-0 group-hover:opacity-100 hover:bg-slate-100 dark:hover:bg-slate-700 transition-opacity border border-slate-200 dark:border-slate-700"
                    >
                      <MoreVertical className="w-3 h-3 text-slate-500" />
                    </button>
                  </div>
                  
                  <span className={cn(
                    "mt-3 text-xs font-medium truncate w-full text-center px-1",
                    isSelected ? "text-blue-700 dark:text-blue-400 font-bold" : "text-slate-700 dark:text-slate-300"
                  )} title={file.name}>
                    {file.name}
                  </span>
                </motion.div>
              );
            })}
          </div>
        ) : (
          <div className="w-full flex flex-col">
            <div className="grid grid-cols-12 gap-4 px-6 py-2 text-[11px] font-semibold text-slate-400 uppercase tracking-wider border-b border-slate-200 dark:border-slate-800 sticky top-0 bg-white/80 dark:bg-slate-950/80 backdrop-blur-sm z-10">
              <div className="col-span-6">名称</div>
              <div className="col-span-3">修改日期</div>
              <div className="col-span-3">大小</div>
            </div>
            <div className="flex flex-col mt-1 px-2">
              {currentFiles.map(file => {
                const isSelected = selectedFileIds.includes(file.id);
                const isCut = clipboard?.action === 'cut' && clipboard.fileIds.includes(file.id);
                const typeColorClasses = file.type === 'folder' ? "text-amber-500" :
                  file.type === 'image' ? "text-blue-500" :
                  file.type === 'video' ? "text-rose-500" :
                  file.type === 'audio' ? "text-purple-500" :
                  "text-slate-400";

                return (
                  <div
                    key={file.id}
                    onClick={(e) => handleFileClick(e, file.id)}
                    onDoubleClick={() => handleFileDoubleClick(file.id)}
                    onContextMenu={(e) => handleContextMenu(e, file.id)}
                    className={cn(
                      "grid grid-cols-12 gap-4 px-4 py-2 items-center rounded-lg cursor-pointer transition-colors border border-transparent",
                      isSelected 
                        ? "bg-blue-50 dark:bg-blue-900/30 border-blue-100 dark:border-blue-900/50" 
                        : "hover:bg-slate-50 dark:hover:bg-slate-900/50",
                      isCut && "opacity-50"
                    )}
                  >
                    <div className="col-span-6 flex items-center space-x-3 overflow-hidden">
                      <FileIcon type={file.type} className={cn("w-5 h-5 shrink-0", typeColorClasses)} />
                      <span className={cn(
                        "text-sm truncate",
                        isSelected ? "text-blue-700 dark:text-blue-400 font-medium" : "text-slate-700 dark:text-slate-300"
                      )}>{file.name}</span>
                    </div>
                    <div className="col-span-3 text-xs text-slate-500 dark:text-slate-400">
                      {format(file.modifiedAt, 'yyyy/MM/dd HH:mm')}
                    </div>
                    <div className="col-span-3 text-xs text-slate-500 dark:text-slate-400 flex justify-between items-center">
                      <span>{file.type !== 'folder' ? formatBytes(file.size) : '--'}</span>
                      <button 
                        onClick={(e) => handleContextMenu(e, file.id)}
                        className="p-1 rounded opacity-0 group-hover:opacity-100 hover:bg-slate-200 dark:hover:bg-slate-700"
                      >
                        <MoreVertical className="w-3.5 h-3.5 text-slate-400" />
                      </button>
                    </div>
                  </div>
                );
              })}
            </div>
          </div>
        )}
      </div>

      {/* Context Menu Overlay */}
      <AnimatePresence>
        {contextMenu && (
          <motion.div
            initial={{ opacity: 0, scale: 0.95 }}
            animate={{ opacity: 1, scale: 1 }}
            exit={{ opacity: 0, scale: 0.95 }}
            transition={{ duration: 0.1 }}
            style={{ top: contextMenu.y, left: contextMenu.x }}
            className="fixed z-50 w-48 py-1 bg-white dark:bg-slate-800 rounded-xl shadow-xl border border-slate-200 dark:border-slate-700 text-sm overflow-hidden"
            onClick={(e) => e.stopPropagation()}
            onContextMenu={(e) => { e.preventDefault(); e.stopPropagation(); }}
          >
            {contextMenu.fileId ? (
              <>
                <button 
                  onClick={() => { handleFileDoubleClick(contextMenu.fileId!); closeContextMenu(); }}
                  className="w-full text-left px-4 py-2 hover:bg-slate-100 dark:hover:bg-slate-700 text-slate-700 dark:text-slate-200"
                >打开</button>
                <div className="h-px bg-slate-200 dark:bg-slate-700 my-1" />
                <button 
                  onClick={() => { copySelected(); closeContextMenu(); }}
                  className="w-full text-left px-4 py-2 hover:bg-slate-100 dark:hover:bg-slate-700 text-slate-700 dark:text-slate-200"
                >复制</button>
                <button 
                  onClick={() => { cutSelected(); closeContextMenu(); }}
                  className="w-full text-left px-4 py-2 hover:bg-slate-100 dark:hover:bg-slate-700 text-slate-700 dark:text-slate-200"
                >剪切</button>
                <button 
                  onClick={() => handleRename(contextMenu.fileId!)}
                  className="w-full text-left px-4 py-2 hover:bg-slate-100 dark:hover:bg-slate-700 text-slate-700 dark:text-slate-200"
                >重命名</button>
                <div className="h-px bg-slate-200 dark:bg-slate-700 my-1" />
                <button 
                  onClick={() => { setInfoModalFileId(contextMenu.fileId!); closeContextMenu(); }}
                  className="w-full text-left px-4 py-2 hover:bg-slate-100 dark:hover:bg-slate-700 text-slate-700 dark:text-slate-200"
                >属性</button>
                <div className="h-px bg-slate-200 dark:bg-slate-700 my-1" />
                <button 
                  onClick={handleDelete}
                  className="w-full text-left px-4 py-2 hover:bg-rose-50 dark:hover:bg-rose-900/30 text-rose-600 dark:text-rose-400"
                >删除</button>
              </>
            ) : (
              <>
                <button 
                  onClick={() => { handleCreateFolder(); closeContextMenu(); }}
                  className="w-full text-left px-4 py-2 hover:bg-slate-100 dark:hover:bg-slate-700 text-slate-700 dark:text-slate-200"
                >新建文件夹</button>
                <button 
                  onClick={() => { paste(); closeContextMenu(); }}
                  disabled={!clipboard}
                  className="w-full text-left px-4 py-2 hover:bg-slate-100 dark:hover:bg-slate-700 text-slate-700 dark:text-slate-200 disabled:opacity-50"
                >粘贴</button>
                <div className="h-px bg-slate-200 dark:bg-slate-700 my-1" />
                <button 
                  onClick={() => { 
                    const el = document.getElementById('refresh-icon');
                    if (el) {
                      el.style.transform = 'rotate(180deg)';
                      setTimeout(() => { el.style.transform = 'none'; }, 300);
                    }
                    closeContextMenu(); 
                  }}
                  className="w-full text-left px-4 py-2 hover:bg-slate-100 dark:hover:bg-slate-700 text-slate-700 dark:text-slate-200"
                >刷新</button>
              </>
            )}
          </motion.div>
        )}
      </AnimatePresence>

      {/* Action Modal */}
      <Modal
        isOpen={actionModal !== null}
        onClose={() => setActionModal(null)}
        title={actionModal?.type === 'delete' ? '确认删除' : actionModal?.type === 'rename' ? '重命名' : '新建文件夹'}
        footer={
          <>
            <button 
              onClick={() => setActionModal(null)} 
              className="px-3 py-1.5 text-xs font-medium text-slate-600 dark:text-slate-400 hover:bg-slate-100 dark:hover:bg-slate-800 rounded-lg transition-colors"
            >
              取消
            </button>
            <button 
              onClick={confirmAction} 
              className={cn(
                "px-3 py-1.5 text-xs font-medium text-white rounded-lg transition-colors shadow-sm", 
                actionModal?.type === 'delete' ? "bg-rose-600 hover:bg-rose-700" : "bg-blue-600 hover:bg-blue-700"
              )}
            >
              确定
            </button>
          </>
        }
      >
        {actionModal?.type === 'delete' ? (
          <p className="text-xs text-slate-500 dark:text-slate-400 my-2">
            确定要删除选中的 {selectedFileIds.length > 0 ? selectedFileIds.length : 1} 个项目吗？
          </p>
        ) : (
          <input
            autoFocus
            type="text"
            value={modalInputValue}
            onChange={(e) => setModalInputValue(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === 'Enter') confirmAction();
              if (e.key === 'Escape') setActionModal(null);
            }}
            className="w-full px-3 py-1.5 my-2 bg-slate-50 dark:bg-slate-950 border border-slate-200 dark:border-slate-800 rounded-lg text-sm focus:outline-none focus:border-blue-500 focus:ring-1 focus:ring-blue-500 text-slate-900 dark:text-slate-100"
            placeholder={actionModal?.type === 'rename' ? '输入新名称' : '输入文件夹名称'}
          />
        )}
      </Modal>

      {/* Info Modal */}
      <Modal
        isOpen={infoModalFileId !== null}
        onClose={() => setInfoModalFileId(null)}
        title="属性"
        footer={
          <button 
            onClick={() => setInfoModalFileId(null)} 
            className="px-3 py-1.5 text-xs font-medium text-slate-600 dark:text-slate-400 hover:bg-slate-100 dark:hover:bg-slate-800 rounded-lg transition-colors"
          >
            关闭
          </button>
        }
      >
        {(() => {
          const file = files.find(f => f.id === infoModalFileId);
          if (!file) return null;
          return (
            <div className="py-2">
              <div className="flex items-center space-x-3 mb-5">
                <FileIcon type={file.type} className="w-8 h-8" />
                <div className="flex-1 min-w-0">
                  <div className="text-sm font-semibold text-slate-900 dark:text-white truncate">
                    {file.name}
                  </div>
                  <div className="text-xs text-slate-500 dark:text-slate-400 capitalize">
                    {file.type}
                  </div>
                </div>
              </div>
              
              <div className="space-y-2.5">
                <div className="grid grid-cols-3 gap-2 text-xs">
                  <div className="text-slate-500 dark:text-slate-400">大小</div>
                  <div className="col-span-2 text-slate-900 dark:text-slate-200 font-medium">
                    {file.type === 'folder' ? '--' : `${formatBytes(file.size)} (${file.size.toLocaleString()} 字节)`}
                  </div>
                  
                  <div className="text-slate-500 dark:text-slate-400">位置</div>
                  <div className="col-span-2 text-slate-900 dark:text-slate-200 font-medium break-all">
                    {file.path}
                  </div>

                  <div className="text-slate-500 dark:text-slate-400">修改时间</div>
                  <div className="col-span-2 text-slate-900 dark:text-slate-200 font-medium">
                    {format(file.modifiedAt, 'yyyy/MM/dd HH:mm:ss')}
                  </div>

                  <div className="text-slate-500 dark:text-slate-400">权限</div>
                  <div className="col-span-2 text-slate-900 dark:text-slate-200 font-medium">
                    {file.type === 'folder' ? 'drwxr-xr-x' : '-rw-r--r--'}
                  </div>
                </div>
              </div>
            </div>
          );
        })()}
      </Modal>

      {/* Selection Info */}
      {selectedFileIds.length > 0 && (
        <div className="px-6 py-2 bg-blue-50 dark:bg-blue-900/20 text-blue-700 dark:text-blue-300 text-xs flex justify-between items-center border-t border-blue-100 dark:border-blue-900/50 shrink-0">
          <span className="font-medium">
            已选择 {selectedFileIds.length} 个项目
            {(() => {
              const selectedFiles = files.filter(f => selectedFileIds.includes(f.id));
              const folders = selectedFiles.filter(f => f.type === 'folder').length;
              const filesCount = selectedFiles.length - folders;
              if (folders > 0 || filesCount > 0) {
                return ` (${folders > 0 ? `${folders} 个文件夹` : ''}${folders > 0 && filesCount > 0 ? '，' : ''}${filesCount > 0 ? `${filesCount} 个文件` : ''})`;
              }
              return '';
            })()}
          </span>
          <button onClick={clearSelection} className="hover:underline font-medium">取消选择</button>
        </div>
      )}
    </div>
  );
}

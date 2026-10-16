import { useEffect, useMemo, useState } from "react";
import { createPortal } from "react-dom";
import { useDropzone, type FileRejection } from "react-dropzone";
import { ChevronRight, Upload } from "lucide-react";
import { toast } from "sonner";

import { addToLibrary, moveLibraryItem, removeLibraryItem } from "@/api/asset-library";
import type { FolderDto, LibraryItemDto } from "@/api/asset-library/type";
import { LibraryAssetCard } from "@/components/assets/asset-card";
import { AssetGrid, AssetGridSkeleton, LoadMoreFooter } from "@/components/assets/asset-grid";
import { AssetPreview, type AssetPreviewEntry } from "@/components/assets/asset-preview";
import { AssetSearch } from "@/components/assets/asset-search";
import { FolderCard } from "@/components/assets/folder-card";
import { FolderNameDialog } from "@/components/assets/folder-name-dialog";
import { FolderPickerDialog } from "@/components/assets/folder-picker-dialog";
import { NewMenu } from "@/components/assets/new-menu";
import { UploadCard } from "@/components/assets/upload-card";
import { EmptyMark } from "@/components/home/empty-mark";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import { Button } from "@/components/ui/button";
import { useAssetFolders } from "@/hooks/use-asset-folders";
import { useAssetLibrary } from "@/hooks/use-asset-library";
import { useAssetUploads } from "@/hooks/use-asset-uploads";
import { useDebouncedValue } from "@/hooks/use-debounced-value";
import { cn } from "@/lib/utils";
import { moveInLibrary, removeFromLibrary, type LibraryApi } from "@/utils/assets/library-actions";

/** 改资产库用到的接口 */
const LIBRARY_API: LibraryApi = { addToLibrary, moveLibraryItem, removeLibraryItem };

/** 文件选择与拖入只收这三类，和后端的素材白名单大类一致；具体格式、大小由后端裁决 */
const ACCEPT = { "image/*": [], "video/*": [], "audio/*": [] };

/**
 * 资产页「个人资产库」：用户自己整理的素材，只有一层文件夹。
 * 根目录显示文件夹卡片和未归档素材；点文件夹进入（地址栏的 folder 参数），面包屑回到根目录。
 * 搜索跨文件夹平铺、隐藏文件夹卡片。「＋ 新建」可新建文件夹、上传资产（也支持拖入文件）。
 * 上传逐个走 uploadAsset，传完登记到当前文件夹，卡片显示进度，可取消、失败可重试。
 * @param searchSlot 页面头部右侧放搜索框和「新建」的容器，用 portal 渲染进去
 * @param folder 当前文件夹 ID，null 是根目录（来自地址栏）
 * @param onFolderChange 进入或离开文件夹；replace 为 true 时不留历史记录
 */
export function LibraryView({
  searchSlot,
  folder,
  onFolderChange,
}: {
  searchSlot: HTMLElement | null;
  folder: string | null;
  onFolderChange: (folder: string | null, options?: { replace?: boolean }) => void;
}) {
  const [input, setInput] = useState("");
  const q = useDebouncedValue(input);
  const searching = q.trim() !== "";

  const folders = useAssetFolders(true);
  const { list, state } = useAssetLibrary(folder, q);
  const { items } = state;
  const currentFolder: FolderDto | null =
    (folder && folders.folders?.find((entry) => entry.id === folder)) || null;

  const [creating, setCreating] = useState(false);
  const [renaming, setRenaming] = useState<FolderDto | null>(null);
  const [deleting, setDeleting] = useState<FolderDto | null>(null);
  const [moving, setMoving] = useState<LibraryItemDto | null>(null);
  const [previewId, setPreviewId] = useState<string | null>(null);

  /** 地址栏里的文件夹已经不存在（别处删了、链接过期）：回到根目录并提示 */
  useEffect(() => {
    if (folder && folders.folders && !currentFolder) {
      toast.error("该内容已不存在");
      onFolderChange(null, { replace: true });
    }
  }, [folder, folders.folders, currentFolder, onFolderChange]);

  /** 一个文件上传并登记完成：当前列表在看同一位置时直接插到最前，其余只刷新计数 */
  const uploads = useAssetUploads((item, task) => {
    folders.adjust(task.folderId, 1);
    folders.reload();
    if (task.folderId === folder && !searching) list.update((prev) => [item, ...prev]);
  });

  const onDrop = (accepted: File[], rejected: FileRejection[]) => {
    if (rejected.length > 0) toast.error(`不支持的文件类型：${rejected[0]?.file.name}`);
    if (accepted.length > 0) uploads.add(accepted, folder);
  };
  const {
    getRootProps,
    getInputProps,
    isDragActive,
    open: openFilePicker,
  } = useDropzone({
    noClick: true,
    noKeyboard: true,
    multiple: true,
    accept: ACCEPT,
    onDrop,
  });

  const entries = useMemo<AssetPreviewEntry[]>(
    () =>
      items.map((item) => ({
        id: item.asset.id,
        asset: item.asset,
        label: item.asset.fileName ?? "素材",
      })),
    [items],
  );

  /** 该位置正在上传的任务；搜索结果里不显示，它们不属于搜索结果 */
  const pendingUploads = searching ? [] : uploads.tasks.filter((task) => task.folderId === folder);
  /** 根目录要等文件夹卡片一起到，不然会先闪一下「还没有资产」 */
  const foldersPending = folder === null && !searching && folders.loading;
  const showFolders = folder === null && !searching && folders.folders !== null;
  const folderList = showFolders ? (folders.folders ?? []) : [];

  const remove = async (item: LibraryItemDto) => {
    const ok = await removeFromLibrary(list, item.id, LIBRARY_API);
    if (!ok) return;
    folders.adjust(item.folderId, -1);
    folders.reload();
    toast.success("已移出资产库");
  };

  /** 移动；失败（请求层已弹提示，列表已回滚）抛出，让选择器保持打开 */
  const move = async (item: LibraryItemDto, folderId: string | null) => {
    const ok = await moveInLibrary(
      list,
      item.id,
      folderId,
      { folder, search: searching },
      LIBRARY_API,
    );
    if (!ok) throw new Error("move failed");
    folders.reload();
    toast.success("已移动");
  };

  const confirmDelete = async () => {
    const target = deleting;
    if (!target) return;
    setDeleting(null);
    try {
      const removed = await folders.remove(target.id);
      if (folder === target.id) onFolderChange(null, { replace: true });
      toast.success(`已删除文件夹，移出 ${removed} 个素材`);
    } catch {
      // 失败提示由请求层弹出，列表不动
    }
  };

  const empty =
    state.status === "ready" &&
    items.length === 0 &&
    pendingUploads.length === 0 &&
    folderList.length === 0;

  return (
    <div {...getRootProps()} className="relative min-h-[60vh] outline-none">
      <input {...getInputProps()} aria-label="选择要上传的文件" />

      {searchSlot &&
        createPortal(
          <>
            <AssetSearch value={input} onChange={setInput} placeholder="搜索文件名" />
            <NewMenu
              canCreateFolder={folder === null}
              onCreateFolder={() => setCreating(true)}
              onUpload={openFilePicker}
            />
          </>,
          searchSlot,
        )}

      <nav aria-label="当前位置" className="mt-4 mb-4.5 flex h-8 items-center gap-1 text-base">
        {folder || searching ? (
          <>
            <button
              type="button"
              onClick={() => {
                setInput("");
                onFolderChange(null);
              }}
              className="text-muted-foreground hover:text-foreground focus-visible:ring-ring/50 rounded-md px-1 transition-colors outline-none focus-visible:ring-3"
            >
              个人资产库
            </button>
            <ChevronRight aria-hidden className="text-muted-foreground size-4" />
            <h2 className="truncate font-semibold">
              {searching ? "搜索结果" : (currentFolder?.name ?? "…")}
            </h2>
          </>
        ) : (
          <h2 className="font-semibold">个人资产库</h2>
        )}
      </nav>

      {state.status === "loading" || foldersPending ? (
        <AssetGridSkeleton />
      ) : state.status === "error" ? (
        <EmptyMark
          title="加载失败"
          hint={
            <Button variant="outline" size="sm" onClick={list.retry}>
              重试
            </Button>
          }
        />
      ) : empty ? (
        searching ? (
          <EmptyMark title={`没有找到“${q.trim()}”`} hint="换个关键词试试" />
        ) : folder ? (
          <EmptyMark title="这个文件夹是空的" hint="点右上角「新建」上传资产，或从生成历史加入" />
        ) : (
          <EmptyMark title="还没有资产" hint="点右上角「新建」上传，或在生成历史里加入资产库" />
        )
      ) : (
        <>
          <AssetGrid>
            {folderList.map((entry) => (
              <FolderCard
                key={entry.id}
                folder={entry}
                onOpen={() => onFolderChange(entry.id)}
                onRename={() => setRenaming(entry)}
                onDelete={() => setDeleting(entry)}
              />
            ))}
            {pendingUploads.map((task) => (
              <UploadCard
                key={task.id}
                task={task}
                onCancel={() => uploads.cancel(task.id)}
                onRetry={() => uploads.retry(task.id)}
              />
            ))}
            {items.map((item) => (
              <LibraryAssetCard
                key={item.id}
                item={item}
                onPreview={() => setPreviewId(item.asset.id)}
                onMove={() => setMoving(item)}
                onRemove={() => void remove(item)}
              />
            ))}
          </AssetGrid>
          {items.length === 0 && (
            <p className="text-muted-foreground mt-6 text-center text-[13px]">
              {searching ? "没有符合的素材" : "这里还没有素材"}
            </p>
          )}
          <LoadMoreFooter
            hasMore={state.nextCursor !== ""}
            loading={state.loadingMore}
            failed={state.moreFailed}
            onLoadMore={list.loadMore}
          />
        </>
      )}

      {/* 拖入文件时的提示层：只做 opacity 过渡，不挡住指针（拖放事件要落到外层） */}
      <div
        aria-hidden
        className={cn(
          "bg-background/70 ring-ring pointer-events-none absolute -inset-2 z-20 grid place-items-center rounded-2xl ring-2 ring-dashed transition-opacity duration-150 motion-reduce:transition-none",
          isDragActive ? "opacity-100" : "opacity-0",
        )}
      >
        <span className="bg-chrome ring-chrome-border flex items-center gap-2 rounded-full px-4 py-2 text-sm ring-1">
          <Upload className="size-4" />
          松开上传到{currentFolder ? `「${currentFolder.name}」` : "根目录"}
        </span>
      </div>

      <FolderNameDialog
        open={creating}
        mode="create"
        onClose={() => setCreating(false)}
        onSubmit={folders.create}
      />
      <FolderNameDialog
        open={renaming !== null}
        mode="rename"
        initial={renaming?.name}
        onClose={() => setRenaming(null)}
        onSubmit={(name) => (renaming ? folders.rename(renaming.id, name) : Promise.resolve())}
      />

      <AlertDialog open={deleting !== null} onOpenChange={(open) => !open && setDeleting(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>删除文件夹「{deleting?.name}」？</AlertDialogTitle>
            <AlertDialogDescription>
              将移除文件夹及其中 {deleting?.itemCount ?? 0} 个素材；画布里已使用的素材不受影响。
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>取消</AlertDialogCancel>
            <AlertDialogAction variant="destructive" onClick={() => void confirmDelete()}>
              删除
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

      <FolderPickerDialog
        open={moving !== null}
        onOpenChange={(open) => !open && setMoving(null)}
        title="移动到"
        confirmLabel="移动"
        initialFolderId={moving?.folderId ?? null}
        currentFolderId={moving?.folderId ?? null}
        onConfirm={(folderId) => (moving ? move(moving, folderId) : Promise.resolve())}
      />

      <AssetPreview
        entries={entries}
        targetId={previewId}
        onTargetChange={setPreviewId}
        onClose={() => setPreviewId(null)}
      />
    </div>
  );
}

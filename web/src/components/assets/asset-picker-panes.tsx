import { useEffect, type MutableRefObject } from "react";
import { ChevronRight } from "lucide-react";
import { toast } from "sonner";

import type { AssetDto } from "@/api/asset/type";
import type { LibraryItemDto } from "@/api/asset-library/type";
import { EmptyMark } from "@/components/home/empty-mark";
import { Button } from "@/components/ui/button";
import type { AssetFoldersState } from "@/hooks/use-asset-folders";
import { useAssetLibrary } from "@/hooks/use-asset-library";
import { useGeneratedAssets } from "@/hooks/use-generated-assets";
import { hasActiveFilter, type GeneratedFilter } from "@/utils/assets/generated-filter";
import type { PagedList } from "@/utils/assets/paged-list";
import { canToggle } from "@/utils/assets/picker-selection";
import type { PickerView } from "@/utils/assets/picker-state";
import type { UploadTask } from "@/utils/assets/upload-queue";

import { HistoryAssetCard, LibraryAssetCard, type CardSelect } from "./asset-card";
import { AssetGrid, AssetGridSkeleton, LoadMoreFooter } from "./asset-grid";
import { ASSET_KINDS } from "./asset-kind-tabs";
import { FolderCard } from "./folder-card";
import { UploadCard } from "./upload-card";

/** 弹框里空状态的占位：高度比资产页矮，免得撑出滚动 */
const EMPTY_CLASS = "min-h-56 flex-1";

/** 一张卡片的选择状态：已选的可取消，没选的在已选满后禁用 */
function selectOf(
  selected: AssetDto[],
  asset: AssetDto,
  onToggle: (asset: AssetDto) => void,
): CardSelect {
  const checked = selected.some((item) => item.id === asset.id);
  return {
    selected: checked,
    disabled: !canToggle(selected, asset),
    onToggle: () => onToggle(asset),
  };
}

/**
 * 弹框里「生成历史」来源的内容：按类型与筛选条件分页，卡片可勾选。
 * 状态沿用资产页：加载骨架、失败重试、筛选无结果可清除、滚动到底加载下一页。
 * @param kind 类型标签
 * @param filter 筛选状态（关键字已防抖）
 * @param selected 当前选择
 * @param onToggle 勾选或取消一个素材
 * @param onClearFilters 「清除筛选」
 */
export function HistoryPane({
  kind,
  filter,
  selected,
  onToggle,
  onClearFilters,
}: {
  kind: PickerView["kind"];
  filter: GeneratedFilter;
  selected: AssetDto[];
  onToggle: (asset: AssetDto) => void;
  onClearFilters: () => void;
}) {
  const { list, state } = useGeneratedAssets(kind, filter);
  const { items } = state;

  if (state.status === "loading") return <AssetGridSkeleton count={10} />;
  if (state.status === "error") {
    return (
      <EmptyMark
        className={EMPTY_CLASS}
        title="加载失败"
        hint={
          <Button variant="outline" size="sm" onClick={list.retry}>
            重试
          </Button>
        }
      />
    );
  }
  if (items.length === 0) {
    return hasActiveFilter(filter) ? (
      <EmptyMark
        className={EMPTY_CLASS}
        title="没有符合条件的素材"
        hint={
          <Button variant="outline" size="sm" onClick={onClearFilters}>
            清除筛选
          </Button>
        }
      />
    ) : (
      <EmptyMark
        className={EMPTY_CLASS}
        title="暂无相关资产"
        hint="在「创作」里生成的内容会出现在这里"
      />
    );
  }
  return (
    <>
      <AssetGrid>
        {items.map((item) => (
          <HistoryAssetCard
            key={item.asset.id}
            item={item}
            select={selectOf(selected, item.asset, onToggle)}
          />
        ))}
      </AssetGrid>
      <LoadMoreFooter
        hasMore={state.nextCursor !== ""}
        loading={state.loadingMore}
        failed={state.moreFailed}
        onLoadMore={list.loadMore}
      />
    </>
  );
}

/**
 * 弹框里「个人资产库」来源的内容：根目录显示文件夹卡片，点进去出现面包屑；
 * 搜索跨文件夹平铺并隐藏文件夹卡片。素材卡可勾选；弹框内上传中的文件显示成上传卡。
 * @param view 当前位置（类型标签与文件夹）
 * @param q 文件名关键字（已防抖）
 * @param folders 文件夹列表状态，由弹框持有（上传完成要调整它的计数）
 * @param tasks 弹框内上传中的任务
 * @param selected 当前选择
 * @param listRef 弹框用它在上传完成时把新素材插进当前列表
 * @param onToggle 勾选或取消一个素材
 * @param onOpenFolder 进入文件夹
 * @param onLeaveFolder 回到根目录（面包屑；文件夹已不存在时也用它）
 * @param onCancelUpload 取消上传或移除失败的任务
 * @param onRetryUpload 重试失败的上传
 */
export function LibraryPane({
  view,
  q,
  folders,
  tasks,
  selected,
  listRef,
  onToggle,
  onOpenFolder,
  onLeaveFolder,
  onCancelUpload,
  onRetryUpload,
}: {
  view: PickerView;
  q: string;
  folders: AssetFoldersState;
  tasks: UploadTask[];
  selected: AssetDto[];
  listRef: MutableRefObject<PagedList<LibraryItemDto> | null>;
  onToggle: (asset: AssetDto) => void;
  onOpenFolder: (folder: string) => void;
  onLeaveFolder: () => void;
  onCancelUpload: (id: string) => void;
  onRetryUpload: (id: string) => void;
}) {
  const { folder, kind } = view;
  const searching = q.trim() !== "";
  const { list, state } = useAssetLibrary(folder, kind, q);
  const { items } = state;
  const currentFolder = (folder && folders.folders?.find((entry) => entry.id === folder)) || null;

  /** 把列表交给弹框：上传完成时它要往里插新素材 */
  useEffect(() => {
    listRef.current = list;
    return () => {
      if (listRef.current === list) listRef.current = null;
    };
  }, [list, listRef]);

  /** 当前文件夹已经不存在（别处删了）：回到根目录并提示 */
  useEffect(() => {
    if (folder && folders.folders && !currentFolder) {
      toast.error("该内容已不存在");
      onLeaveFolder();
    }
  }, [folder, folders.folders, currentFolder, onLeaveFolder]);

  /** 该位置正在上传的任务；搜索结果里不显示，它们不属于搜索结果 */
  const pendingUploads = searching ? [] : tasks.filter((task) => task.folderId === folder);
  /** 根目录要等文件夹卡片一起到，不然会先闪一下「还没有资产」 */
  const foldersPending = folder === null && !searching && folders.loading;
  const folderList = folder === null && !searching ? (folders.folders ?? []) : [];
  const kindLabel = ASSET_KINDS.find((entry) => entry.value === kind)?.label ?? "";

  const crumb =
    folder || searching ? (
      <nav aria-label="当前位置" className="mb-2.5 flex h-7 items-center gap-1 text-sm">
        <button
          type="button"
          onClick={onLeaveFolder}
          className="text-muted-foreground hover:text-foreground focus-visible:ring-ring/50 rounded-md px-1 transition-colors outline-none focus-visible:ring-3"
        >
          个人资产库
        </button>
        <ChevronRight aria-hidden className="text-muted-foreground size-4" />
        <h2 className="truncate font-semibold">
          {searching ? "搜索结果" : (currentFolder?.name ?? "…")}
        </h2>
      </nav>
    ) : null;

  if (state.status === "loading" || foldersPending) {
    return (
      <>
        {crumb}
        <AssetGridSkeleton count={10} />
      </>
    );
  }
  if (state.status === "error") {
    return (
      <>
        {crumb}
        <EmptyMark
          className={EMPTY_CLASS}
          title="加载失败"
          hint={
            <Button variant="outline" size="sm" onClick={list.retry}>
              重试
            </Button>
          }
        />
      </>
    );
  }

  const empty = items.length === 0 && pendingUploads.length === 0 && folderList.length === 0;
  return (
    <>
      {crumb}
      {empty ? (
        searching ? (
          <EmptyMark
            className={EMPTY_CLASS}
            title={`没有找到“${q.trim()}”`}
            hint="换个关键词试试"
          />
        ) : folder ? (
          <EmptyMark
            className={EMPTY_CLASS}
            title="这个文件夹是空的"
            hint="点右上角「上传」添加素材"
          />
        ) : (
          <EmptyMark
            className={EMPTY_CLASS}
            title="还没有资产"
            hint="点右上角「上传」，或先在资产页把生成历史加入资产库"
          />
        )
      ) : (
        <>
          <AssetGrid>
            {folderList.map((entry) => (
              <FolderCard key={entry.id} folder={entry} onOpen={() => onOpenFolder(entry.id)} />
            ))}
            {pendingUploads.map((task) => (
              <UploadCard
                key={task.id}
                task={task}
                onCancel={() => onCancelUpload(task.id)}
                onRetry={() => onRetryUpload(task.id)}
              />
            ))}
            {items.map((item) => (
              <LibraryAssetCard
                key={item.id}
                item={item}
                select={selectOf(selected, item.asset, onToggle)}
              />
            ))}
          </AssetGrid>
          {items.length === 0 && (
            <p className="text-muted-foreground mt-6 text-center text-[13px]">
              {searching ? "没有符合的素材" : `这里还没有${kindLabel}素材`}
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
    </>
  );
}

import { useCallback, useMemo, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { toast } from "sonner";

import { getCanvasList } from "@/api/canvas";
import type { CanvasListItemDto } from "@/api/canvas/type";
import { addToLibrary, moveLibraryItem, removeLibraryItem } from "@/api/asset-library";
import type { AssetKind, GeneratedItemDto } from "@/api/asset-library/type";
import { AddToLibraryAction } from "@/components/assets/add-to-library-action";
import { AssetKindTabs } from "@/components/assets/asset-kind-tabs";
import { HistoryAssetCard } from "@/components/assets/asset-card";
import { AssetGrid, AssetGridSkeleton, LoadMoreFooter } from "@/components/assets/asset-grid";
import { AssetPreview, type AssetPreviewEntry } from "@/components/assets/asset-preview";
import { AssetSearch } from "@/components/assets/asset-search";
import { FilterMenu, type FilterGroup } from "@/components/assets/filter-menu";
import { FolderPickerDialog } from "@/components/assets/folder-picker-dialog";
import { EmptyMark } from "@/components/home/empty-mark";
import { Button } from "@/components/ui/button";
import { useDebouncedValue } from "@/hooks/use-debounced-value";
import { useGeneratedAssets } from "@/hooks/use-generated-assets";
import {
  DEFAULT_GENERATED_FILTER,
  hasActiveFilter,
  sourceFromValue,
  sourceToValue,
  type GeneratedFilter,
} from "@/utils/assets/generated-filter";
import { addGeneratedToLibrary, type LibraryApi } from "@/utils/assets/library-actions";
import type { TimeRange } from "@/utils/assets/time-range";

/** 加入资产库用到的接口 */
const LIBRARY_API: LibraryApi = { addToLibrary, moveLibraryItem, removeLibraryItem };

/** 时间下拉的选项 */
const RANGE_OPTIONS: { value: TimeRange; label: string }[] = [
  { value: "all", label: "全部时间" },
  { value: "today", label: "今天" },
  { value: "week", label: "近 7 天" },
  { value: "month", label: "近 30 天" },
];

/** 排序下拉的选项 */
const ORDER_OPTIONS = [
  { value: "desc", label: "最新优先" },
  { value: "asc", label: "最早优先" },
];

/** 来源下拉里画布列表的加载状态 */
type CanvasOptions = { status: "idle" | "loading" | "ready" | "error"; items: CanvasListItemDto[] };

/** 来源下拉里最多列出多少张画布 */
const CANVAS_OPTION_LIMIT = 60;

/**
 * 资产页「生成历史」：首页对话与所有画布生成的素材，按图片 / 视频 / 音频分标签，
 * 可按来源、时间、排序、提示词筛选，滚动到底加载下一页。只读，不提供删除。
 * 点卡片用灯箱预览；已在资产库的显示角标；其余可「加入资产库…」。
 * @param searchSlot 页面头部右侧放搜索框的容器，搜索框用 portal 渲染进去
 */
export function HistoryView({ searchSlot }: { searchSlot: HTMLElement | null }) {
  const [kind, setKind] = useState<AssetKind>("image");
  const [source, setSource] = useState(DEFAULT_GENERATED_FILTER.source);
  const [range, setRange] = useState<TimeRange>("all");
  const [order, setOrder] = useState<GeneratedFilter["order"]>("desc");
  /** 输入框里的原文；防抖后才进筛选，不然每敲一个字都发请求 */
  const [input, setInput] = useState("");
  const q = useDebouncedValue(input);
  const filter = useMemo<GeneratedFilter>(
    () => ({ source, range, order, q }),
    [source, range, order, q],
  );

  const { list, state } = useGeneratedAssets(kind, filter);
  const { items } = state;
  const filtered = hasActiveFilter(filter);

  const [canvases, setCanvases] = useState<CanvasOptions>({ status: "idle", items: [] });
  /** 画布列表是否已发起请求：加载中或已成功就不重复拉，失败后重新打开下拉会再试 */
  const canvasesRequested = useRef(false);
  /** 第一次打开来源下拉时才去拉画布列表 */
  const loadCanvases = useCallback((open: boolean) => {
    if (!open || canvasesRequested.current) return;
    canvasesRequested.current = true;
    setCanvases((prev) => ({ status: "loading", items: prev.items }));
    getCanvasList({ page: 1, page_size: CANVAS_OPTION_LIMIT })
      .then((result) => setCanvases({ status: "ready", items: result.items }))
      .catch(() => {
        canvasesRequested.current = false;
        setCanvases({ status: "error", items: [] });
      });
  }, []);

  /** 「加入资产库…」目标条目；null 表示选择器关着 */
  const [adding, setAdding] = useState<GeneratedItemDto | null>(null);
  const [previewId, setPreviewId] = useState<string | null>(null);

  const entries = useMemo<AssetPreviewEntry[]>(
    () =>
      items.map((item) => ({
        id: item.asset.id,
        asset: item.asset,
        label: item.prompt || item.asset.fileName || "生成结果",
      })),
    [items],
  );

  /** 加入资产库；失败（请求层已弹提示）抛出，让选择器保持打开 */
  const add = async (item: GeneratedItemDto, folderId: string | null) => {
    const result = await addGeneratedToLibrary(list, item, folderId, LIBRARY_API);
    if (!result) throw new Error("add to library failed");
    toast.success("已加入资产库");
  };

  const clearFilters = () => {
    setSource(DEFAULT_GENERATED_FILTER.source);
    setRange("all");
    setInput("");
  };

  const sourceGroups: FilterGroup[] = [
    {
      options: [
        { value: "all", label: "全部来源" },
        { value: "conversation", label: "首页对话" },
      ],
    },
    ...(canvases.items.length > 0
      ? [
          {
            heading: "画布",
            options: canvases.items.map((canvas) => ({
              value: `canvas:${canvas.id}`,
              label: canvas.title,
            })),
          },
        ]
      : []),
  ];
  /** 选中的画布不在已加载的列表里（还没打开过下拉）时，补一个选项让按钮文案能显示它 */
  const sourceValue = sourceToValue(source);
  if (source.type === "canvas" && !canvases.items.some((canvas) => canvas.id === source.canvasId)) {
    sourceGroups.push({
      heading: "画布",
      options: [{ value: sourceValue, label: "指定画布" }],
    });
  }

  return (
    <>
      {searchSlot &&
        createPortal(
          <AssetSearch value={input} onChange={setInput} placeholder="搜索提示词" />,
          searchSlot,
        )}

      <div className="mt-4 mb-4.5 flex flex-wrap items-center gap-1">
        <AssetKindTabs value={kind} onChange={setKind} />
        <span aria-hidden className="bg-border mx-1.5 h-4 w-px" />
        <FilterMenu
          label="来源"
          value={sourceValue}
          defaultValue="all"
          groups={sourceGroups}
          onChange={(value) => setSource(sourceFromValue(value))}
          onOpenChange={loadCanvases}
          footer={
            canvases.status === "loading" ? (
              <div className="text-muted-foreground px-2 py-1.5 text-xs">正在加载画布…</div>
            ) : canvases.status === "error" ? (
              <div className="text-muted-foreground px-2 py-1.5 text-xs">画布列表加载失败</div>
            ) : undefined
          }
        />
        <FilterMenu
          label="时间"
          value={range}
          defaultValue="all"
          groups={[{ options: RANGE_OPTIONS }]}
          onChange={(value) => setRange(value as TimeRange)}
        />
        <FilterMenu
          label="排序"
          value={order}
          defaultValue="desc"
          groups={[{ options: ORDER_OPTIONS }]}
          onChange={(value) => setOrder(value as GeneratedFilter["order"])}
        />
      </div>

      {state.status === "loading" ? (
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
      ) : items.length === 0 ? (
        filtered ? (
          <EmptyMark
            title="没有符合条件的素材"
            hint={
              <Button variant="outline" size="sm" onClick={clearFilters}>
                清除筛选
              </Button>
            }
          />
        ) : (
          <EmptyMark title="暂无相关资产" hint="在「创作」里生成的内容会出现在这里" />
        )
      ) : (
        <>
          <AssetGrid>
            {items.map((item) => (
              <HistoryAssetCard
                key={item.asset.id}
                item={item}
                onPreview={() => setPreviewId(item.asset.id)}
                onAddToLibrary={() => setAdding(item)}
                onOpenCanvas={(id) => window.open(`/canvas/${id}`, "_blank", "noopener")}
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
      )}

      <FolderPickerDialog
        open={adding !== null}
        onOpenChange={(open) => !open && setAdding(null)}
        title="加入资产库"
        confirmLabel="加入"
        onConfirm={(folderId) => (adding ? add(adding, folderId) : Promise.resolve())}
      />

      <AssetPreview
        entries={entries}
        targetId={previewId}
        onTargetChange={setPreviewId}
        onClose={() => setPreviewId(null)}
        renderAction={(asset) => {
          const item = items.find((entry) => entry.asset.id === asset.id);
          return item ? (
            <AddToLibraryAction
              inLibrary={item.libraryItemId !== null}
              onAdd={(folderId) => add(item, folderId)}
            />
          ) : null;
        }}
      />
    </>
  );
}

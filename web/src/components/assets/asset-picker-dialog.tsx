import { useCallback, useMemo, useRef, useState } from "react";
import { Clock, ListFilter, Upload } from "lucide-react";
import { toast } from "sonner";

import type { AssetDto } from "@/api/asset/type";
import type { LibraryItemDto } from "@/api/asset-library/type";
import { Segmented } from "@/components/home/segmented";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogTitle } from "@/components/ui/dialog";
import { useAssetFolders } from "@/hooks/use-asset-folders";
import { useAssetUploads } from "@/hooks/use-asset-uploads";
import { useCanvasSourceMenu } from "@/hooks/use-canvas-source-menu";
import { useDebouncedValue } from "@/hooks/use-debounced-value";
import {
  DEFAULT_GENERATED_FILTER,
  sourceFromValue,
  type GeneratedFilter,
} from "@/utils/assets/generated-filter";
import type { PagedList } from "@/utils/assets/paged-list";
import { describeSelection, toggleAsset } from "@/utils/assets/picker-selection";
import {
  INITIAL_PICKER_VIEW,
  applyUploadDone,
  enterFolder,
  leaveFolder,
  switchKind,
  switchSource,
  uploadFolder,
  type PickerSource,
  type PickerView,
} from "@/utils/assets/picker-state";
import type { TimeRange } from "@/utils/assets/time-range";

import { AssetKindTabs } from "./asset-kind-tabs";
import { HistoryPane, LibraryPane } from "./asset-picker-panes";
import { AssetSearch } from "./asset-search";
import { FilterMenu } from "./filter-menu";

/** 来源切换的两个选项 */
const SOURCE_OPTIONS: { value: PickerSource; label: string }[] = [
  { value: "library", label: "个人资产库" },
  { value: "history", label: "生成历史" },
];

/** 「时间」图标按钮的选项 */
const RANGE_OPTIONS: { value: TimeRange; label: string }[] = [
  { value: "all", label: "全部时间" },
  { value: "today", label: "今天" },
  { value: "week", label: "近 7 天" },
  { value: "month", label: "近 30 天" },
];

/** 弹框内上传的文件选择：和资产页一样只收图片、视频、音频，具体格式与大小由后端裁决 */
const UPLOAD_ACCEPT = "image/*,video/*,audio/*";

/** 弹框的外框：桌面居中的大弹框，手机宽度全屏（覆盖 DialogContent 的居中定位与圆角） */
const CONTENT_CLASS = [
  "flex flex-col gap-0 overflow-hidden p-0",
  "sm:h-[min(46rem,calc(100dvh-4rem))] sm:max-w-[min(64rem,calc(100%-4rem))]",
  "max-sm:top-0 max-sm:left-0 max-sm:h-dvh max-sm:max-w-none max-sm:translate-x-0 max-sm:translate-y-0 max-sm:rounded-none max-sm:ring-0",
].join(" ");

/** 弹框内一份「位置 + 选择」：两者要一起更新（上传完成时自动勾选并切标签） */
type PickerState = { view: PickerView; selected: AssetDto[] };

/**
 * 画布里的资产库弹框（设计文档 §6.2）：从「个人资产库」或「生成历史」多选素材，确认后交给画布建节点。
 * 顶部来源切换，第二行类型标签（下划线选中态）与搜索；生成历史来源有「时间」「筛选」图标按钮，
 * 个人资产库来源有「上传」（文件归入当前所在文件夹，传完自动勾选并切到该类型标签）。
 * 一次最多选 20 个，满了其余卡片禁用并提示。每次打开都从初始状态开始。手机宽度全屏。
 * @param open 是否打开
 * @param onOpenChange 打开状态变化
 * @param onConfirm 确认：拿到选中的素材（按勾选顺序）；调用后弹框自动关闭
 */
export function AssetPickerDialog({
  open,
  onOpenChange,
  onConfirm,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onConfirm: (assets: AssetDto[]) => void;
}) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className={CONTENT_CLASS} data-slot="asset-picker">
        <DialogTitle className="sr-only">资产库</DialogTitle>
        <DialogDescription className="sr-only">选择要引入画布的素材</DialogDescription>
        {/* 打开时才挂载内容：选择、筛选、搜索、上传队列每次都从初始状态开始，关闭时进行中的上传随之中止 */}
        {open && (
          <PickerBody
            onConfirm={(assets) => {
              onConfirm(assets);
              onOpenChange(false);
            }}
          />
        )}
      </DialogContent>
    </Dialog>
  );
}

function PickerBody({ onConfirm }: { onConfirm: (assets: AssetDto[]) => void }) {
  const [state, setState] = useState<PickerState>({ view: INITIAL_PICKER_VIEW, selected: [] });
  /** 上传完成回调要读「此刻」的位置与选择，且多个文件可能在同一次渲染前接连完成，所以用 ref 跟 state 同步 */
  const stateRef = useRef(state);
  const commit = useCallback((next: PickerState) => {
    stateRef.current = next;
    setState(next);
  }, []);
  const { view, selected } = state;

  const [input, setInput] = useState("");
  const q = useDebouncedValue(input);
  const [sourceFilter, setSourceFilter] = useState(DEFAULT_GENERATED_FILTER.source);
  const [range, setRange] = useState<TimeRange>("all");
  const filter = useMemo<GeneratedFilter>(
    () => ({ source: sourceFilter, range, order: "desc", q }),
    [sourceFilter, range, q],
  );
  const sourceMenu = useCanvasSourceMenu(sourceFilter);

  const folders = useAssetFolders(true);
  const libraryList = useRef<PagedList<LibraryItemDto> | null>(null);
  const fileInput = useRef<HTMLInputElement>(null);

  const uploads = useAssetUploads((item, task) => {
    const current = stateRef.current;
    const done = applyUploadDone(current.view, current.selected, item.asset);
    commit({ view: done.view, selected: done.selected });
    if (done.dropped > 0) {
      toast.info(`已选满，「${item.asset.fileName ?? "素材"}」已上传但没有勾选`);
    }
    folders.adjust(task.folderId, 1);
    folders.reload();
    // 还在看同一个位置、同一种类型：新素材直接插到最前；类型标签换了，列表会按新标签重新加载
    const sameList =
      current.view.source === "library" &&
      task.folderId === current.view.folder &&
      item.asset.kind === current.view.kind &&
      q.trim() === "";
    if (sameList) libraryList.current?.update((prev) => [item, ...prev]);
  });

  const toggle = (asset: AssetDto) =>
    commit({ ...stateRef.current, selected: toggleAsset(stateRef.current.selected, asset) });
  const changeSource = (source: PickerSource) => {
    setInput("");
    commit({ ...stateRef.current, view: switchSource(stateRef.current.view, source) });
  };
  const changeKind = (kind: PickerView["kind"]) =>
    commit({ ...stateRef.current, view: switchKind(stateRef.current.view, kind) });
  const openFolder = (folder: string) => {
    setInput("");
    commit({ ...stateRef.current, view: enterFolder(stateRef.current.view, folder) });
  };
  const goRoot = useCallback(() => {
    setInput("");
    commit({ ...stateRef.current, view: leaveFolder(stateRef.current.view) });
  }, [commit]);
  const clearFilters = () => {
    setSourceFilter(DEFAULT_GENERATED_FILTER.source);
    setRange("all");
    setInput("");
  };

  const summary = describeSelection(selected);
  const library = view.source === "library";

  return (
    <>
      <div className="flex flex-col gap-3 px-4 pt-4">
        <Segmented
          variant="track"
          className="self-start"
          label="资产来源"
          options={SOURCE_OPTIONS}
          value={view.source}
          onChange={changeSource}
        />
        <div className="flex flex-wrap items-center justify-between gap-x-3 gap-y-2 border-b">
          <AssetKindTabs variant="underline" value={view.kind} onChange={changeKind} />
          <div className="flex items-center gap-1 pb-1.5">
            <AssetSearch
              value={input}
              onChange={setInput}
              placeholder={library ? "搜索文件名" : "搜索提示词"}
            />
            {library ? (
              <Button
                variant="outline"
                size="sm"
                onClick={() => fileInput.current?.click()}
                className="ml-1"
              >
                <Upload />
                上传
              </Button>
            ) : (
              <>
                <FilterMenu
                  icon={<Clock />}
                  label="时间"
                  value={range}
                  defaultValue="all"
                  groups={[{ options: RANGE_OPTIONS }]}
                  onChange={(value) => setRange(value as TimeRange)}
                />
                <FilterMenu
                  icon={<ListFilter />}
                  label="筛选"
                  value={sourceMenu.value}
                  defaultValue="all"
                  groups={sourceMenu.groups}
                  onChange={(value) => setSourceFilter(sourceFromValue(value))}
                  onOpenChange={sourceMenu.onOpenChange}
                  footer={sourceMenu.footer}
                />
              </>
            )}
          </div>
        </div>
      </div>

      <input
        ref={fileInput}
        type="file"
        multiple
        accept={UPLOAD_ACCEPT}
        aria-label="选择要上传的文件"
        className="sr-only"
        tabIndex={-1}
        onChange={(event) => {
          const files = Array.from(event.target.files ?? []);
          // 同一批文件连着选两次也得有反应，所以选完就把值清掉
          event.target.value = "";
          if (files.length > 0) uploads.add(files, uploadFolder(stateRef.current.view));
        }}
      />

      {/* 切来源时整块内容淡入：只做 opacity，减少动态效果时不播 */}
      <div
        key={view.source}
        className="animate-in fade-in-0 min-h-0 flex-1 overflow-y-auto px-4 pt-3 pb-3 duration-150 motion-reduce:animate-none"
      >
        {library ? (
          <LibraryPane
            view={view}
            q={q}
            folders={folders}
            tasks={uploads.tasks}
            selected={selected}
            listRef={libraryList}
            onToggle={toggle}
            onOpenFolder={openFolder}
            onLeaveFolder={goRoot}
            onCancelUpload={uploads.cancel}
            onRetryUpload={uploads.retry}
          />
        ) : (
          <HistoryPane
            kind={view.kind}
            filter={filter}
            selected={selected}
            onToggle={toggle}
            onClearFilters={clearFilters}
          />
        )}
      </div>

      <div className="bg-muted/50 flex items-center justify-between gap-3 border-t px-4 py-3 pb-[max(0.75rem,env(safe-area-inset-bottom))]">
        <p aria-live="polite" className="flex min-w-0 flex-wrap items-baseline gap-x-2 text-sm">
          <span className="font-medium tabular-nums">{summary.label}</span>
          {(summary.confirmHint ?? summary.limitHint) && (
            <span className="text-muted-foreground text-xs">
              {summary.confirmHint ?? summary.limitHint}
            </span>
          )}
        </p>
        {/* 禁用的按钮收不到悬停，提示挂在外层 */}
        <span title={summary.confirmHint ?? undefined}>
          <Button disabled={summary.confirmDisabled} onClick={() => onConfirm(selected)}>
            确认
          </Button>
        </span>
      </div>
    </>
  );
}

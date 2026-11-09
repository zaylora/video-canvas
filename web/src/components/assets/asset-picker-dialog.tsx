import { useCallback, useMemo, useRef, useState } from "react";
import { AnimatePresence, motion } from "motion/react";
import { Clock, ListFilter, Upload, X } from "lucide-react";
import { toast } from "sonner";

import type { AssetDto } from "@/api/asset/type";
import type { AssetKind, LibraryItemDto } from "@/api/asset-library/type";
import { Segmented } from "@/components/home/segmented";
import { ChromeButton, ChromeSeparator } from "@/components/canvas/chrome/chrome";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogTitle,
} from "@/components/ui/dialog";
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
import { DURATION, EASE_OUT } from "@/lib/motion";
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

import { AssetCover } from "./asset-card";
import { AssetKindTabs } from "./asset-kind-tabs";
import { HistoryPane, LibraryPane, type PickRules } from "./asset-picker-panes";
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

/**
 * 弹框的外框：半透明清透面板（coding-standards 6.1），桌面居中的大弹框，手机宽度全屏
 * （覆盖 DialogContent 的实心底、居中定位与圆角）
 */
const CONTENT_CLASS = [
  "bg-chrome ring-chrome-border flex flex-col gap-0 overflow-hidden rounded-3xl p-0 shadow-2xl backdrop-blur-2xl backdrop-saturate-150",
  "sm:h-[min(48rem,calc(100dvh-4rem))] sm:max-w-[min(68rem,calc(100%-4rem))]",
  "max-sm:top-0 max-sm:left-0 max-sm:h-dvh max-sm:max-w-none max-sm:translate-x-0 max-sm:translate-y-0 max-sm:rounded-none max-sm:ring-0",
].join(" ");

/** 底部已选托盘里最多摆几张缩略图，多出的折成「+N」 */
const TRAY_MAX = 8;

/** 弹框内一份「位置 + 选择」：两者要一起更新（上传完成时自动勾选并切标签） */
type PickerState = { view: PickerView; selected: AssetDto[] };

/**
 * 选素材弹框的可选项：默认（都不给）就是画布「从资产库添加」的行为，多选最多 20 个、不限种类。
 * 角色卡编辑弹框用它选主图 / 补充图 / 声音：锁定种类、限制个数、标注已用素材。
 */
export type AssetPickerOptions = {
  /** 锁定素材种类：两个来源都只列这一种，隐藏类型标签，弹框内上传也只收这一种 */
  kind?: AssetKind;
  /** 一次最多选几个，缺省 20；1 是单选（点另一个就换成它） */
  limit?: number;
  /** 某个素材不能选的原因（「已是主图」「已添加」）：卡片禁用并标注，不隐藏 */
  reasonOf?: (asset: AssetDto) => string | null;
  /** 弹框标题（读屏用），缺省「资产库」 */
  title?: string;
  /** 确认按钮文字，缺省「确认」 */
  confirmLabel?: string;
};

/**
 * 画布里的资产库弹框（设计文档 §6.2）：从「个人资产库」或「生成历史」多选素材，确认后交给画布建节点。
 * 顶部来源切换，第二行搜索，生成历史来源还有类型标签（下划线选中态）；生成历史来源有「时间」「筛选」图标按钮，
 * 个人资产库来源有「上传」（文件归入当前所在文件夹，传完自动勾选并切到该类型标签）。
 * 一次最多选 20 个，满了其余卡片禁用并提示。每次打开都从初始状态开始。手机宽度全屏。
 * 传 AssetPickerOptions 可锁定种类、改上限（见该类型）。
 * @param open 是否打开
 * @param onOpenChange 打开状态变化
 * @param onConfirm 确认：拿到选中的素材（按勾选顺序）；调用后弹框自动关闭
 */
export function AssetPickerDialog({
  open,
  onOpenChange,
  onConfirm,
  ...options
}: AssetPickerOptions & {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onConfirm: (assets: AssetDto[]) => void;
}) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className={CONTENT_CLASS} showCloseButton={false} data-slot="asset-picker">
        <DialogTitle className="sr-only">{options.title ?? "资产库"}</DialogTitle>
        <DialogDescription className="sr-only">选择要引入的素材</DialogDescription>
        {/* 内容随弹框外壳一起挂载和卸载（Base UI 在退出动画结束后才卸载）：
            选择、筛选、搜索、上传队列每次打开都从初始状态开始，关闭时进行中的上传随之中止，
            关闭动画期间内容不会先消失 */}
        <PickerBody
          {...options}
          onConfirm={(assets) => {
            onConfirm(assets);
            onOpenChange(false);
          }}
        />
      </DialogContent>
    </Dialog>
  );
}

function PickerBody({
  kind,
  limit,
  reasonOf,
  confirmLabel = "确认",
  onConfirm,
}: AssetPickerOptions & { onConfirm: (assets: AssetDto[]) => void }) {
  const rules = useMemo<PickRules>(() => ({ limit, reasonOf }), [limit, reasonOf]);
  const [state, setState] = useState<PickerState>({
    view: kind ? { ...INITIAL_PICKER_VIEW, kind } : INITIAL_PICKER_VIEW,
    selected: [],
  });
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
    const done = applyUploadDone(current.selected, item.asset, limit);
    commit({ view: current.view, selected: done.selected });
    if (done.dropped > 0) {
      toast.info(`已选满，「${item.asset.fileName ?? "素材"}」已上传但没有勾选`);
    }
    folders.adjust(task.folderId, 1);
    folders.reload();
    // 还在看同一个位置：新素材直接插到最前
    const sameList =
      current.view.source === "library" && task.folderId === current.view.folder && q.trim() === "";
    if (sameList) libraryList.current?.update((prev) => [item, ...prev]);
  });

  const toggle = (asset: AssetDto) =>
    commit({ ...stateRef.current, selected: toggleAsset(stateRef.current.selected, asset, limit) });
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

  const summary = describeSelection(selected, limit);
  const library = view.source === "library";

  return (
    <>
      <div className="flex flex-col gap-3 px-5 pt-5 pb-1">
        <div className="flex flex-wrap items-center gap-2">
          <Segmented
            variant="chrome"
            label="资产来源"
            options={SOURCE_OPTIONS}
            value={view.source}
            onChange={changeSource}
          />
          <div className="ml-auto flex items-center gap-1.5">
            <AssetSearch
              value={input}
              onChange={setInput}
              placeholder={library ? "搜索文件名" : "搜索提示词"}
            />
            {library ? (
              <button
                type="button"
                onClick={() => fileInput.current?.click()}
                className="bg-chrome-hover ring-chrome-border text-muted-foreground hover:text-foreground focus-visible:ring-ring/60 inline-flex h-9 shrink-0 items-center gap-1.5 rounded-full px-4 text-[13px] ring-1 transition-colors outline-none focus-visible:ring-2 [&_svg]:size-4"
              >
                <Upload />
                上传
              </button>
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
            <ChromeSeparator className="max-sm:hidden" />
            <DialogClose render={<ChromeButton aria-label="关闭" className="size-9" />}>
              <X />
            </DialogClose>
          </div>
        </div>
        {/* 个人资产库不分类型；生成历史的接口按类型取，所以只有它有标签 */}
        {!library && !kind && <AssetKindTabs value={view.kind} onChange={changeKind} />}
      </div>

      <input
        ref={fileInput}
        type="file"
        multiple
        accept={kind ? `${kind}/*` : UPLOAD_ACCEPT}
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
        className="animate-in fade-in-0 min-h-0 flex-1 overflow-y-auto px-5 pt-3 pb-4 duration-150 [scrollbar-width:thin] motion-reduce:animate-none"
      >
        {library ? (
          <LibraryPane
            view={view}
            q={q}
            folders={folders}
            tasks={uploads.tasks}
            selected={selected}
            kind={kind}
            rules={rules}
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
            rules={rules}
            onToggle={toggle}
            onClearFilters={clearFilters}
          />
        )}
      </div>

      <div className="px-3 pb-[max(0.75rem,env(safe-area-inset-bottom))]">
        <div className="bg-chrome-hover ring-chrome-border flex h-15 items-center gap-3 rounded-full py-2 pr-2 pl-5 ring-1">
          <SelectionTray selected={selected} onRemove={toggle} />
          <p
            aria-live="polite"
            className="flex min-w-0 flex-1 flex-wrap items-baseline gap-x-2 text-sm"
          >
            <span className="font-medium tabular-nums">{summary.label}</span>
            {(summary.confirmHint ?? summary.limitHint) && (
              <span className="text-muted-foreground text-xs">
                {summary.confirmHint ?? summary.limitHint}
              </span>
            )}
          </p>
          {/* 禁用的按钮收不到悬停，提示挂在外层 */}
          <span title={summary.confirmHint ?? undefined}>
            <Button
              disabled={summary.confirmDisabled}
              onClick={() => onConfirm(selected)}
              className="h-11 rounded-full px-6"
            >
              {confirmLabel}
            </Button>
          </span>
        </div>
      </div>
    </>
  );
}

/**
 * 底部栏左侧的已选托盘：按勾选顺序排已选素材的小缩略图，点一下取消勾选；
 * 超过 TRAY_MAX 张时只摆前几张，剩下的折成「+N」。没选时不占位。
 * @param selected 当前选择，按勾选顺序
 * @param onRemove 取消勾选某个素材
 */
function SelectionTray({
  selected,
  onRemove,
}: {
  selected: AssetDto[];
  onRemove: (asset: AssetDto) => void;
}) {
  if (selected.length === 0) return null;
  const shown = selected.slice(0, TRAY_MAX);
  const rest = selected.length - shown.length;
  return (
    <ul aria-label="已选素材" className="flex shrink-0 items-center -space-x-2 max-sm:hidden">
      <AnimatePresence initial={false}>
        {shown.map((asset) => (
          <motion.li
            key={asset.id}
            layout
            initial={{ opacity: 0, scale: 0.8 }}
            animate={{ opacity: 1, scale: 1 }}
            exit={{ opacity: 0, scale: 0.8, transition: { duration: DURATION.exit } }}
            transition={{ duration: DURATION.base, ease: EASE_OUT }}
            className="relative size-10 hover:z-10"
          >
            <button
              type="button"
              aria-label={`取消选择 ${asset.fileName ?? "素材"}`}
              title={asset.fileName ?? undefined}
              onClick={() => onRemove(asset)}
              className="group/tray bg-muted ring-background focus-visible:ring-ring relative block size-full overflow-hidden rounded-xl ring-2 transition-transform duration-150 outline-none hover:-translate-y-0.5 motion-reduce:transition-none"
            >
              <AssetCover asset={asset} />
              <span className="bg-cover-scrim text-cover-foreground absolute inset-0 grid place-items-center opacity-0 transition-opacity duration-120 group-hover/tray:opacity-100 group-focus-visible/tray:opacity-100">
                <X className="size-4" />
              </span>
            </button>
          </motion.li>
        ))}
      </AnimatePresence>
      {rest > 0 && (
        <li className="bg-muted text-muted-foreground ring-background relative grid size-10 place-items-center rounded-xl text-xs font-medium tabular-nums ring-2">
          +{rest}
        </li>
      )}
    </ul>
  );
}

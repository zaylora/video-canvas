import { motion } from "motion/react";
import {
  Check,
  ExternalLink,
  FolderInput,
  FolderPlus,
  Library,
  Music2,
  Unlink,
} from "lucide-react";

import type { AssetDto } from "@/api/asset/type";
import type { GeneratedItemDto, LibraryItemDto } from "@/api/asset-library/type";
import { MediaPreview, VideoPoster } from "@/components/canvas/media-preview";
import { DropdownMenuItem } from "@/components/ui/dropdown-menu";
import { TAP_CARD } from "@/lib/motion";
import { cn } from "@/lib/utils";
import { formatMediaDuration } from "@/utils/canvas/media-lod";
import { assetAspect } from "@/utils/assets/aspect";
import { formatAssetDay } from "@/utils/assets/format";

import { CardMenu } from "./card-menu";

/** 视频卡用 16:10，图片和音频用正方形：和旧资产页一致 */
const aspectOf = (kind: AssetDto["kind"]) =>
  kind === "video" ? "aspect-[16/10]" : "aspect-square";

/**
 * 素材封面：图片用 `?v=thumb`，视频用 `?v=poster`（取不到就显示占位图标），音频没有画面，显示音符占位。
 * 填满父级盒子，画幅由父级定。
 * @param asset 素材
 */
export function AssetCover({ asset }: { asset: AssetDto }) {
  if (asset.kind === "image") {
    return (
      <div className="absolute inset-0">
        <MediaPreview src={asset.url} mode="thumb" fit="cover" alt={asset.fileName ?? ""} />
      </div>
    );
  }
  if (asset.kind === "video") {
    return (
      <div className="absolute inset-0">
        <VideoPoster src={asset.url} />
      </div>
    );
  }
  return (
    <div className="bg-muted text-muted-foreground/60 absolute inset-0 grid place-items-center">
      <Music2 className="size-1/4" />
    </div>
  );
}

/**
 * 卡片画幅：natural（资产页瀑布流）按素材真实宽高，用内联 aspect-ratio；否则沿用固定画幅（画布资产库弹框）。
 * 返回 className 和 style，两者只会给一个。
 */
const frameOf = (asset: AssetDto, natural?: boolean) =>
  natural
    ? { className: "rounded-2xl", style: { aspectRatio: assetAspect(asset) } }
    : { className: aspectOf(asset.kind), style: undefined };

/** 卡片外框共用的类：封面底色、圆角、聚焦描边 */
const CARD_FRAME = "group/card bg-muted relative min-w-0 overflow-hidden rounded-xl";

/** 封面上的整张点击区：聚焦时描边 */
const COVER_BUTTON =
  "focus-visible:ring-ring/60 absolute inset-0 z-0 rounded-[inherit] outline-none focus-visible:ring-3 focus-visible:ring-inset";

/**
 * 卡片的选择模式（画布资产库弹框）：点卡片是勾选 / 取消，不再预览，⋯ 菜单也收起。
 * disabled 是已选满上限时其余卡片的状态；已选的卡片永远可点（用来取消）。
 */
export type CardSelect = {
  /** 是否已勾选 */
  selected: boolean;
  /** 不能再勾选（已选满），已选中的卡片不会是 true */
  disabled: boolean;
  /** 不能选的原因（「已是主图」「已添加」「已达上限」），显示在封面上；不隐藏卡片 */
  reason?: string | null;
  /** 点击卡片：勾选或取消 */
  onToggle: () => void;
};

/** 选择模式下卡片外框的状态：选中描边、禁用变淡 */
const selectFrame = (select?: CardSelect) =>
  select &&
  cn(
    "transition-opacity duration-150 motion-reduce:transition-none",
    select.selected && "ring-foreground ring-2",
    select.disabled && "opacity-45",
  );

/** 选择模式下卡片外框的数据属性，样式和测试都靠它认 */
const selectAttrs = (select?: CardSelect) =>
  select && {
    "data-selected": select.selected || undefined,
    "data-disabled": select.disabled || undefined,
  };

/** 选择模式下左上角的勾选框：只是状态指示，点击落在整张卡片的点击区上；始终可见，触屏也一眼看得出 */
function SelectBox({ selected }: { selected: boolean }) {
  return (
    <span
      data-slot="select-box"
      aria-hidden
      className={cn(
        "pointer-events-none absolute top-2 left-2 z-10 grid size-5 place-items-center rounded-full transition-colors duration-120 motion-reduce:transition-none",
        selected
          ? "bg-foreground text-background"
          : "bg-cover-scrim ring-cover-foreground/70 ring-1",
      )}
    >
      <Check
        className={cn(
          "size-3.5 transition-opacity duration-120 motion-reduce:transition-none",
          selected ? "opacity-100" : "opacity-0",
        )}
      />
    </span>
  );
}

/** 选择模式下标注不能选的原因：压在封面右下，和左上的勾选框、右上的时长错开 */
function ReasonChip({ reason }: { reason?: string | null }) {
  if (!reason) return null;
  return (
    <span
      data-slot="select-reason"
      className="bg-cover-scrim text-cover-foreground pointer-events-none absolute right-2 bottom-9 z-10 rounded-md px-1.5 py-0.5 text-[11px]"
    >
      {reason}
    </span>
  );
}

/** 左上角的小角标（时长、已在资产库）：压在封面上，用遮罩底保证深浅封面都看得清 */
const CHIP =
  "bg-cover-scrim text-cover-foreground inline-flex h-5 items-center gap-1 rounded-md px-1.5 text-[11px] tabular-nums";

/**
 * 生成历史的一张卡：悬停时底部浮出提示词与「模型 · 日期」；视频带时长角标；
 * 已在资产库的显示角标；⋯ 菜单里有「加入资产库…」与「在画布中打开」。不提供删除。
 * @param item 生成历史条目
 * @param onPreview 点击卡片：打开只读预览
 * @param onAddToLibrary 菜单里的「加入资产库…」，已在库里时不出现
 * @param onOpenCanvas 菜单里的「在画布中打开」，来源画布存在时才可点
 * @param select 选择模式：给了就变成可勾选的卡片，预览与 ⋯ 菜单都收起（此时上面三个回调可省略）
 * @param natural 按素材真实画幅显示（资产页瀑布流）
 */
export function HistoryAssetCard({
  item,
  onPreview,
  onAddToLibrary,
  onOpenCanvas,
  select,
  natural,
}: {
  item: GeneratedItemDto;
  natural?: boolean;
  onPreview?: () => void;
  onAddToLibrary?: () => void;
  onOpenCanvas?: (canvasId: string) => void;
  select?: CardSelect;
}) {
  const { asset, canvas } = item;
  const inLibrary = item.libraryItemId !== null;
  const duration = asset.kind === "image" ? null : formatMediaDuration(asset.durationMs);
  const label = item.prompt || asset.fileName || "生成结果";

  return (
    <motion.div
      data-slot="history-card"
      data-in-library={inLibrary || undefined}
      {...selectAttrs(select)}
      whileTap={select?.disabled ? undefined : TAP_CARD}
      className={cn(CARD_FRAME, frameOf(asset, natural).className, selectFrame(select))}
      style={frameOf(asset, natural).style}
    >
      <AssetCover asset={asset} />
      <button
        type="button"
        aria-label={label}
        aria-pressed={select ? select.selected : undefined}
        disabled={select?.disabled}
        onClick={select ? select.onToggle : onPreview}
        className={cn(COVER_BUTTON, select?.disabled && "cursor-not-allowed")}
      />
      {select && <SelectBox selected={select.selected} />}
      {select && <ReasonChip reason={select.reason} />}
      {(duration || inLibrary) && (
        <span
          className={cn(
            "pointer-events-none absolute top-2 flex flex-col gap-1",
            select ? "right-2 items-end" : "left-2 items-start",
          )}
        >
          {duration && <span className={CHIP}>{duration}</span>}
          {inLibrary && (
            <span data-slot="in-library-badge" className={CHIP}>
              <Library className="size-3" />
              已在资产库
            </span>
          )}
        </span>
      )}
      <span className="from-cover-scrim text-cover-foreground pointer-events-none absolute inset-x-0 bottom-0 bg-linear-to-t to-transparent px-2.5 pt-7 pb-2 text-xs opacity-0 transition-opacity duration-120 group-focus-within/card:opacity-100 group-hover/card:opacity-100">
        <span className="line-clamp-2 leading-snug">{item.prompt}</span>
        <span className="text-[11px] opacity-70">
          {[item.modelLabel, formatAssetDay(item.createdAt)].filter(Boolean).join(" · ")}
        </span>
      </span>
      {!select && onAddToLibrary && onOpenCanvas && (!inLibrary || canvas) && (
        <CardMenu>
          {!inLibrary && (
            <DropdownMenuItem onClick={onAddToLibrary}>
              <FolderPlus />
              加入资产库…
            </DropdownMenuItem>
          )}
          {canvas &&
            (canvas.deleted ? (
              <DropdownMenuItem disabled>
                <Unlink />
                已删除的画布
              </DropdownMenuItem>
            ) : (
              <DropdownMenuItem onClick={() => onOpenCanvas(canvas.id)}>
                <ExternalLink />
                在画布中打开
              </DropdownMenuItem>
            ))}
        </CardMenu>
      )}
    </motion.div>
  );
}

/**
 * 个人资产库的一张素材卡：封面 + 底部常显的文件名；⋯ 菜单里有「移动到…」与「移出资产库」。
 * @param item 资产库条目
 * @param onPreview 点击卡片：打开预览
 * @param onMove 菜单里的「移动到…」
 * @param onRemove 菜单里的「移出资产库」
 * @param select 选择模式：给了就变成可勾选的卡片，预览与 ⋯ 菜单都收起（此时上面三个回调可省略）
 * @param natural 按素材真实画幅显示（资产页瀑布流）
 */
export function LibraryAssetCard({
  item,
  onPreview,
  onMove,
  onRemove,
  select,
  natural,
}: {
  item: LibraryItemDto;
  natural?: boolean;
  onPreview?: () => void;
  onMove?: () => void;
  onRemove?: () => void;
  select?: CardSelect;
}) {
  const { asset } = item;
  const name = asset.fileName ?? "未命名";
  const duration = asset.kind === "image" ? null : formatMediaDuration(asset.durationMs);

  return (
    <motion.div
      data-slot="library-card"
      {...selectAttrs(select)}
      whileTap={select?.disabled ? undefined : TAP_CARD}
      className={cn(CARD_FRAME, frameOf(asset, natural).className, selectFrame(select))}
      style={frameOf(asset, natural).style}
    >
      <AssetCover asset={asset} />
      <button
        type="button"
        aria-label={name}
        aria-pressed={select ? select.selected : undefined}
        disabled={select?.disabled}
        onClick={select ? select.onToggle : onPreview}
        className={cn(COVER_BUTTON, select?.disabled && "cursor-not-allowed")}
      />
      {select && <SelectBox selected={select.selected} />}
      {select && <ReasonChip reason={select.reason} />}
      {duration && (
        <span
          className={cn(CHIP, "pointer-events-none absolute top-2", select ? "right-2" : "left-2")}
        >
          {duration}
        </span>
      )}
      <span className="from-cover-scrim text-cover-foreground pointer-events-none absolute inset-x-0 bottom-0 bg-linear-to-t to-transparent px-2.5 pt-7 pb-2 text-xs">
        <span className="block truncate font-medium">{name}</span>
      </span>
      {!select && onMove && onRemove && (
        <CardMenu>
          <DropdownMenuItem onClick={onMove}>
            <FolderInput />
            移动到…
          </DropdownMenuItem>
          <DropdownMenuItem onClick={onRemove}>
            <Unlink />
            移出资产库
          </DropdownMenuItem>
        </CardMenu>
      )}
    </motion.div>
  );
}

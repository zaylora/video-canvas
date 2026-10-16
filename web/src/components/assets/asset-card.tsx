import { motion } from "motion/react";
import { ExternalLink, FolderInput, FolderPlus, Library, Music2, Unlink } from "lucide-react";

import type { AssetDto } from "@/api/asset/type";
import type { GeneratedItemDto, LibraryItemDto } from "@/api/asset-library/type";
import { MediaPreview, VideoPoster } from "@/components/canvas/media-preview";
import { DropdownMenuItem } from "@/components/ui/dropdown-menu";
import { TAP_CARD } from "@/lib/motion";
import { cn } from "@/lib/utils";
import { formatMediaDuration } from "@/utils/canvas/media-lod";
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

/** 卡片外框共用的类：封面底色、圆角、聚焦描边 */
const CARD_FRAME = "group/card bg-muted relative min-w-0 overflow-hidden rounded-xl";

/** 封面上的整张点击区：聚焦时描边 */
const COVER_BUTTON =
  "focus-visible:ring-ring/60 absolute inset-0 z-0 rounded-[inherit] outline-none focus-visible:ring-3 focus-visible:ring-inset";

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
 */
export function HistoryAssetCard({
  item,
  onPreview,
  onAddToLibrary,
  onOpenCanvas,
}: {
  item: GeneratedItemDto;
  onPreview: () => void;
  onAddToLibrary: () => void;
  onOpenCanvas: (canvasId: string) => void;
}) {
  const { asset, canvas } = item;
  const inLibrary = item.libraryItemId !== null;
  const duration = asset.kind === "image" ? null : formatMediaDuration(asset.durationMs);
  const label = item.prompt || asset.fileName || "生成结果";

  return (
    <motion.div
      data-slot="history-card"
      data-in-library={inLibrary || undefined}
      whileTap={TAP_CARD}
      className={cn(CARD_FRAME, aspectOf(asset.kind))}
    >
      <AssetCover asset={asset} />
      <button type="button" aria-label={label} onClick={onPreview} className={COVER_BUTTON} />
      {(duration || inLibrary) && (
        <span className="pointer-events-none absolute top-2 left-2 flex flex-col items-start gap-1">
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
      {(!inLibrary || canvas) && (
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
 */
export function LibraryAssetCard({
  item,
  onPreview,
  onMove,
  onRemove,
}: {
  item: LibraryItemDto;
  onPreview: () => void;
  onMove: () => void;
  onRemove: () => void;
}) {
  const { asset } = item;
  const name = asset.fileName ?? "未命名";
  const duration = asset.kind === "image" ? null : formatMediaDuration(asset.durationMs);

  return (
    <motion.div
      data-slot="library-card"
      whileTap={TAP_CARD}
      className={cn(CARD_FRAME, aspectOf(asset.kind))}
    >
      <AssetCover asset={asset} />
      <button type="button" aria-label={name} onClick={onPreview} className={COVER_BUTTON} />
      {duration && (
        <span className={cn(CHIP, "pointer-events-none absolute top-2 left-2")}>{duration}</span>
      )}
      <span className="from-cover-scrim text-cover-foreground pointer-events-none absolute inset-x-0 bottom-0 bg-linear-to-t to-transparent px-2.5 pt-7 pb-2 text-xs">
        <span className="block truncate font-medium">{name}</span>
      </span>
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
    </motion.div>
  );
}

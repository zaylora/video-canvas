import { useRef, type ReactNode } from "react";
import { motion } from "motion/react";
import { Clapperboard, Loader2, PackagePlus, Play, RotateCw, UserRoundPlus, X } from "lucide-react";

import type { MarketingSlot } from "@/store/marketing-composer";
import { TAP } from "@/lib/motion";
import { cn } from "@/lib/utils";

import { ProductStack } from "./product-stack";

/** 商品图最多几张 */
const MAX_PRODUCTS = 4;

/** 一个上传块的尺寸：64×80，和原型一致 */
const TILE = "relative h-20 w-16 shrink-0";

/** 空的上传块：虚线描边的半透明块，图标在上、文字在下 */
function AddTile({
  icon,
  label,
  title,
  disabled,
  onClick,
}: {
  icon: ReactNode;
  label: string;
  title: string;
  disabled?: boolean;
  onClick: () => void;
}) {
  return (
    <motion.button
      type="button"
      whileTap={disabled ? undefined : TAP}
      disabled={disabled}
      title={title}
      aria-label={title}
      onClick={onClick}
      className={cn(
        TILE,
        "bg-chrome-hover text-muted-foreground flex flex-col items-center justify-center gap-1.5 rounded-xl border border-dashed border-[color-mix(in_oklch,var(--foreground)_18%,transparent)] text-xs outline-none transition-colors duration-150",
        "hover:text-foreground focus-visible:ring-ring/50 focus-visible:ring-3 disabled:cursor-not-allowed disabled:opacity-45 disabled:hover:text-muted-foreground [&_svg]:size-5",
      )}
    >
      {icon}
      {label}
    </motion.button>
  );
}

/** 已有素材的缩略块：底部是名称标签，右上是移除，上传中转圈，失败可移除重来 */
function FilledTile({
  slot,
  caption,
  video,
  stacked,
  count,
  fixed,
  onClick,
  onRemove,
}: {
  slot: Pick<MarketingSlot, "url" | "status">;
  caption: string;
  video?: boolean;
  /** 同一商品的多张图叠成一摞 */
  stacked?: boolean;
  /** 叠放时右下角的张数 */
  count?: number;
  /** 固定的素材（详情页的商品图）：不能移除 */
  fixed?: boolean;
  onClick?: () => void;
  onRemove?: () => void;
}) {
  const failed = slot.status === "error";
  return (
    <div className={cn(TILE, "group/tile")}>
      {stacked && (
        <div className="bg-chrome-hover ring-chrome-border absolute inset-0 translate-x-1 -translate-y-1 rotate-6 rounded-xl ring-1" />
      )}
      <button
        type="button"
        onClick={onClick}
        disabled={!onClick}
        aria-label={caption}
        className={cn(
          "bg-muted ring-chrome-border focus-visible:ring-ring/50 absolute inset-0 overflow-hidden rounded-xl ring-1 outline-none focus-visible:ring-3",
          failed && "ring-destructive ring-2",
          !onClick && "cursor-default",
        )}
      >
        {slot.url &&
          (video ? (
            <video src={slot.url} preload="metadata" muted className="size-full object-cover" />
          ) : (
            <img src={slot.url} alt="" className="size-full object-cover" />
          ))}
        {video && (
          <span className="absolute inset-0 grid place-items-center text-white [&_svg]:size-5 [&_svg]:fill-white/90">
            <Play />
          </span>
        )}
        <span className="from-cover-scrim text-cover-foreground absolute inset-x-0 bottom-0 bg-linear-to-t to-transparent px-1 pt-3 pb-1 text-[11px] leading-tight">
          {failed ? "上传失败" : caption}
        </span>
        {slot.status === "uploading" && (
          <span className="bg-cover-scrim/60 text-cover-foreground absolute inset-0 grid place-items-center">
            <Loader2 className="size-4.5 animate-spin" />
          </span>
        )}
        {failed && (
          <span className="bg-cover-scrim/50 text-cover-foreground absolute inset-0 grid place-items-center">
            <RotateCw className="size-4.5" />
          </span>
        )}
      </button>
      {count !== undefined && count > 1 && (
        <span className="bg-foreground text-background absolute right-1 bottom-6 grid min-w-4 place-items-center rounded-full px-1 text-[10px] leading-4 tabular-nums">
          {count}
        </span>
      )}
      {!fixed && onRemove && (
        <button
          type="button"
          onClick={onRemove}
          aria-label={`移除${caption}`}
          className="bg-cover-scrim text-cover-foreground focus-visible:ring-ring/50 absolute -top-1.5 -right-1.5 grid size-5 place-items-center rounded-full opacity-0 outline-none transition-opacity duration-150 group-focus-within/tile:opacity-100 group-hover/tile:opacity-100 focus-visible:ring-2 max-md:opacity-100"
        >
          <X className="size-3" />
        </button>
      )}
    </div>
  );
}

/**
 * 生成框左侧的三个上传块：商品（必填，1–4 张同一商品，叠成一摞，点缩略图再加一张）、模特（可选）、参考视频（可选）。
 * 详情页的 dock 里商品块固定为项目的商品图（不能增删）；模型不收视频参考时参考块禁用，悬停写原因。
 * @param dock 详情页底部的 dock
 * @param products 列表页已上传的商品图
 * @param fixedProducts dock 里项目的商品图
 * @param model 已选的模特图
 * @param projectModelUrl dock 里项目已有的模特图地址
 * @param refVideo 已选的参考视频
 * @param refDisabledReason 参考视频不可用的原因；可用为 null
 * @param refMaxHint 参考块的悬停提示（可用时）
 */
export function ComposerSlots({
  dock,
  products,
  fixedProducts,
  model,
  projectModelUrl,
  refVideo,
  refDisabledReason,
  refHint,
  onAddProducts,
  onRemoveProduct,
  onPickModel,
  onClearModel,
  onPickRef,
  onClearRef,
}: {
  dock: boolean;
  products: MarketingSlot[];
  fixedProducts: Array<{ id: string; url: string }>;
  model: MarketingSlot | null;
  projectModelUrl?: string;
  refVideo: MarketingSlot | null;
  refDisabledReason: string | null;
  refHint: string;
  onAddProducts: (files: File[]) => void;
  onRemoveProduct: (id: string) => void;
  onPickModel: (file: File) => void;
  onClearModel: () => void;
  onPickRef: (file: File) => void;
  onClearRef: () => void;
}) {
  const productInput = useRef<HTMLInputElement>(null);
  const modelInput = useRef<HTMLInputElement>(null);
  const refInput = useRef<HTMLInputElement>(null);
  /** 选完文件后清空 input，同一个文件才能再选一次 */
  const take = (input: HTMLInputElement, handler: (files: File[]) => void) => {
    const files = Array.from(input.files ?? []);
    input.value = "";
    if (files.length > 0) handler(files);
  };

  const shownProducts = dock ? fixedProducts : products;
  const productStatus: MarketingSlot["status"] = products.some((item) => item.status === "error")
    ? "error"
    : products.some((item) => item.status === "uploading")
      ? "uploading"
      : "done";
  const modelUrl = model?.url ?? projectModelUrl;

  return (
    <div data-slot="marketing-slots" className="flex shrink-0 items-start gap-3">
      <input
        ref={productInput}
        type="file"
        accept="image/*"
        multiple
        hidden
        onChange={(event) => take(event.currentTarget, onAddProducts)}
      />
      <input
        ref={modelInput}
        type="file"
        accept="image/*"
        hidden
        onChange={(event) => take(event.currentTarget, (files) => onPickModel(files[0]))}
      />
      <input
        ref={refInput}
        type="file"
        accept="video/*"
        hidden
        onChange={(event) => take(event.currentTarget, (files) => onPickRef(files[0]))}
      />

      {dock && shownProducts.length > 0 ? (
        <FilledTile
          slot={{ url: shownProducts[0].url, status: productStatus }}
          caption="商品"
          stacked={shownProducts.length > 1}
          count={shownProducts.length}
          fixed
        />
      ) : products.length > 0 ? (
        <ProductStack
          items={products.map((item) => ({ id: item.id, url: item.url, status: item.status }))}
          max={MAX_PRODUCTS}
          onAdd={onAddProducts}
          onRemove={onRemoveProduct}
        />
      ) : (
        <AddTile
          icon={<PackagePlus />}
          label="商品"
          title="上传商品图（必填，1–4 张同一商品）"
          onClick={() => productInput.current?.click()}
        />
      )}

      {modelUrl ? (
        <FilledTile
          slot={{ url: modelUrl, status: model?.status ?? "done" }}
          caption="模特"
          onClick={() => modelInput.current?.click()}
          onRemove={model ? onClearModel : undefined}
        />
      ) : (
        <AddTile
          icon={<UserRoundPlus />}
          label="模特"
          title="上传模特图（可选）"
          onClick={() => modelInput.current?.click()}
        />
      )}

      {refVideo ? (
        <FilledTile
          slot={refVideo}
          caption={refVideo.durationSec ? `参考 ${Math.round(refVideo.durationSec)}s` : "参考"}
          video
          onRemove={onClearRef}
        />
      ) : (
        <AddTile
          icon={<Clapperboard />}
          label="参考"
          title={refDisabledReason ?? refHint}
          disabled={!!refDisabledReason}
          onClick={() => refInput.current?.click()}
        />
      )}
    </div>
  );
}

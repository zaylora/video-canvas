import { useRef, useState } from "react";
import { toast } from "sonner";
import {
  BadgeCheck,
  ChevronDown,
  ChevronUp,
  Loader2,
  MousePointerClick,
  Package,
  Pencil,
  RefreshCw,
  ScanEye,
  ShieldBan,
  type LucideIcon,
} from "lucide-react";

import { uploadAsset } from "@/api/asset";
import type { AssetRef, ProductFacts, Project, Round } from "@/api/marketing/type";
import { ProductStack } from "@/components/marketing/product-stack";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Textarea } from "@/components/ui/textarea";
import { cn } from "@/lib/utils";
import { buildFacts } from "@/utils/marketing/facts";
import { toAssetNumber } from "@/utils/tasks/capabilities";

import type { FactsSave } from "./use-actions";

/** 每段超过几条折叠成「+N 条」 */
const FOLD_AFTER = 4;

/** 商品图最多几张 */
const MAX_PRODUCTS = 4;

/** 事实卡的一段：图标小标题 + 列表；条目多时先收起，点「+N 条」展开。busy 时标题旁转圈（重新识别中） */
function FactSection({
  icon: Icon,
  title,
  items,
  busy,
}: {
  icon: LucideIcon;
  title: string;
  items: string[];
  busy?: boolean;
}) {
  const [expanded, setExpanded] = useState(false);
  if (items.length === 0 && !busy) return null;
  const folded = !expanded && items.length > FOLD_AFTER;
  const shown = folded ? items.slice(0, FOLD_AFTER) : items;
  return (
    <div>
      <h4 className="text-muted-foreground flex items-center gap-1.5 text-[11.5px] font-medium [&_svg]:size-3.5">
        <Icon />
        {title}
        {busy && <Loader2 className="text-status-running animate-spin" aria-label="重新识别中" />}
      </h4>
      <ul className={cn("mt-1 grid gap-0.75 text-[12.5px] leading-relaxed", busy && "opacity-50")}>
        {shown.map((item) => (
          <li
            key={item}
            className="before:bg-muted-foreground/60 relative pl-2.5 before:absolute before:top-[0.68em] before:left-0 before:size-1 before:rounded-full"
          >
            {item}
          </li>
        ))}
      </ul>
      {folded && (
        <button
          type="button"
          onClick={() => setExpanded(true)}
          className="text-muted-foreground hover:text-foreground mt-0.5 text-xs"
        >
          +{items.length - FOLD_AFTER} 条
        </button>
      )}
    </div>
  );
}

/** 弹窗里的一张素材：已有的直接带素材 ID，新选的先本地预览再上传 */
interface ImageTile {
  key: string;
  assetId: number | null;
  url: string;
  status: "done" | "uploading" | "error";
}

const tileOf = (asset: AssetRef): ImageTile => ({
  key: asset.id,
  assetId: toAssetNumber(asset.id),
  url: asset.url,
  status: "done",
});

/** 选好的文件放到哪：加一张商品图、换模特图 */
type PickTarget = "add" | "model";

/** 弹窗里的模特图块：点一下换图 */
function EditTile({
  tile,
  caption,
  disabled,
  onPick,
}: {
  tile: ImageTile;
  caption: string;
  disabled: boolean;
  onPick: () => void;
}) {
  return (
    <div className="relative h-16 w-13 shrink-0">
      <button
        type="button"
        disabled={disabled}
        onClick={onPick}
        aria-label={`换${caption}`}
        title={`换${caption}`}
        className={cn(
          "bg-muted ring-chrome-border focus-visible:ring-ring/50 absolute inset-0 overflow-hidden rounded-xl ring-1 outline-none focus-visible:ring-2 disabled:cursor-not-allowed",
          tile.status === "error" && "ring-destructive ring-2",
        )}
      >
        <img src={tile.url} alt="" className="size-full object-cover" />
        <span className="bg-cover-scrim text-cover-foreground absolute bottom-0.5 left-0.5 rounded-full px-1 text-[9px] leading-3.5">
          {tile.status === "error" ? "上传失败" : caption}
        </span>
        {tile.status === "uploading" && (
          <span className="bg-cover-scrim/60 text-cover-foreground absolute inset-0 grid place-items-center">
            <Loader2 className="size-4 animate-spin" />
          </span>
        )}
      </button>
    </div>
  );
}

/**
 * 商品信息编辑弹窗：商品图（项目级，1–4 张）、模特图（当前这一轮，只能换不能加减）、
 * 商品名、外观锚点、卖点、禁用说法、引导下单。保存后只影响之后的出片。
 * 外观锚点旁的「按商品图重新识别」先保存，再在最新一轮重跑事实卡环节，只替换锚点。
 * 有轮次在写脚本时素材不能改，也不能重新识别（事实卡和脚本正读这些图）。
 */
export function FactDialog({
  facts,
  productAssets,
  modelAsset,
  imagesLocked,
  open,
  busy,
  onOpenChange,
  onSave,
  onRefresh,
}: {
  facts: ProductFacts;
  productAssets: AssetRef[];
  /** 当前这一轮的模特图；没有为 null（不能在这里加） */
  modelAsset: AssetRef | null;
  /** 有轮次在写脚本 */
  imagesLocked: boolean;
  open: boolean;
  busy: boolean;
  onOpenChange: (open: boolean) => void;
  onSave: (save: Omit<FactsSave, "round">) => void;
  onRefresh: (save: Omit<FactsSave, "round">) => void;
}) {
  const [edit, setEdit] = useState({
    name: facts.name,
    anchors: facts.anchors.join("\n"),
    selling: facts.sellingPoints.map((item) => item.text).join("\n"),
    prohibited: facts.prohibited.join("\n"),
    cta: facts.cta,
  });
  const [products, setProducts] = useState<ImageTile[]>(() => productAssets.map(tileOf));
  const [model, setModel] = useState<ImageTile | null>(() =>
    modelAsset ? tileOf(modelAsset) : null,
  );
  const fileInput = useRef<HTMLInputElement>(null);

  const field = (key: keyof typeof edit) => ({
    value: edit[key],
    onChange: (event: { target: { value: string } }) =>
      setEdit((prev) => ({ ...prev, [key]: event.target.value })),
  });

  /** 选好文件：先放本地预览占位，上传完换成素材地址；失败标红，可点开再换 */
  const onFile = async (file: File | undefined, where: PickTarget) => {
    if (!file) return;
    if (!file.type.startsWith("image/")) {
      toast.error("只支持图片");
      return;
    }
    const key = `new-${crypto.randomUUID()}`;
    const preview = URL.createObjectURL(file);
    const tile: ImageTile = { key, assetId: null, url: preview, status: "uploading" };
    const patch = (next: Partial<ImageTile>) => {
      if (where === "model") {
        setModel((prev) => (prev?.key === key ? { ...prev, ...next } : prev));
      } else {
        setProducts((prev) => prev.map((item) => (item.key === key ? { ...item, ...next } : item)));
      }
    };
    if (where === "model") setModel(tile);
    else setProducts((prev) => [...prev, tile].slice(0, MAX_PRODUCTS));
    try {
      const asset = await uploadAsset(file);
      patch({ assetId: toAssetNumber(asset.id), url: asset.url, status: "done" });
      URL.revokeObjectURL(preview);
    } catch {
      patch({ status: "error" });
    }
  };

  const tiles = model ? [...products, model] : products;
  const uploading = tiles.some((item) => item.status !== "done");

  /** 要保存的内容：素材只带变了的 */
  const payload = (): Omit<FactsSave, "round"> => {
    const ids = products.map((item) => item.assetId).filter((id): id is number => id !== null);
    const before = productAssets.map((item) => toAssetNumber(item.id));
    const sameProducts =
      ids.length === before.length && ids.every((id, index) => id === before[index]);
    const beforeModel = modelAsset ? toAssetNumber(modelAsset.id) : null;
    return {
      facts: buildFacts(facts, edit),
      productAssetIds: sameProducts ? undefined : ids,
      modelAssetId: model?.assetId && model.assetId !== beforeModel ? model.assetId : undefined,
    };
  };
  const canSave = !busy && !uploading && !!edit.name.trim() && products.length > 0;
  const imagesDisabled = imagesLocked || busy;

  const inputClass =
    "bg-chrome-hover border-chrome-border focus-visible:ring-ring/50 w-full rounded-2xl border px-3.5 py-2 text-sm outline-none focus-visible:ring-2";
  // 多行框随内容长高，封顶后框内滚动，免得把弹窗撑出屏幕
  const textareaClass = "max-h-36 overflow-y-auto";
  return (
    <Dialog open={open} onOpenChange={(next) => !busy && onOpenChange(next)}>
      <DialogContent className="flex max-h-[min(88svh,820px)] flex-col sm:max-w-xl">
        <DialogHeader>
          <DialogTitle>商品信息</DialogTitle>
        </DialogHeader>
        <div className="-mx-4 grid min-h-0 flex-1 gap-3.5 overflow-y-auto px-4 py-0.5">
          <div className="grid gap-1.5 text-xs">
            <span className="text-muted-foreground">商品图与模特</span>
            <div className="flex flex-wrap items-center gap-2 pt-1.5">
              <ProductStack
                items={products.map((item) => ({
                  id: item.key,
                  url: item.url,
                  status: item.status,
                }))}
                max={MAX_PRODUCTS}
                min={1}
                size="h-16 w-13"
                disabled={imagesDisabled}
                onAdd={(files) => files.forEach((file) => void onFile(file, "add"))}
                onRemove={(key) => setProducts((prev) => prev.filter((item) => item.key !== key))}
              />
              {model && (
                <EditTile
                  tile={model}
                  caption="模特"
                  disabled={imagesDisabled}
                  onPick={() => fileInput.current?.click()}
                />
              )}
            </div>
            <input
              ref={fileInput}
              type="file"
              accept="image/*"
              hidden
              onChange={(event) => {
                void onFile(event.target.files?.[0], "model");
                event.target.value = "";
              }}
            />
          </div>
          <label className="grid gap-1.5 text-xs">
            <span className="text-muted-foreground">商品名</span>
            <input className={inputClass} {...field("name")} />
          </label>
          <div className="grid gap-1.5 text-xs">
            <div className="flex items-center justify-between gap-2">
              <label htmlFor="marketing-facts-anchors" className="text-muted-foreground">
                外观锚点（每行一条）
              </label>
              <button
                type="button"
                disabled={!canSave || imagesLocked}
                onClick={() => onRefresh(payload())}
                className="text-muted-foreground hover:text-foreground hover:bg-chrome-hover inline-flex items-center gap-1 rounded-full px-2 py-0.5 transition-colors disabled:pointer-events-none disabled:opacity-45 [&_svg]:size-3.5"
              >
                <RefreshCw />
                按商品图重新识别
              </button>
            </div>
            <Textarea
              id="marketing-facts-anchors"
              rows={3}
              className={cn(inputClass, textareaClass)}
              {...field("anchors")}
            />
          </div>
          <label className="grid gap-1.5 text-xs">
            <span className="text-muted-foreground">卖点（每行一条）</span>
            <Textarea rows={3} className={cn(inputClass, textareaClass)} {...field("selling")} />
          </label>
          <label className="grid gap-1.5 text-xs">
            <span className="text-muted-foreground">禁用说法（每行一条）</span>
            <Textarea rows={2} className={cn(inputClass, textareaClass)} {...field("prohibited")} />
          </label>
          <label className="grid gap-1.5 text-xs">
            <span className="text-muted-foreground">引导下单</span>
            <input className={inputClass} {...field("cta")} />
          </label>
        </div>
        <DialogFooter>
          <Button variant="ghost" size="sm" disabled={busy} onClick={() => onOpenChange(false)}>
            取消
          </Button>
          <Button size="sm" disabled={!canSave} onClick={() => onSave(payload())}>
            {busy && <Loader2 className="animate-spin" />}
            保存
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

/**
 * 左栏下半的商品信息（可折叠，默认展开）：素材缩略图（模特图取当前这一轮出片用的）、
 * 外观锚点 / 卖点 / 禁用说法 / 引导下单，右上的笔打开编辑。
 * 事实卡还没生成时（写脚本中）显示一行转圈；重新识别外观锚点期间锚点标题旁转圈。
 */
export function FactsPanel({
  project,
  round,
  scripting,
  refreshing,
  onEdit,
}: {
  project: Project;
  round: Round | undefined;
  scripting: boolean;
  refreshing: boolean;
  onEdit: () => void;
}) {
  const [open, setOpen] = useState(true);
  const facts = project.product;
  const modelAsset = round ? round.modelAsset : project.modelAsset;
  return (
    <section data-slot="marketing-facts" className="grid gap-3 px-4 pb-4">
      <div className="flex items-center gap-1">
        <button
          type="button"
          aria-expanded={open}
          onClick={() => setOpen((value) => !value)}
          className="text-muted-foreground hover:text-foreground flex min-w-0 flex-1 items-center gap-1.5 py-1 text-xs"
        >
          <Package className="size-3.5" />
          商品信息
          {open ? (
            <ChevronUp className="ml-auto size-3.5" />
          ) : (
            <ChevronDown className="ml-auto size-3.5" />
          )}
        </button>
        <Button
          variant="ghost"
          size="icon-sm"
          aria-label="编辑商品信息"
          title="编辑商品信息"
          disabled={!facts}
          onClick={onEdit}
        >
          <Pencil />
        </Button>
      </div>
      {open && (
        <>
          <button
            type="button"
            disabled={!facts}
            onClick={onEdit}
            aria-label="编辑商品图与模特"
            className="focus-visible:ring-ring/50 flex flex-wrap gap-1.5 rounded-xl text-left outline-none focus-visible:ring-2 disabled:cursor-default"
          >
            {project.productAssets.map((asset, index) => (
              <Thumb key={asset.id} url={asset.url} label={index === 0 ? "商品" : undefined} />
            ))}
            {modelAsset && <Thumb url={modelAsset.url} label="模特" />}
          </button>
          {facts ? (
            <>
              <FactSection
                icon={ScanEye}
                title="外观锚点"
                items={facts.anchors}
                busy={refreshing}
              />
              <FactSection
                icon={BadgeCheck}
                title="卖点"
                items={facts.sellingPoints.map((item) => item.text)}
              />
              <FactSection icon={ShieldBan} title="禁用说法" items={facts.prohibited} />
              <FactSection
                icon={MousePointerClick}
                title="引导下单"
                items={facts.cta ? [facts.cta] : []}
              />
            </>
          ) : scripting ? (
            <div className="text-muted-foreground flex items-center gap-1.5 text-[12.5px]">
              <Loader2 className="size-3.5 animate-spin" />
              正在生成商品事实卡
            </div>
          ) : null}
        </>
      )}
    </section>
  );
}

/** 素材缩略图，悬停略放大 */
function Thumb({ url, label }: { url: string; label?: string }) {
  return (
    <span className="bg-muted relative block h-12.5 w-10 overflow-hidden rounded-[10px] transition-transform duration-150 hover:scale-105">
      <img src={url} alt="" className="size-full object-cover" />
      {label && (
        <span className="bg-cover-scrim text-cover-foreground absolute bottom-0.5 left-0.5 rounded-full px-1 text-[9px] leading-3.5">
          {label}
        </span>
      )}
    </span>
  );
}

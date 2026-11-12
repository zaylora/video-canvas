import { useState } from "react";
import {
  BadgeCheck,
  ChevronDown,
  ChevronUp,
  Loader2,
  MousePointerClick,
  Package,
  Pencil,
  ScanEye,
  ShieldBan,
  type LucideIcon,
} from "lucide-react";

import type { ProductFacts, Project } from "@/api/marketing/type";
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

/** 每段超过几条折叠成「+N 条」 */
const FOLD_AFTER = 4;

/** 事实卡的一段：图标小标题 + 列表；条目多时先收起，点「+N 条」展开 */
function FactSection({
  icon: Icon,
  title,
  items,
}: {
  icon: LucideIcon;
  title: string;
  items: string[];
}) {
  const [expanded, setExpanded] = useState(false);
  if (items.length === 0) return null;
  const folded = !expanded && items.length > FOLD_AFTER;
  const shown = folded ? items.slice(0, FOLD_AFTER) : items;
  return (
    <div>
      <h4 className="text-muted-foreground flex items-center gap-1.5 text-[11.5px] font-medium [&_svg]:size-3.5">
        <Icon />
        {title}
      </h4>
      <ul className="mt-1 grid gap-0.75 text-[12.5px] leading-relaxed">
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

/** 商品事实卡编辑弹窗：商品名、外观锚点、卖点、禁用说法、引导下单；保存后下次出片按新的事实卡拼提示词 */
export function FactDialog({
  facts,
  open,
  busy,
  onOpenChange,
  onSave,
}: {
  facts: ProductFacts;
  open: boolean;
  busy: boolean;
  onOpenChange: (open: boolean) => void;
  onSave: (facts: ProductFacts) => void;
}) {
  const [edit, setEdit] = useState({
    name: facts.name,
    anchors: facts.anchors.join("\n"),
    selling: facts.sellingPoints.map((item) => item.text).join("\n"),
    prohibited: facts.prohibited.join("\n"),
    cta: facts.cta,
  });
  const field = (key: keyof typeof edit) => ({
    value: edit[key],
    onChange: (event: { target: { value: string } }) =>
      setEdit((prev) => ({ ...prev, [key]: event.target.value })),
  });
  const inputClass =
    "bg-chrome-hover border-chrome-border focus-visible:ring-ring/50 w-full rounded-2xl border px-3.5 py-2 text-sm outline-none focus-visible:ring-2";
  return (
    <Dialog open={open} onOpenChange={(next) => !busy && onOpenChange(next)}>
      <DialogContent className="sm:max-w-xl">
        <DialogHeader>
          <DialogTitle>商品事实卡</DialogTitle>
        </DialogHeader>
        <div className="grid gap-3.5">
          <label className="grid gap-1.5 text-xs">
            <span className="text-muted-foreground">商品名</span>
            <input className={inputClass} {...field("name")} />
          </label>
          <label className="grid gap-1.5 text-xs">
            <span className="text-muted-foreground">外观锚点（每行一条）</span>
            <Textarea rows={3} className={inputClass} {...field("anchors")} />
          </label>
          <label className="grid gap-1.5 text-xs">
            <span className="text-muted-foreground">卖点（每行一条）</span>
            <Textarea rows={3} className={inputClass} {...field("selling")} />
          </label>
          <label className="grid gap-1.5 text-xs">
            <span className="text-muted-foreground">禁用说法（每行一条）</span>
            <Textarea rows={2} className={inputClass} {...field("prohibited")} />
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
          <Button
            size="sm"
            disabled={busy || !edit.name.trim()}
            onClick={() => onSave(buildFacts(facts, edit))}
          >
            {busy && <Loader2 className="animate-spin" />}
            保存
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

/**
 * 左栏下半的商品信息（可折叠，默认展开）：素材缩略图、外观锚点 / 卖点 / 禁用说法 / 引导下单，
 * 右上的笔打开事实卡编辑。事实卡还没生成时（写脚本中）显示一行转圈。
 */
export function FactsPanel({
  project,
  scripting,
  onEdit,
}: {
  project: Project;
  scripting: boolean;
  onEdit: () => void;
}) {
  const [open, setOpen] = useState(true);
  const facts = project.product;
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
          aria-label="编辑商品事实卡"
          title="编辑商品事实卡"
          disabled={!facts}
          onClick={onEdit}
        >
          <Pencil />
        </Button>
      </div>
      {open && (
        <>
          <div className="flex flex-wrap gap-1.5">
            {project.productAssets.map((asset, index) => (
              <Thumb key={asset.id} url={asset.url} label={index === 0 ? "商品" : undefined} />
            ))}
            {project.modelAsset && <Thumb url={project.modelAsset.url} label="模特" />}
          </div>
          {facts ? (
            <>
              <FactSection icon={ScanEye} title="外观锚点" items={facts.anchors} />
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
    <div
      className={cn(
        "bg-muted relative h-12.5 w-10 overflow-hidden rounded-[10px] transition-transform duration-150 hover:scale-105",
      )}
    >
      <img src={url} alt="" className="size-full object-cover" />
      {label && (
        <span className="bg-cover-scrim text-cover-foreground absolute bottom-0.5 left-0.5 rounded-full px-1 text-[9px] leading-3.5">
          {label}
        </span>
      )}
    </div>
  );
}

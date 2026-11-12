import { useState } from "react";
import { Link } from "react-router";
import {
  ChevronLeft,
  Loader2,
  MousePointerClick,
  Pencil,
  ScanEye,
  ShieldBan,
  Sparkles,
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
import { buildFacts } from "@/utils/marketing/facts";

/** 事实卡的一段：图标小标题 + 列表 */
function Fact({ icon: Icon, title, items }: { icon: LucideIcon; title: string; items: string[] }) {
  return (
    <div>
      <h4 className="text-muted-foreground flex items-center gap-1.5 text-xs font-medium [&_svg]:size-3.5">
        <Icon />
        {title}
      </h4>
      <ul className="mt-1.5 grid gap-1 text-[13px] leading-snug">
        {items.map((item) => (
          <li
            key={item}
            className="before:text-muted-foreground relative pl-3 before:absolute before:left-0 before:content-['·']"
          >
            {item}
          </li>
        ))}
      </ul>
    </div>
  );
}

/** 商品事实卡编辑弹窗：商品名、外观锚点、卖点、禁用说法、引导下单；保存后下次出片按新的事实卡拼提示词 */
function FactDialog({
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
 * 详情页左栏商品卡：返回、素材缩略图（商品 / 模特）、商品名 + 编辑、事实卡四段（外观锚点 / 卖点 / 禁用说法 / 引导下单）。
 * 事实卡还没生成时显示一行转圈；生成后可以编辑，保存带 revision，别处改过（68009）会载入最新版。
 * @param scripting 项目正在写脚本（事实卡可能还在生成）
 * @param saving 正在保存事实卡
 * @param onSave 保存，返回是否成功
 */
export function SideCard({
  project,
  scripting,
  saving,
  onSave,
}: {
  project: Project;
  scripting: boolean;
  saving: boolean;
  onSave: (facts: ProductFacts) => Promise<boolean>;
}) {
  const [editing, setEditing] = useState(false);
  const facts = project.product;
  return (
    <aside
      data-slot="marketing-side-card"
      className="bg-chrome ring-chrome-border grid content-start gap-4 rounded-3xl p-4 ring-1 backdrop-blur-xl lg:sticky lg:top-4"
    >
      <Link
        to="/marketing"
        className="text-muted-foreground hover:text-foreground focus-visible:ring-ring/50 -mt-1 -ml-1.5 inline-flex h-8 w-fit items-center gap-0.5 rounded-full pr-3 pl-1.5 text-[13px] outline-none focus-visible:ring-2"
      >
        <ChevronLeft className="size-4" />
        营销工作室
      </Link>
      <div className="grid grid-cols-3 gap-1.5">
        {project.productAssets.map((asset, index) => (
          <div key={asset.id} className="bg-muted relative aspect-4/5 overflow-hidden rounded-xl">
            <img src={asset.url} alt="" className="size-full object-cover" />
            {index === 0 && (
              <span className="bg-cover-scrim text-cover-foreground absolute bottom-1 left-1 rounded-full px-1.5 text-[10px] leading-4">
                商品
              </span>
            )}
          </div>
        ))}
        {project.modelAsset && (
          <div className="bg-muted relative aspect-4/5 overflow-hidden rounded-xl">
            <img src={project.modelAsset.url} alt="" className="size-full object-cover" />
            <span className="bg-cover-scrim text-cover-foreground absolute bottom-1 left-1 rounded-full px-1.5 text-[10px] leading-4">
              模特
            </span>
          </div>
        )}
      </div>
      <div className="flex items-center gap-1.5">
        <div className="min-w-0 flex-1 text-base font-semibold">{project.title}</div>
        <Button
          variant="ghost"
          size="icon-sm"
          aria-label="编辑商品事实卡"
          title="编辑商品事实卡"
          disabled={!facts}
          onClick={() => setEditing(true)}
        >
          <Pencil />
        </Button>
      </div>
      {facts ? (
        <>
          <Fact icon={ScanEye} title="外观锚点" items={facts.anchors} />
          <Fact icon={Sparkles} title="卖点" items={facts.sellingPoints.map((item) => item.text)} />
          <Fact icon={ShieldBan} title="禁用说法" items={facts.prohibited} />
          {facts.cta && <Fact icon={MousePointerClick} title="引导下单" items={[facts.cta]} />}
        </>
      ) : scripting ? (
        <div className="text-muted-foreground flex items-center gap-1.5 text-[12.5px]">
          <Loader2 className="size-3.5 animate-spin" />
          正在生成商品事实卡
        </div>
      ) : null}
      {facts && editing && (
        <FactDialog
          key={project.revision}
          facts={facts}
          open
          busy={saving}
          onOpenChange={setEditing}
          onSave={(next) => void onSave(next).then((ok) => ok && setEditing(false))}
        />
      )}
    </aside>
  );
}

import { useState, type KeyboardEvent } from "react";
import {
  Box,
  Camera,
  Check,
  Clock,
  Film,
  History,
  LayoutGrid,
  Shapes,
  Sun,
  User,
  type LucideIcon,
} from "lucide-react";

import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import {
  MOTION_CATEGORIES,
  MOTION_PRESETS,
  PRESET_KINDS,
  STYLE_CATEGORIES,
  STYLE_PRESETS,
  TEMPLATE_GROUPS,
  TEMPLATE_PRESETS,
  findPreset,
  type PresetKind,
  type TemplateIcon,
} from "@/constants/presets";
import { cn } from "@/lib/utils";
import type { PresetPick } from "@/utils/canvas/preset-rules";

import { usePromptRefs, type PromptPresets } from "./prompt-mention";

/** 入口按钮和选择器各类的图标 */
const KIND_ICON: Record<PresetKind, LucideIcon> = { style: Box, motion: Camera, tpl: Shapes };

const TEMPLATE_ICON: Record<TemplateIcon, LucideIcon> = {
  film: Film,
  grid: LayoutGrid,
  clock: Clock,
  history: History,
  sun: Sun,
  user: User,
  box: Box,
};

/** 全部、分类标签：横向滚动的一行小胶囊 */
function CategoryTabs({
  categories,
  value,
  onValueChange,
}: {
  categories: readonly { id: string; label: string }[];
  value: string;
  onValueChange: (id: string) => void;
}) {
  return (
    <div
      role="tablist"
      className="flex gap-1 overflow-x-auto px-2.5 pt-2.5 pb-1.5 [scrollbar-width:none]"
    >
      {[{ id: "all", label: "全部" }, ...categories].map((item) => (
        <button
          key={item.id}
          type="button"
          role="tab"
          aria-selected={value === item.id}
          onClick={() => onValueChange(item.id)}
          className={cn(
            "focus-visible:ring-node-ring h-7 shrink-0 rounded-full px-3 text-xs outline-none transition-colors focus-visible:ring-2",
            value === item.id
              ? "bg-muted text-foreground"
              : "text-muted-foreground hover:text-foreground",
          )}
        >
          {item.label}
        </button>
      ))}
    </div>
  );
}

/** 卡片右上角的「已选」勾 */
function SelectedMark() {
  return (
    <span className="bg-preset absolute top-1.5 right-1.5 grid size-4.5 place-items-center rounded-full text-white">
      <Check className="size-3 stroke-[3]" />
    </span>
  );
}

const CARD_CLASS = cn(
  "group focus-visible:ring-node-ring relative overflow-hidden rounded-[10px] bg-muted text-left outline-none",
  "ring-chrome-border ring-1 ring-inset transition-[box-shadow,transform] duration-120",
  "hover:ring-node-ring hover:ring-[1.5px] focus-visible:ring-2 active:scale-[0.97]",
);

/** 卡片底部压名字的渐变条 */
const NAME_CLASS =
  "from-cover-scrim text-cover-foreground absolute inset-x-0 bottom-0 bg-linear-to-t to-transparent px-2 pt-4 pb-1.5 text-xs font-semibold";

/**
 * 选择器本体（风格 / 运镜 / 模板）：分类标签 + 卡片网格。
 * 没有随悬停变化的信息条：弹窗锚在底部，内容高度一变它就会上下跳，鼠标底下的卡片跟着换，形成抖动。
 * 用途和说明放在卡片的原生提示里。分类状态在这一层：关掉选择器就重置，下次从「全部」开始。
 */
function PresetBody({
  kind,
  selected,
  onPick,
}: {
  kind: PresetKind;
  selected: readonly PresetPick[];
  onPick: (id: string) => void;
}) {
  const [category, setCategory] = useState("all");
  const chosen = new Set(selected.filter((item) => item.kind === kind).map((item) => item.id));
  // 从 chip 点进来的是替换：它自己当前那一条在选择器里照样显示为已选
  const itemProps = (id: string, name: string, hint?: string) => ({
    "data-preset-item": true,
    "aria-pressed": chosen.has(id),
    // 用途和说明放在原生提示里：不占版面，选择器的高度就不会随悬停变化
    title: hint ? `${name} · ${hint}` : name,
    onClick: () => onPick(id),
  });

  const onKeyDown = (event: KeyboardEvent<HTMLDivElement>) => {
    if (!event.key.startsWith("Arrow")) return;
    const items = [...event.currentTarget.querySelectorAll<HTMLElement>("[data-preset-item]")];
    const index = items.indexOf(document.activeElement as HTMLElement);
    if (index < 0) return;
    const grid = event.currentTarget.querySelector<HTMLElement>("[data-preset-grid]");
    const columns = grid ? getComputedStyle(grid).gridTemplateColumns.split(" ").length : 1;
    const step = { ArrowRight: 1, ArrowLeft: -1, ArrowDown: columns, ArrowUp: -columns }[event.key];
    const next = step ? items[index + step] : undefined;
    if (!next) return;
    event.preventDefault();
    next.focus();
  };

  return (
    <div onKeyDown={onKeyDown}>
      {kind === "style" && (
        <>
          <CategoryTabs
            categories={STYLE_CATEGORIES}
            value={category}
            onValueChange={setCategory}
          />
          <div
            data-preset-grid
            className="grid max-h-[min(318px,46vh)] grid-cols-2 gap-2 overflow-y-auto px-2.5 pt-1.5 pb-2.5 sm:grid-cols-3 md:grid-cols-4"
          >
            {STYLE_PRESETS.filter((item) => category === "all" || item.category === category).map(
              (item) => (
                <button
                  key={item.id}
                  type="button"
                  aria-label={item.name}
                  {...itemProps(item.id, item.name)}
                  className={cn(
                    CARD_CLASS,
                    "aspect-video",
                    chosen.has(item.id) && "ring-preset ring-2",
                  )}
                >
                  <img
                    src={item.cover}
                    alt=""
                    draggable={false}
                    loading="lazy"
                    className="size-full object-cover"
                  />
                  <span className={NAME_CLASS}>{item.name}</span>
                  {chosen.has(item.id) && <SelectedMark />}
                </button>
              ),
            )}
          </div>
        </>
      )}
      {kind === "motion" && (
        <>
          <CategoryTabs
            categories={MOTION_CATEGORIES}
            value={category}
            onValueChange={setCategory}
          />
          <div
            data-preset-grid
            className="grid max-h-[min(318px,46vh)] grid-cols-2 gap-2 overflow-y-auto px-2.5 pt-1.5 pb-2.5 sm:grid-cols-3 md:grid-cols-4"
          >
            {MOTION_PRESETS.filter((item) => category === "all" || item.category === category).map(
              (item) => (
                <button
                  key={item.id}
                  type="button"
                  aria-label={item.name}
                  {...itemProps(item.id, item.name, item.usage)}
                  className={cn(
                    CARD_CLASS,
                    "aspect-square",
                    chosen.has(item.id) && "ring-preset ring-2",
                  )}
                >
                  <img
                    src={item.cover}
                    alt=""
                    draggable={false}
                    loading="lazy"
                    className="size-full object-cover"
                  />
                  <span className={NAME_CLASS}>{item.name}</span>
                  {chosen.has(item.id) && <SelectedMark />}
                </button>
              ),
            )}
          </div>
        </>
      )}
      {kind === "tpl" && (
        <div className="grid grid-cols-1 items-start gap-x-3 gap-y-1 px-2.5 pt-3 pb-2 sm:grid-cols-2 md:grid-cols-3">
          {[
            [TEMPLATE_GROUPS[0]],
            [TEMPLATE_GROUPS[1], TEMPLATE_GROUPS[2]],
            [TEMPLATE_GROUPS[3]],
          ].map((groups) => (
            <div key={groups[0]} className="flex flex-col gap-0.5">
              {groups.map((group, index) => (
                <div key={group} className={cn("flex flex-col gap-0.5", index > 0 && "mt-2.5")}>
                  <span className="text-muted-foreground px-2 pt-1 pb-1.5 text-xs">{group}</span>
                  {TEMPLATE_PRESETS.filter((item) => item.group === group).map((item) => {
                    const Icon = TEMPLATE_ICON[item.icon];
                    return (
                      <button
                        key={item.id}
                        type="button"
                        {...itemProps(item.id, item.name, item.usage)}
                        className="hover:bg-chrome-hover focus-visible:ring-node-ring flex items-center gap-2.5 rounded-[10px] px-2 py-1.5 text-left text-[13px] font-semibold outline-none transition-colors focus-visible:ring-2"
                      >
                        <span
                          className={cn(
                            "grid size-8.5 shrink-0 place-items-center rounded-[9px]",
                            chosen.has(item.id)
                              ? "bg-preset text-white"
                              : "bg-muted text-muted-foreground",
                          )}
                        >
                          <Icon className="size-4.5" />
                        </span>
                        {item.name}
                      </button>
                    );
                  })}
                </div>
              ))}
            </div>
          ))}
        </div>
      )}
    </div>
  );
}

/** 入口按钮的样式：只有图标，和面板底栏的 chip 同高 */
const TRIGGER_CLASS = cn(
  "nodrag text-muted-foreground relative inline-grid h-8.5 w-9 shrink-0 place-items-center rounded-full",
  "ring-1 ring-foreground/10 transition-[background-color,color,transform] hover:bg-chrome-hover hover:text-foreground",
  "focus-visible:ring-node-ring/60 outline-none focus-visible:ring-2 active:scale-[0.96]",
  "data-popup-open:bg-foreground/10 data-popup-open:text-foreground",
  "disabled:pointer-events-none disabled:opacity-40 [&_svg]:size-4",
);

/** 一类预设的入口按钮 + 它的选择器 */
function PresetMenu({
  kind,
  open,
  onOpenChange,
  presets,
  disabled,
}: {
  kind: PresetKind;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  presets: PromptPresets;
  disabled?: boolean;
}) {
  const Icon = KIND_ICON[kind];
  const { label, multi } = PRESET_KINDS[kind];
  const mine = presets.selected.filter((item) => item.kind === kind);
  const names = mine.map((item) => findPreset(kind, item.id)?.name ?? "已下架");
  const chip = presets.chip?.kind === kind ? presets.chip : null;

  const pick = (id: string) => {
    // 从 chip 点进来选了别处已有的运镜：不执行，也不提示，选择器保持打开
    if (presets.apply(kind, id, chip?.index)?.type === "blocked") return;
    // 单选和从 chip 点进来的替换是一次性的；运镜从入口进来可以连选，不关
    if (!multi || chip) {
      onOpenChange(false);
      presets.focusEditor();
    }
  };

  return (
    <Popover open={open} onOpenChange={onOpenChange}>
      <PopoverTrigger
        className={cn(TRIGGER_CLASS, mine.length > 0 && "bg-foreground/10 text-foreground")}
        aria-label={label}
        title={
          disabled
            ? "生成中，暂时不能改预设"
            : `${label}${names.length ? `：${names.join("、")}` : ""}`
        }
        disabled={disabled}
      >
        <Icon />
        {mine.length > 1 ? (
          <span className="bg-preset ring-popover absolute -top-0.5 -right-0.5 min-w-3.75 rounded-full px-1 text-center font-mono text-[10px] leading-3.75 font-semibold text-white tabular-nums ring-2">
            {mine.length}
          </span>
        ) : mine.length === 1 ? (
          <span className="bg-preset ring-popover absolute top-1 right-1.5 size-1.75 rounded-full ring-2" />
        ) : null}
      </PopoverTrigger>
      <PopoverContent
        side="top"
        align="start"
        sideOffset={10}
        anchor={chip?.anchor}
        aria-label={`选择${label}`}
        className="nodrag nowheel w-[min(620px,calc(100vw-1rem))] gap-0 overflow-hidden rounded-2xl p-0"
      >
        <PresetBody kind={kind} selected={presets.selected} onPick={pick} />
      </PopoverContent>
    </Popover>
  );
}

/**
 * 生成面板底栏的节点预设入口（设计稿 6.13）：图片是风格和预设模板，视频是运镜。
 * 选择结果写进提示词里的预设 chip；点提示词里的 chip 会在它旁边弹出同类选择器，用来替换。
 * 必须放在 NodePromptInput 里面，它通过上下文拿到编辑器。
 */
export function PresetPicker({
  kinds,
  disabled,
}: {
  kinds: readonly PresetKind[];
  disabled?: boolean;
}) {
  const refs = usePromptRefs();
  // 从入口按钮点开的是哪一类；从 chip 点开的由上下文里的 chip 决定
  const [fromTrigger, setFromTrigger] = useState<PresetKind | null>(null);
  if (!refs) return null;
  const { presets } = refs;
  const chipKind = presets.chip && kinds.includes(presets.chip.kind) ? presets.chip.kind : null;
  const openKind = chipKind ?? fromTrigger;

  return (
    <>
      {kinds.map((kind) => (
        <PresetMenu
          key={kind}
          kind={kind}
          open={!disabled && openKind === kind}
          presets={presets}
          disabled={disabled}
          onOpenChange={(next) => {
            presets.setChip(null);
            setFromTrigger(next ? kind : null);
          }}
        />
      ))}
    </>
  );
}

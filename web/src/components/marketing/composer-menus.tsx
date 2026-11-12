import type { ComponentType, ReactNode } from "react";
import { motion } from "motion/react";
import { Check, ChevronDown, Copy, Globe, Lightbulb, Package } from "lucide-react";

import type {
  AngleOption,
  MarketingAngle,
  MarketingLanguage,
  MarketingPlatform,
  PlatformOption,
} from "@/api/marketing/type";
import type { ModelInfo } from "@/api/model/type";
import { RatioIcon } from "@/components/ratio-icon";
import {
  InstagramIcon,
  TaobaoIcon,
  TikTokIcon,
  XiaohongshuIcon,
} from "@/components/marketing/platform-icons";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { TAP } from "@/lib/motion";
import { cn } from "@/lib/utils";
import {
  durationOptions,
  MAX_ADAPT,
  MAX_ANGLES,
  ratioOptions,
  resolutionOptions,
  type ComposerDraft,
} from "@/utils/marketing/composer";

/** 工具条按钮的公共样式 */
export const BAR_BUTTON =
  "text-foreground hover:bg-muted focus-visible:ring-ring/50 inline-flex h-8 max-w-56 items-center gap-1.5 rounded-[10px] px-2.5 text-[13px] outline-none focus-visible:ring-3 disabled:opacity-60 [&_svg]:size-4 [&_svg]:shrink-0";

/** 平台图标：有官方品牌图标的用品牌图标（抖音与 TikTok 同一个音符标），亚马逊（已下线，只给旧轮次显示）与通用用线性图标 */
export const PLATFORM_ICON: Record<MarketingPlatform, ComponentType<{ className?: string }>> = {
  douyin: TikTokIcon,
  xhs: XiaohongshuIcon,
  tmall: TaobaoIcon,
  tiktok: TikTokIcon,
  amazon: Package,
  reels: InstagramIcon,
  generic: Globe,
};

/** 带弹层的工具条按钮：trigger 是按钮内容，弹层向上或向下展开 */
function BarPopover({
  label,
  ariaLabel,
  className,
  width,
  side,
  children,
}: {
  label: ReactNode;
  ariaLabel: string;
  className?: string;
  width: string;
  side: "top" | "bottom";
  children: ReactNode;
}) {
  return (
    <Popover>
      <PopoverTrigger
        render={<motion.button type="button" whileTap={TAP} />}
        aria-label={ariaLabel}
        className={cn(BAR_BUTTON, "group/bar", className)}
      >
        {label}
        <ChevronDown className="size-3.5 transition-transform duration-120 group-data-popup-open/bar:rotate-180" />
      </PopoverTrigger>
      <PopoverContent
        side={side}
        align="start"
        sideOffset={6}
        className={cn("max-h-[min(70vh,34rem)] gap-1 overflow-y-auto p-1.5", width)}
      >
        {children}
      </PopoverContent>
    </Popover>
  );
}

/** 弹层里的一行：可带选中勾 */
function MenuRow({
  active,
  disabled,
  title,
  onClick,
  children,
}: {
  active?: boolean;
  disabled?: boolean;
  title?: string;
  onClick: () => void;
  children: ReactNode;
}) {
  return (
    <button
      type="button"
      disabled={disabled}
      title={title}
      onClick={onClick}
      className="hover:bg-muted focus-visible:ring-ring/50 flex min-h-10 w-full items-center gap-2.5 rounded-[10px] px-2.5 py-1.5 text-left outline-none focus-visible:ring-2 disabled:cursor-not-allowed disabled:opacity-45 disabled:hover:bg-transparent"
    >
      {children}
      {active !== undefined && (
        <span
          aria-hidden
          className={cn(
            "ml-auto grid size-4 shrink-0 place-items-center rounded-[5px] border",
            active ? "border-foreground bg-foreground text-background" : "border-border",
          )}
        >
          {active && <Check className="size-3" />}
        </span>
      )}
    </button>
  );
}

/** 一组互斥的小分段按钮（份数、分辨率、时长、语言） */
function SegRow<T extends string | number>({
  value,
  options,
  onChange,
  label,
}: {
  value: T;
  options: Array<{ value: T; label: string; disabled?: boolean; title?: string }>;
  onChange: (value: T) => void;
  label: string;
}) {
  return (
    <div role="radiogroup" aria-label={label} className="flex flex-wrap gap-1.5">
      {options.map((option) => (
        <button
          key={String(option.value)}
          type="button"
          role="radio"
          aria-checked={option.value === value}
          disabled={option.disabled}
          title={option.title}
          onClick={() => onChange(option.value)}
          className={cn(
            "ring-chrome-border focus-visible:ring-ring/50 h-8 rounded-full px-3 text-[13px] tabular-nums outline-none ring-1 transition-colors duration-120 focus-visible:ring-2 disabled:cursor-not-allowed disabled:opacity-40",
            option.value === value
              ? "bg-chrome-hover text-foreground font-medium"
              : "text-muted-foreground hover:bg-chrome-hover",
          )}
        >
          {option.label}
        </button>
      ))}
    </div>
  );
}

/** 弹层里的小标题 */
function FieldLabel({ children }: { children: ReactNode }) {
  return <div className="text-muted-foreground px-1 pb-1.5 text-xs">{children}</div>;
}

/** 投放平台：名称用 text-beam 着色，菜单里写画幅、旁白语言和写法要点 */
export function PlatformMenu({
  platforms,
  value,
  onPick,
  side,
}: {
  platforms: PlatformOption[];
  value: MarketingPlatform;
  onPick: (platform: PlatformOption) => void;
  side: "top" | "bottom";
}) {
  const current = platforms.find((item) => item.key === value) ?? platforms[0];
  const Icon = PLATFORM_ICON[current?.key ?? "generic"];
  return (
    <BarPopover
      ariaLabel={`投放平台，当前 ${current?.name ?? ""}`}
      className="text-beam font-medium"
      width="w-80"
      side={side}
      label={
        <>
          <Icon />
          <span className="truncate">{current?.name}</span>
        </>
      }
    >
      {platforms.map((platform) => {
        const RowIcon = PLATFORM_ICON[platform.key];
        return (
          <MenuRow key={platform.key} onClick={() => onPick(platform)}>
            <RowIcon className="size-4 shrink-0" />
            <span className="grid min-w-0 gap-0.5">
              <span className="text-[13.5px] font-medium">{platform.name}</span>
              <span className="text-muted-foreground text-xs leading-snug">
                {[
                  platform.aspectRatio,
                  platform.language === "en" ? "English 旁白" : "中文旁白",
                  platform.style,
                ]
                  .filter(Boolean)
                  .join(" · ")}
              </span>
            </span>
            {platform.key === value && <Check className="ml-auto size-4 shrink-0" />}
          </MenuRow>
        );
      })}
    </BarPopover>
  );
}

/**
 * 创意角度：无参考视频时是「自动 ×1–4」或多选内置角度（最多 4 个）；
 * 有参考视频时顶部固定「跟拍 1 份」，下面选改编份数 0–3 和改编角度（最多改编份数个）。
 */
export function AngleMenu({
  label,
  angles,
  draft,
  hasRef,
  onPick,
  onAuto,
  onAutoCount,
  onAdaptCount,
  side,
}: {
  label: string;
  angles: AngleOption[];
  draft: ComposerDraft;
  hasRef: boolean;
  onPick: (key: MarketingAngle) => void;
  onAuto: () => void;
  onAutoCount: (n: number) => void;
  onAdaptCount: (n: number) => void;
  side: "top" | "bottom";
}) {
  const auto = draft.angles.length === 0;
  const max = hasRef ? draft.adaptCount : MAX_ANGLES;
  const full = !auto && draft.angles.length >= max;
  const showAngles = !(hasRef && draft.adaptCount === 0);
  return (
    <BarPopover
      ariaLabel={`创意角度，当前 ${label}`}
      width="w-85"
      side={side}
      label={
        <>
          {hasRef ? <Copy /> : <Lightbulb />}
          <span className="truncate">{label}</span>
        </>
      }
    >
      {hasRef && (
        <div className="grid gap-2 px-1 pb-1.5">
          <div className="flex items-start gap-2.5 px-1.5 pt-1">
            <span className="bg-foreground/80 text-background mt-0.5 grid size-4 shrink-0 place-items-center rounded-[5px]">
              <Check className="size-3" />
            </span>
            <span className="grid gap-0.5">
              <span className="text-[13.5px] font-medium">跟拍 1 份</span>
              <span className="text-muted-foreground text-xs leading-snug">
                结构、节奏、钩子照参考片，只换成你的商品和卖点
              </span>
            </span>
          </div>
          <div className="px-1.5">
            <FieldLabel>改编份数</FieldLabel>
            <SegRow
              label="改编份数"
              value={draft.adaptCount}
              onChange={onAdaptCount}
              options={Array.from({ length: MAX_ADAPT + 1 }, (_, n) => ({
                value: n,
                label: String(n),
              }))}
            />
          </div>
        </div>
      )}
      {showAngles && (
        <>
          <MenuRow active={auto} onClick={onAuto}>
            <span className="grid gap-0.5">
              <span className="text-[13.5px] font-medium">自动</span>
              <span className="text-muted-foreground text-xs leading-snug">
                按商品挑{hasRef ? "改编" : ""}角度
              </span>
            </span>
          </MenuRow>
          {auto && !hasRef && (
            <div className="px-2.5 pb-2">
              <SegRow
                label="自动份数"
                value={draft.autoCount}
                onChange={onAutoCount}
                options={[1, 2, 3, 4].map((n) => ({ value: n, label: `${n} 个` }))}
              />
            </div>
          )}
          <div className="text-muted-foreground px-2.5 pt-1.5 text-xs">
            {hasRef ? "改编角度" : "内置角度"}
          </div>
          {angles.map((angle) => {
            const on = draft.angles.includes(angle.key);
            return (
              <MenuRow
                key={angle.key}
                active={on}
                disabled={!on && full}
                title={!on && full ? `最多 ${max} 个` : undefined}
                onClick={() => onPick(angle.key)}
              >
                <span className="grid gap-0.5">
                  <span className="text-[13.5px] font-medium">{angle.name}</span>
                  <span className="text-muted-foreground text-xs leading-snug">{angle.desc}</span>
                </span>
              </MenuRow>
            );
          })}
        </>
      )}
    </BarPopover>
  );
}

/** 参数条上的摘要：画幅 · 分辨率 · 时长 · 旁白语言 */
export function paramsLabel(draft: ComposerDraft): string {
  return [
    draft.aspectRatio,
    draft.resolution.toUpperCase(),
    `${draft.duration}s`,
    draft.language === "en" ? "EN" : "中",
  ].join(" · ");
}

/** 参数弹层：画幅 / 分辨率 / 时长按模型能力列出，旁白语言，写脚本的文本模型（只列能看图的） */
export function ParamsMenu({
  model,
  draft,
  textModels,
  textLoading,
  onChange,
  side,
}: {
  model: ModelInfo | undefined;
  draft: ComposerDraft;
  textModels: ModelInfo[];
  textLoading: boolean;
  onChange: (patch: Partial<ComposerDraft>) => void;
  side: "top" | "bottom";
}) {
  const caps = model?.capabilities;
  const ratios = ratioOptions(caps);
  const resolutions = resolutionOptions(caps);
  const durations = durationOptions(caps);
  const languages: Array<{ value: MarketingLanguage; label: string }> = [
    { value: "zh", label: "中文" },
    { value: "en", label: "English" },
  ];
  return (
    <BarPopover
      ariaLabel="生成参数"
      className="tabular-nums"
      width="w-85"
      side={side}
      label={<span className="truncate">{paramsLabel(draft)}</span>}
    >
      <div className="grid gap-4 p-2.5">
        {ratios.length > 0 && (
          <div>
            <FieldLabel>画幅</FieldLabel>
            <div role="radiogroup" aria-label="画幅" className="flex flex-wrap gap-1.5">
              {ratios.map((ratio) => (
                <button
                  key={ratio}
                  type="button"
                  role="radio"
                  aria-checked={draft.aspectRatio === ratio}
                  onClick={() => onChange({ aspectRatio: ratio })}
                  className={cn(
                    "ring-chrome-border focus-visible:ring-ring/50 inline-flex h-9 items-center gap-1.5 rounded-full px-3 text-[13px] tabular-nums outline-none ring-1 transition-colors duration-120 focus-visible:ring-2",
                    draft.aspectRatio === ratio
                      ? "bg-chrome-hover text-foreground font-medium"
                      : "text-muted-foreground hover:bg-chrome-hover",
                  )}
                >
                  <RatioIcon ratio={ratio} className="h-4 w-auto" />
                  {ratio}
                </button>
              ))}
            </div>
          </div>
        )}
        {resolutions.length > 0 && (
          <div>
            <FieldLabel>分辨率</FieldLabel>
            <SegRow
              label="分辨率"
              value={draft.resolution}
              onChange={(value) => onChange({ resolution: value })}
              options={resolutions.map((item) => ({ value: item, label: item.toUpperCase() }))}
            />
          </div>
        )}
        {durations.length > 0 && (
          <div>
            <FieldLabel>时长</FieldLabel>
            <SegRow
              label="时长"
              value={draft.duration}
              onChange={(value) => onChange({ duration: value })}
              options={durations.map((item) => ({ value: item, label: `${item}s` }))}
            />
          </div>
        )}
        <div>
          <FieldLabel>旁白语言</FieldLabel>
          <SegRow
            label="旁白语言"
            value={draft.language}
            onChange={(value) => onChange({ language: value })}
            options={languages}
          />
        </div>
        <div>
          <FieldLabel>写脚本的文本模型</FieldLabel>
          <div className="grid gap-0.5">
            <MenuRow active={draft.textModel === ""} onClick={() => onChange({ textModel: "" })}>
              <span className="text-[13px]">自动选择</span>
            </MenuRow>
            {textModels.map((item) => (
              <MenuRow
                key={item.key}
                active={draft.textModel === item.key}
                onClick={() => onChange({ textModel: item.key })}
              >
                <span className="grid gap-0.5">
                  <span className="text-[13px]">{item.label}</span>
                  <span className="text-muted-foreground text-xs">
                    {item.capabilities.video
                      ? "能看图 · 能看视频"
                      : "能看图；拆参考片时改用能看视频的模型"}
                  </span>
                </span>
              </MenuRow>
            ))}
            {textModels.length === 0 && (
              <div className="text-muted-foreground px-2.5 py-2 text-xs">
                {textLoading ? "加载中…" : "没有能看图的文本模型"}
              </div>
            )}
          </div>
        </div>
      </div>
    </BarPopover>
  );
}

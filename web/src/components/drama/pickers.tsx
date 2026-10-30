import { useState, type ReactNode } from "react";
import { Check, ChevronDown, Pencil } from "lucide-react";

import type { DramaRatio, DramaStyles } from "@/api/drama/type";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { Textarea } from "@/components/ui/textarea";
import { RatioIcon } from "@/components/ratio-icon";
import { cn } from "@/lib/utils";
import {
  CUSTOM_STYLE_KEY,
  EPISODE_SECOND_OPTIONS,
  RATIOS,
  TARGET_EPISODE_OPTIONS,
} from "@/utils/drama/options";

import { DramaPoster } from "./poster";

/** 已随前端发布预览图的内置画风；新画风在补图前仍使用渐变占位。 */
const STYLE_PREVIEW_KEYS = new Set([
  "anime_90s",
  "chinese_anime",
  "flat_2d",
  "mature_urban_anime",
  "animation_3d",
  "chinese_3d",
  "clay_stop_motion",
  "chinese_cyber_3d",
  "real_historical",
  "real_city_film",
  "real_sweet_romance",
]);

/** 浮层里的小标题 */
export function MenuLabel({ children }: { children: ReactNode }) {
  return <div className="text-muted-foreground px-1.5 pt-1 text-xs">{children}</div>;
}

/** 浮层里的一个选项行 */
function OptionRow({
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
      aria-pressed={active}
      onClick={onClick}
      className={cn(
        "hover:bg-muted focus-visible:ring-ring/50 flex min-h-8 items-center gap-2 rounded-md px-2 text-left text-sm outline-none focus-visible:ring-2 disabled:cursor-not-allowed disabled:opacity-40 disabled:hover:bg-transparent",
        active && "bg-muted font-medium",
      )}
    >
      {children}
    </button>
  );
}

/**
 * 带浮层的选择按钮：创作卡底部的「题材 / 画风 / 比例 / 集数」和工作台顶栏共用。
 * 选完用 close 关掉浮层；set 表示已有值（高亮）。
 */
export function ChipPopover({
  icon,
  label,
  showChevron = true,
  set = true,
  variant = "chip",
  size = "default",
  disabled,
  title,
  width,
  children,
}: {
  icon?: ReactNode;
  label: string;
  showChevron?: boolean;
  set?: boolean;
  /** chip 带边框的胶囊；bar 工具栏里的平按钮；chrome 放在半透明胶囊（ChromePill）里的圆按钮 */
  variant?: "chip" | "bar" | "chrome";
  /** lg 加高一档、字和图标放大（放在主操作按钮旁边时用） */
  size?: "default" | "lg";
  disabled?: boolean;
  title?: string;
  width?: string;
  children: (close: () => void) => ReactNode;
}) {
  const [open, setOpen] = useState(false);
  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger
        disabled={disabled}
        title={title}
        className={cn(
          "focus-visible:ring-ring/50 inline-flex items-center gap-1.5 text-[13px] outline-none focus-visible:ring-2 disabled:cursor-not-allowed disabled:opacity-50 [&_svg]:size-4 [&_svg]:shrink-0",
          variant === "chip"
            ? "hover:bg-muted h-8 rounded-full border px-3"
            : variant === "chrome"
              ? "hover:bg-chrome-hover h-8 rounded-full px-3 transition-colors duration-150"
              : "hover:bg-muted h-8 rounded-lg px-2.5",
          variant === "chip" && !set && "text-muted-foreground",
          size === "lg" && "h-9 gap-2 px-3 text-sm [&_svg]:size-5",
        )}
      >
        {icon}
        <span className="max-w-40 truncate">{label}</span>
        {showChevron && <ChevronDown className="opacity-50" />}
      </PopoverTrigger>
      <PopoverContent align="start" sideOffset={6} className={cn("gap-1", width ?? "w-64")}>
        {children(() => setOpen(false))}
      </PopoverContent>
    </Popover>
  );
}

/** 题材：三列网格，可以不指定 */
export function GenreList({
  genres,
  value,
  onPick,
}: {
  genres: DramaStyles["genres"];
  value: string;
  onPick: (key: string) => void;
}) {
  return (
    <>
      <div className="grid grid-cols-3 gap-0.5">
        {genres.map((genre) => (
          <OptionRow
            key={genre.key}
            active={value === genre.key || value === genre.name}
            onClick={() => onPick(genre.key)}
          >
            {genre.name}
          </OptionRow>
        ))}
      </div>
      <OptionRow onClick={() => onPick("")}>
        <span className="text-muted-foreground">不指定</span>
      </OptionRow>
    </>
  );
}

/** 画风：卡片网格 + 自定义（自定义时给一个描述框） */
export function StyleGrid({
  styles,
  value,
  custom,
  onPick,
  onCustomChange,
  note,
}: {
  styles: DramaStyles["styles"];
  value: string;
  custom: string;
  onPick: (key: string) => void;
  onCustomChange: (text: string) => void;
  note?: string;
}) {
  return (
    <>
      <div className="grid grid-cols-3 gap-2 sm:grid-cols-5">
        {styles.map((style) => (
          <button
            key={style.key}
            type="button"
            aria-pressed={value === style.key}
            title={style.prompt}
            onClick={() => onPick(style.key)}
            className={cn(
              "focus-visible:ring-ring/50 group flex flex-col gap-1 rounded-lg p-1 text-left text-xs outline-none focus-visible:ring-2",
              value === style.key ? "bg-muted" : "hover:bg-muted/60",
            )}
          >
            <DramaPoster seed={style.key} className="aspect-16/10 w-full rounded-lg">
              {STYLE_PREVIEW_KEYS.has(style.key) && (
                <img
                  src={`/drama-styles/${style.key}.webp`}
                  alt={`${style.name}风格预览`}
                  width={640}
                  height={400}
                  loading="lazy"
                  decoding="async"
                  className="absolute inset-0 size-full object-cover"
                />
              )}
            </DramaPoster>
            <span className="truncate px-0.5">{style.name}</span>
          </button>
        ))}
        <button
          type="button"
          aria-pressed={value === CUSTOM_STYLE_KEY}
          onClick={() => onPick(CUSTOM_STYLE_KEY)}
          className={cn(
            "focus-visible:ring-ring/50 flex flex-col gap-1 rounded-lg p-1 text-left text-xs outline-none focus-visible:ring-2",
            value === CUSTOM_STYLE_KEY ? "bg-muted" : "hover:bg-muted/60",
          )}
        >
          <span className="bg-muted text-muted-foreground grid aspect-16/10 w-full place-items-center rounded-lg">
            <Pencil className="size-4" />
          </span>
          <span className="px-0.5">自定义</span>
        </button>
      </div>
      {value === CUSTOM_STYLE_KEY && (
        <Textarea
          value={custom}
          onChange={(event) => onCustomChange(event.target.value)}
          maxLength={500}
          placeholder="描述你想要的画风，如：黏土定格，暖色，微缩景观感"
          className="mt-1 min-h-16 text-sm"
        />
      )}
      {note && <div className="text-muted-foreground px-1.5 pb-1 text-xs">{note}</div>}
    </>
  );
}

/** 比例：已有锁定集时除当前值外全部禁用 */
export function RatioList({
  value,
  onPick,
  fixed,
  hint,
}: {
  value: DramaRatio;
  onPick: (ratio: DramaRatio) => void;
  /** 比例已固定（已有资产按当前比例生成） */
  fixed?: boolean;
  hint?: string;
}) {
  return (
    <>
      <MenuLabel>成片比例{hint ? ` · ${hint}` : ""}</MenuLabel>
      {RATIOS.map((ratio) => (
        <OptionRow
          key={ratio.key}
          active={value === ratio.key}
          disabled={!!fixed && value !== ratio.key}
          title={fixed && value !== ratio.key ? "已有资产按当前比例生成" : undefined}
          onClick={() => onPick(ratio.key)}
        >
          <RatioIcon ratio={ratio.key} />
          {ratio.key}
          {value === ratio.key && <Check className="ml-auto size-4" />}
        </OptionRow>
      ))}
      {fixed && (
        <div className="text-muted-foreground px-1.5 pb-1 text-xs">已拆解资产，比例已固定</div>
      )}
    </>
  );
}

/** 目标集数 + 单集时长 */
export function EpisodesPicker({
  target,
  seconds,
  onTarget,
  onSeconds,
}: {
  target: number;
  seconds: number;
  onTarget: (n: number) => void;
  onSeconds: (n: number) => void;
}) {
  return (
    <>
      {TARGET_EPISODE_OPTIONS.map((n) => (
        <OptionRow key={n} active={target === n} onClick={() => onTarget(n)}>
          {n === 0 ? "自动" : `${n} 集`}
          {n === 0 && <span className="text-muted-foreground ml-auto text-xs">按剧本已有分集</span>}
        </OptionRow>
      ))}
      <MenuLabel>单集时长</MenuLabel>
      <div className="flex gap-1 px-1 pb-1">
        {EPISODE_SECOND_OPTIONS.map((n) => (
          <button
            key={n}
            type="button"
            aria-pressed={seconds === n}
            onClick={() => onSeconds(n)}
            className={cn(
              "focus-visible:ring-ring/50 h-8 flex-1 rounded-md border text-sm tabular-nums outline-none focus-visible:ring-2",
              seconds === n
                ? "bg-primary text-primary-foreground border-transparent"
                : "hover:bg-muted",
            )}
          >
            {n}s
          </button>
        ))}
      </div>
    </>
  );
}

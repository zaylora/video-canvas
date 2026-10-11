import { ChevronDown, Check, Maximize } from "lucide-react";

import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { cn } from "@/lib/utils";
import {
  OUTPAINT_MULTS,
  RATIO_PRESETS,
  detectRatio,
  fitsCap,
  frameByRatio,
  longMult,
  type OriginSize,
  type OutpaintFrame,
} from "@/utils/canvas/outpaint-frame";

/** 比例条上一个按钮的样式 */
const CHIP_CLASS = cn(
  "nodrag inline-flex h-8 shrink-0 items-center gap-2 rounded-lg px-3 text-[13px] font-semibold whitespace-nowrap outline-none",
  "transition-[background-color,color,opacity] hover:bg-chrome-hover focus-visible:ring-node-ring/60 focus-visible:ring-2",
  "aria-pressed:bg-muted disabled:pointer-events-none disabled:opacity-35 [&_svg]:shrink-0",
);

/** 按宽高比画的小圆角矩形图标 */
function RatioIcon({ ratio }: { ratio: number }) {
  const w = ratio >= 1 ? 14 : 14 * ratio;
  const h = ratio >= 1 ? 14 / ratio : 14;
  return (
    <svg viewBox="0 0 16 16" className="size-4" fill="none" stroke="currentColor" strokeWidth="1.6">
      <rect x={8 - w / 2} y={8 - h / 2} width={w} height={h} rx="2.4" />
    </svg>
  );
}

const fmtMult = (mult: number) => `${Math.round(mult * 100) / 100}x`;

/**
 * 扩图面板顶上的比例条（设计稿 6.17，照原型）：倍数菜单 + 原比例 + 比例预设。
 * 倍数只在「原比例」下可选；放不下（超过 3 倍）的预设置灰并写原因。
 */
export function OutpaintRatioBar({
  origin,
  frame,
  disabled,
  onPickMult,
  onPickOriginal,
  onPickRatio,
}: {
  origin: OriginSize;
  frame: OutpaintFrame;
  disabled?: boolean;
  /** 选了倍数（原比例下放大几倍） */
  onPickMult: (mult: number) => void;
  /** 选了「原比例」 */
  onPickOriginal: () => void;
  /** 选了比例预设 */
  onPickRatio: (ratio: number) => void;
}) {
  const current = detectRatio(origin, frame);
  const isOriginal = current === "orig";
  const mult = longMult(origin, frame);

  return (
    <div
      role="toolbar"
      aria-label="扩图比例"
      className="nodrag nowheel flex max-w-full items-center gap-0.5 overflow-x-auto"
    >
      <DropdownMenu modal={false}>
        <DropdownMenuTrigger
          disabled={disabled || !isOriginal}
          aria-label="扩大倍数"
          title={isOriginal ? "扩大倍数" : "倍数只在「原比例」下可选"}
          className={cn(CHIP_CLASS, "gap-1 px-2.5 font-mono tabular-nums")}
        >
          {isOriginal ? fmtMult(mult) : "—"}
          <ChevronDown className="size-3.5 opacity-60" />
        </DropdownMenuTrigger>
        <DropdownMenuContent align="start" sideOffset={6} className="w-auto min-w-28 rounded-xl">
          {OUTPAINT_MULTS.map((item) => (
            <DropdownMenuItem
              key={item}
              className="h-8 justify-between gap-4 font-mono tabular-nums"
              onClick={() => onPickMult(item)}
            >
              {fmtMult(item)}
              {isOriginal && Math.abs(mult - item) < 0.005 && <Check className="size-3.5" />}
            </DropdownMenuItem>
          ))}
        </DropdownMenuContent>
      </DropdownMenu>

      <span className="bg-chrome-border mx-1.5 h-5 w-px shrink-0" />

      <button
        type="button"
        className={CHIP_CLASS}
        aria-pressed={isOriginal}
        disabled={disabled}
        onClick={onPickOriginal}
      >
        <Maximize className="size-4" />
        <span className="max-md:hidden">原比例</span>
      </button>
      {RATIO_PRESETS.map((preset) => {
        const over = !fitsCap(origin, frameByRatio(origin, preset.r));
        return (
          <button
            key={preset.key}
            type="button"
            className={CHIP_CLASS}
            aria-pressed={current === preset.key}
            disabled={disabled || over}
            title={over ? "超过 3 倍上限" : undefined}
            onClick={() => onPickRatio(preset.r)}
          >
            <RatioIcon ratio={preset.r} />
            {preset.key}
          </button>
        );
      })}
    </div>
  );
}

import { ArrowUp, ChevronDown, Loader2, Scan, Sparkle } from "lucide-react";
import { motion } from "motion/react";

import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { TAP } from "@/lib/motion";
import { cn } from "@/lib/utils";
import {
  OUTPAINT_MULTS,
  OUTPAINT_RATIOS,
  detectRatio,
  fitsCap,
  formatMult,
  frameByRatio,
  longMult,
  type ImageSize,
  type OutpaintFrame,
  type RatioKey,
} from "@/utils/canvas/outpaint";

/** 比例小图标：按真实宽高比画圆角矩形 */
function RatioIcon({ ratio }: { ratio: number }) {
  const w = ratio >= 1 ? 14 : 14 * ratio;
  const h = ratio >= 1 ? 14 / ratio : 14;
  return (
    <svg viewBox="0 0 16 16" className="size-4 shrink-0" fill="none" aria-hidden>
      <rect
        x={8 - w / 2}
        y={8 - h / 2}
        width={w}
        height={h}
        rx="2.4"
        stroke="currentColor"
        strokeWidth="1.6"
      />
    </svg>
  );
}

/** 比例条上的按钮：当前比例高亮，超出 3 倍上限的置灰 */
const CHIP_CLASS =
  "focus-visible:ring-node-ring/60 hover:bg-chrome-hover aria-pressed:bg-muted inline-flex h-8 shrink-0 items-center gap-2 rounded-lg px-3 text-[13px] font-semibold whitespace-nowrap outline-none focus-visible:ring-2 disabled:pointer-events-none disabled:opacity-35 transition-colors";

type OutpaintPanelProps = {
  /** 原图的像素尺寸 */
  size: ImageSize;
  /** 当前框 */
  frame: OutpaintFrame;
  /** 面板宽度（屏幕像素） */
  width: number;
  /** 提示词 */
  prompt: string;
  onPrompt: (value: string) => void;
  /** 点比例按钮 */
  onPickRatio: (key: RatioKey) => void;
  /** 选倍数 */
  onPickMult: (mult: number) => void;
  /** 这次要花的积分，没有价格信息为 undefined */
  credits: number | undefined;
  /** 积分不够：发送键禁用 */
  insufficient: boolean;
  /** 提交中 */
  busy: boolean;
  onSubmit: () => void;
};

/**
 * 扩图面板（设计稿 6.17）：上面一条比例条（倍数下拉 + 原比例 + 五个预设），下面是提示词和发送。
 * 倍数只在「原比例」下可选，其他比例（包括拖手柄拖出的自定义比例）时置灰显示「—」。
 * 提示词可以留空，固定指令由提交时补上。
 */
export function OutpaintPanel({
  size,
  frame,
  width,
  prompt,
  onPrompt,
  onPickRatio,
  onPickMult,
  credits,
  insufficient,
  busy,
  onSubmit,
}: OutpaintPanelProps) {
  const current = detectRatio(size, frame);
  const isOrig = current === "orig";
  const mult = longMult(size, frame);
  const blocked = insufficient || busy;

  return (
    // nodrag nopan nowheel：在面板里选字、点按钮、滚动，都不能带动画布
    <div
      className="nodrag nopan nowheel flex flex-col items-center gap-2 text-left"
      style={{ width }}
    >
      <div
        role="toolbar"
        aria-label="扩图比例"
        className="bg-panel text-popover-foreground ring-foreground/5 flex max-w-full items-center gap-0.5 overflow-x-auto rounded-2xl p-1.5 shadow-2xl ring-1 [&::-webkit-scrollbar]:hidden"
      >
        <DropdownMenu modal={false}>
          <DropdownMenuTrigger
            disabled={!isOrig}
            aria-label="扩大倍数"
            title={isOrig ? "扩大倍数" : "倍数只在「原比例」下可选"}
            className={cn(CHIP_CLASS, "gap-1 px-2.5 tabular-nums")}
          >
            {isOrig ? formatMult(mult) : "—"}
            <ChevronDown className="size-3.5 opacity-60" />
          </DropdownMenuTrigger>
          {/* 菜单向下展开，盖在提示词面板上，不挡住上面的框 */}
          <DropdownMenuContent
            side="bottom"
            align="start"
            sideOffset={8}
            className="w-auto min-w-28 rounded-xl"
          >
            <DropdownMenuRadioGroup
              value={String(OUTPAINT_MULTS.find((m) => Math.abs(m - mult) < 0.005) ?? "")}
              onValueChange={(value) => onPickMult(Number(value))}
            >
              {OUTPAINT_MULTS.map((m) => (
                <DropdownMenuRadioItem key={m} value={String(m)} className="tabular-nums">
                  {formatMult(m)}
                </DropdownMenuRadioItem>
              ))}
            </DropdownMenuRadioGroup>
          </DropdownMenuContent>
        </DropdownMenu>

        <span aria-hidden className="bg-chrome-border mx-1.5 h-5 w-px shrink-0" />

        <motion.button
          type="button"
          whileTap={TAP}
          aria-pressed={current === "orig"}
          onClick={() => onPickRatio("orig")}
          className={CHIP_CLASS}
        >
          <Scan className="size-4" />
          原比例
        </motion.button>
        {OUTPAINT_RATIOS.map((item) => {
          const over = !fitsCap(size, frameByRatio(size, item.ratio));
          return (
            <motion.button
              key={item.key}
              type="button"
              whileTap={over ? undefined : TAP}
              aria-pressed={current === item.key}
              disabled={over}
              title={over ? "超过 3 倍上限" : undefined}
              onClick={() => onPickRatio(item.key)}
              className={cn(CHIP_CLASS, "tabular-nums")}
            >
              <RatioIcon ratio={item.ratio} />
              {item.key}
            </motion.button>
          );
        })}
      </div>

      <div className="bg-panel text-popover-foreground ring-foreground/5 flex w-full items-center gap-3 rounded-2xl py-3 pr-3 pl-4 shadow-2xl ring-1">
        <textarea
          value={prompt}
          rows={1}
          aria-label="提示词"
          placeholder="描述你想如何修改图片"
          onChange={(event) => onPrompt(event.target.value)}
          onKeyDown={(event) => {
            // 中文输入法选词时的回车不算发送
            if (event.key === "Enter" && !event.shiftKey && !event.nativeEvent.isComposing) {
              event.preventDefault();
              if (!blocked) onSubmit();
            }
          }}
          className="placeholder:text-muted-foreground field-sizing-content max-h-30 min-h-6 min-w-0 flex-1 resize-none bg-transparent leading-6 outline-none"
        />
        <span
          className="flex shrink-0 items-center gap-3"
          title={insufficient ? "积分不足" : undefined}
        >
          <span
            className={cn(
              "flex items-center gap-1 text-[13px] font-medium tabular-nums",
              insufficient ? "text-destructive" : "text-muted-foreground",
            )}
          >
            <Sparkle
              className={cn("size-3.5", insufficient ? "fill-destructive" : "fill-credit")}
              strokeWidth={0}
            />
            {credits === undefined ? "—" : credits}
          </span>
          <motion.button
            type="button"
            whileTap={blocked ? undefined : { scale: 0.92 }}
            whileHover={blocked ? undefined : { scale: 1.05 }}
            aria-label={busy ? "生成中" : "开始扩图"}
            disabled={blocked}
            onClick={onSubmit}
            className={cn(
              "bg-foreground text-background focus-visible:ring-node-ring/60 grid size-9 place-items-center rounded-full outline-none transition-colors focus-visible:ring-2 disabled:pointer-events-none",
              blocked && "bg-foreground/35",
            )}
          >
            {busy ? (
              <Loader2 className="size-4 animate-spin" />
            ) : (
              <ArrowUp className="size-4 stroke-[2.5]" />
            )}
          </motion.button>
        </span>
      </div>
    </div>
  );
}

import { useRef, useState } from "react";
import { useVirtualizer } from "@tanstack/react-virtual";
import { Check, Lock } from "lucide-react";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { cn } from "@/lib/utils";

/** 选集网格里的一项（写剧本 / 拆解两个弹窗共用的最小形状） */
export interface PickOption {
  /** 集号 */
  no: number;
  /** 标题 */
  title: string;
  /** 不能选 */
  disabled: boolean;
  /** 不能选的原因 */
  reason: string | null;
  /** 已有剧本：可以重新生成，标「已有剧本」（只有写剧本弹窗用） */
  hasScript?: boolean;
}

/** 网格列数：固定 3 列，每行一个虚拟行 */
const COLUMNS = 3;
/** 一行的高度（含行距） */
const ROW_SIZE = 42;

/**
 * 选集网格：3 列，集多（长篇一次规划上千集）时用虚拟列表，只渲染可见的行。
 * 已拆解的集带小锁，不能选的置灰并在悬停时说明原因。
 */
export function EpisodeGrid({
  options,
  selected,
  onToggle,
  maxHeightClass = "max-h-72",
}: {
  /** 全部可列出的集（已规划的） */
  options: readonly PickOption[];
  /** 选中的集号 */
  selected: readonly number[];
  /** 点一集 */
  onToggle: (no: number) => void;
  /** 滚动区最大高度 */
  maxHeightClass?: string;
}) {
  const scroller = useRef<HTMLDivElement>(null);
  const rows = Math.ceil(options.length / COLUMNS);
  // useVirtualizer 的返回值不能被 React Compiler 记忆化；这里只在本组件内使用，不传给 memo 子组件
  // oxlint-disable-next-line react/incompatible-library
  const virtual = useVirtualizer({
    count: rows,
    getScrollElement: () => scroller.current,
    estimateSize: () => ROW_SIZE,
    overscan: 6,
    // 首屏（含服务端渲染 / 测试）还没量过容器时按这个高度出行，挂载后由真实尺寸接管
    initialRect: { width: 520, height: 288 },
  });
  const chosen = new Set(selected);

  return (
    <div ref={scroller} data-slot="episode-grid" className={cn("overflow-y-auto", maxHeightClass)}>
      <div className="relative w-full" style={{ height: virtual.getTotalSize() }}>
        {virtual.getVirtualItems().map((row) => (
          <div
            key={row.index}
            className="absolute top-0 left-0 grid w-full grid-cols-3 gap-1.5"
            style={{ height: row.size - 6, transform: `translateY(${row.start}px)` }}
          >
            {options.slice(row.index * COLUMNS, row.index * COLUMNS + COLUMNS).map((option) => {
              const on = chosen.has(option.no);
              return (
                <button
                  key={option.no}
                  type="button"
                  aria-pressed={on}
                  disabled={option.disabled}
                  title={option.reason ?? undefined}
                  onClick={() => onToggle(option.no)}
                  className={cn(
                    "focus-visible:ring-ring/50 flex min-w-0 items-center gap-2 rounded-lg border px-2 text-left text-sm outline-none focus-visible:ring-2 disabled:cursor-not-allowed disabled:opacity-45",
                    on ? "border-foreground bg-muted" : "hover:bg-muted/60",
                  )}
                >
                  <span className="bg-background grid size-4 shrink-0 place-items-center rounded border">
                    {on ? (
                      <Check className="size-3" />
                    ) : option.reason === "已拆解" ? (
                      <Lock className="size-2.5" />
                    ) : null}
                  </span>
                  <span className="text-muted-foreground tabular-nums">{option.no}.</span>
                  <span className="min-w-0 flex-1 truncate">{option.title}</span>
                  {option.hasScript && !option.disabled && (
                    <span className="bg-muted text-muted-foreground shrink-0 rounded-full px-1.5 text-[10px] leading-4">
                      已有剧本
                    </span>
                  )}
                </button>
              );
            })}
          </div>
        ))}
      </div>
    </div>
  );
}

/**
 * 「按范围选：第 [a]–[b] 集」：填两个集号，点「选中」把范围内能选的集都选上（替换当前选择）
 */
export function RangeSelect({
  max,
  onSelect,
}: {
  /** 最大集号：输入框的上限，默认填满全范围 */
  max: number;
  /** 点「选中」，带上 a、b（可能 a > b，由调用方对调） */
  onSelect: (from: number, to: number) => void;
}) {
  const [from, setFrom] = useState("1");
  const [to, setTo] = useState(String(Math.min(10, Math.max(1, max))));
  const num = (value: string) => Math.max(1, Math.floor(Number(value) || 1));
  return (
    <div className="text-muted-foreground flex items-center gap-1.5 text-[13px]">
      按范围选：第
      <Input
        type="number"
        min={1}
        max={max}
        value={from}
        aria-label="起始集"
        onChange={(event) => setFrom(event.target.value)}
        className="h-8 w-20 px-2 tabular-nums"
      />
      –
      <Input
        type="number"
        min={1}
        max={max}
        value={to}
        aria-label="结束集"
        onChange={(event) => setTo(event.target.value)}
        className="h-8 w-20 px-2 tabular-nums"
      />
      集
      <Button variant="outline" size="xs" onClick={() => onSelect(num(from), num(to))}>
        选中
      </Button>
    </div>
  );
}

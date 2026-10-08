import { useLayoutEffect, useMemo, useRef, useState, type KeyboardEvent } from "react";
import { Tooltip as TooltipPrimitive } from "@base-ui/react/tooltip";
import { Sparkles } from "lucide-react";
import { Link } from "react-router";

import type { ActivityDto } from "@/api/me/type";
import { cn } from "@/lib/utils";

import {
  buildHeatmap,
  cellLabel,
  moveCell,
  recentYearStart,
  tooltipLines,
  type CellPos,
  type HeatCell,
} from "./heatmap";

/** 格子边长（px） */
const CELL = 11;
/** 格子间距（px） */
const GAP = 3;
/** 一格加间距 */
const STEP = CELL + GAP;
/** 左侧星期标签宽度 */
const LABEL_W = 24;
/** 顶部月份标签高度 */
const LABEL_H = 16;
/** 右侧留白：最后一列若是月份起点，"10月" 从该列左边画起，文字比一列宽，不留白会被 SVG 边界裁掉 */
const RIGHT_PAD = 16;
/** 左侧只标一、三、五 */
const WEEKDAY_LABELS: [number, string][] = [
  [1, "一"],
  [3, "三"],
  [5, "五"],
];

/** 格子的 key，也用来找 DOM 元素 */
const keyOf = (pos: CellPos) => `${pos.row}-${pos.col}`;

/**
 * 生成活跃热力图（设计 docs/design/个人中心 §6.4）：手写 SVG，7 行（周日到周六）× 53 列。
 * - 颜色走 --heat-0..4，明暗主题各一套。
 * - ARIA grid + roving tabindex：方向键逐格，Home/End 到行首尾，PageUp/PageDown 到列首尾；
 *   悬停与聚焦都显示 tooltip，aria-label 与 tooltip 文案一致。
 * - 宽度不够时横向滚动，默认滚到最右（最近）。
 * - tooltip 只挂一个，锚定到当前格子，免得几百个格子各挂一个浮层。
 * @param data GET /me/activity 的响应
 * @param year 选中的自然年；null 表示最近一年
 */
export function ActivityHeatmap({ data, year }: { data: ActivityDto; year: number | null }) {
  const model = useMemo(
    () =>
      buildHeatmap({
        start: year === null ? recentYearStart(data.end) : data.start,
        end: data.end,
        days: data.days,
      }),
    [data, year],
  );
  const scrollRef = useRef<HTMLDivElement>(null);
  const cellRefs = useRef(new Map<string, SVGRectElement>());
  const [focusPos, setFocusPos] = useState<CellPos | null>(null);
  const [active, setActive] = useState<{ cell: HeatCell; el: SVGRectElement } | null>(null);
  const [prevModel, setPrevModel] = useState(model);

  /** 换了数据（切年份）：可聚焦格回到最后一天，tooltip 收起 */
  if (prevModel !== model) {
    setPrevModel(model);
    setFocusPos(null);
    setActive(null);
  }
  const tabPos = focusPos ?? model.lastCell;

  useLayoutEffect(() => {
    const el = scrollRef.current;
    if (el) el.scrollLeft = el.scrollWidth;
  }, [model]);

  const cellAt = (pos: CellPos) => model.columns[pos.col]?.[pos.row] ?? null;

  const show = (pos: CellPos) => {
    const cell = cellAt(pos);
    const el = cellRefs.current.get(keyOf(pos));
    if (cell && el) setActive({ cell, el });
  };

  const onKeyDown = (event: KeyboardEvent<SVGSVGElement>) => {
    if (!tabPos) return;
    const next = moveCell(model, tabPos, event.key);
    const handled = ["ArrowLeft", "ArrowRight", "ArrowUp", "ArrowDown", "Home", "End"].includes(
      event.key,
    );
    if (!next) {
      if (handled || event.key.startsWith("Page")) event.preventDefault();
      return;
    }
    event.preventDefault();
    setFocusPos(next);
    cellRefs.current.get(keyOf(next))?.focus();
  };

  const width = LABEL_W + model.columns.length * STEP + RIGHT_PAD;
  const height = LABEL_H + 7 * STEP;
  const [first, second] = active ? tooltipLines(active.cell.date, active.cell.day) : ["", ""];

  return (
    <div className="grid gap-2">
      <div ref={scrollRef} className="overflow-x-auto overscroll-x-contain pb-1">
        <svg
          width={width}
          height={height}
          role="grid"
          aria-label="生成活跃热力图，方向键移动"
          aria-rowcount={7}
          aria-colcount={model.columns.length}
          className="block"
          onKeyDown={onKeyDown}
          onMouseLeave={() => setActive(null)}
        >
          {model.months.map((month) => (
            <text
              key={`${month.col}-${month.label}`}
              aria-hidden
              x={LABEL_W + month.col * STEP}
              y={10}
              className="fill-muted-foreground text-[10px]"
            >
              {month.label}
            </text>
          ))}
          {WEEKDAY_LABELS.map(([row, label]) => (
            <text
              key={row}
              aria-hidden
              x={0}
              y={LABEL_H + row * STEP + CELL - 1}
              className="fill-muted-foreground text-[10px]"
            >
              {label}
            </text>
          ))}
          {Array.from({ length: 7 }, (_, row) => (
            <g key={row} role="row" aria-rowindex={row + 1}>
              {model.columns.map((column, col) => {
                const cell = column[row];
                if (!cell) return null;
                const pos = { row, col };
                const isTab = tabPos?.row === row && tabPos.col === col;
                return (
                  <rect
                    key={col}
                    ref={(el) => {
                      if (el) cellRefs.current.set(keyOf(pos), el);
                      else cellRefs.current.delete(keyOf(pos));
                    }}
                    role="gridcell"
                    aria-colindex={col + 1}
                    tabIndex={isTab ? 0 : -1}
                    data-date={cell.date}
                    aria-label={cellLabel(cell.date, cell.day)}
                    x={LABEL_W + col * STEP}
                    y={LABEL_H + row * STEP}
                    width={CELL}
                    height={CELL}
                    rx={2}
                    style={{ fill: `var(--heat-${cell.level})` }}
                    className="focus-visible:stroke-foreground outline-none focus-visible:stroke-[1.5]"
                    onMouseEnter={() => show(pos)}
                    onFocus={() => {
                      setFocusPos(pos);
                      show(pos);
                    }}
                    onBlur={() => setActive(null)}
                  />
                );
              })}
            </g>
          ))}
        </svg>
      </div>

      <TooltipPrimitive.Root open={!!active}>
        <TooltipPrimitive.Portal>
          <TooltipPrimitive.Positioner
            anchor={active?.el ?? null}
            side="top"
            sideOffset={6}
            className="isolate z-50"
          >
            <TooltipPrimitive.Popup className="bg-foreground text-background pointer-events-none z-50 grid max-w-xs gap-0.5 rounded-md px-3 py-1.5 text-xs">
              <span className="tabular-nums">{first}</span>
              {second && <span className="tabular-nums opacity-75">{second}</span>}
            </TooltipPrimitive.Popup>
          </TooltipPrimitive.Positioner>
        </TooltipPrimitive.Portal>
      </TooltipPrimitive.Root>

      {model.total === 0 && (
        <p className="text-muted-foreground flex items-center gap-1.5 text-xs">
          <Sparkles className="size-3.5" />
          还没有生成记录，
          <Link to="/" className="text-foreground underline-offset-4 hover:underline">
            去画布里试试
          </Link>
        </p>
      )}

      <div className="text-muted-foreground flex flex-wrap items-center gap-x-4 gap-y-1 text-xs tabular-nums">
        <span>
          {year === null ? "过去一年共 " : `${year} 年共 `}
          <b className="text-foreground font-semibold">{model.total.toLocaleString()}</b> 次生成 ·
          活跃 {model.activeDays} 天
        </span>
        <span>
          最长连续 {model.longestStreak} 天
          {year === null && ` · 当前连续 ${model.currentStreak} 天`}
        </span>
        <span className="ml-auto flex items-center gap-1" aria-hidden>
          少
          {[0, 1, 2, 3, 4].map((level) => (
            <span
              key={level}
              className={cn("inline-block size-[11px] rounded-[2px]")}
              style={{ background: `var(--heat-${level})` }}
            />
          ))}
          多
        </span>
      </div>
    </div>
  );
}

import { useState } from "react";

import { cn } from "@/lib/utils";
import {
  GRID_PICKER_TITLE,
  PICKER_DISABLED_TITLE,
  isCellHighlighted,
  isCellPickable,
  pickerCells,
  pickerLabel,
  type PickerCell,
} from "@/utils/canvas/grid-picker";

/** 全部格子，不会变，模块里算一次 */
const CELLS = pickerCells();

/**
 * 宫格切分「自定义」的 5×5 格子选择器（设计稿 §4.1）：鼠标划过哪个格，从左上到该格的矩形高亮，
 * 标题右侧显示 `行×列`，点击确认。(1,1) 不可选（至少 2 格），但在选区里时照样跟着高亮。
 * 键盘聚焦到格子上和悬停一样会高亮，回车确认。
 * @param onPick 选定了行列；不可选的格子不会触发
 */
export function GridPicker({ onPick }: { onPick: (rows: number, cols: number) => void }) {
  const [hover, setHover] = useState<PickerCell | null>(null);

  return (
    <div className="flex flex-col gap-2" data-slot="grid-picker">
      <div className="text-muted-foreground flex items-center justify-between gap-6 text-xs">
        <span>{GRID_PICKER_TITLE}</span>
        <b className="text-foreground font-medium tabular-nums">{pickerLabel(hover)}</b>
      </div>
      <div
        className="grid grid-cols-5 gap-[5px]"
        onMouseLeave={() => setHover(null)}
        onBlur={(event) => {
          if (!event.currentTarget.contains(event.relatedTarget)) setHover(null);
        }}
      >
        {CELLS.map((cell) => {
          const pickable = isCellPickable(cell);
          const lit = isCellHighlighted(hover, cell);
          return (
            <button
              key={`${cell.rows}-${cell.cols}`}
              type="button"
              aria-label={`${cell.rows}×${cell.cols}`}
              aria-disabled={!pickable || undefined}
              title={pickable ? undefined : PICKER_DISABLED_TITLE}
              className={cn(
                "focus-visible:ring-node-ring/70 size-6 rounded-[5px] outline-none focus-visible:ring-2",
                lit ? "bg-[oklch(0.78_0.12_265)]" : "bg-foreground/15",
                pickable ? "cursor-pointer" : "cursor-not-allowed",
                !pickable && !lit && "opacity-35",
              )}
              onMouseEnter={() => setHover(cell)}
              onFocus={() => setHover(cell)}
              onClick={() => {
                if (pickable) onPick(cell.rows, cell.cols);
              }}
            />
          );
        })}
      </div>
    </div>
  );
}

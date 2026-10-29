import { useCallback, useRef, useState } from "react";

import {
  marqueeBox,
  marqueeHits,
  marqueeSelection,
  type Box,
  type Point,
} from "@/utils/drama/marquee";

/** 正在进行的一次框选：按下点（相对容器）、开始前的选择、是否追加 */
type Drag = { start: Point; base: ReadonlySet<string>; additive: boolean };

const sameSet = (a: ReadonlySet<string>, b: ReadonlySet<string>) =>
  a.size === b.size && [...a].every((id) => b.has(id));

/**
 * 资产列表的鼠标框选：在空白处（不是按钮上）按下拖动画框，框到的可选卡片即选中。
 * 卡片按 data-asset-id 量位置；坐标都相对容器，容器跟着滚动，滚动中框选也对得上。
 * 只点一下空白（没拖动、没按 Shift / Ctrl）视为清空选择。
 * @param options.selectable 可以被框选的卡片 id
 * @param options.selected 当前选择
 * @param options.onSelect 框选改变了选择
 * @param options.onClear 点了一下空白
 * @returns 挂到容器上的 ref 与指针事件，以及当前选框（没有在框选为 null）
 */
export function useMarquee(options: {
  selectable: readonly string[];
  selected: ReadonlySet<string>;
  onSelect: (next: ReadonlySet<string>) => void;
  onClear: () => void;
}) {
  const { selectable, selected, onSelect, onClear } = options;
  const ref = useRef<HTMLDivElement>(null);
  const drag = useRef<Drag | null>(null);
  const [box, setBox] = useState<Box | null>(null);

  const local = useCallback((event: { clientX: number; clientY: number }): Point => {
    const rect = ref.current?.getBoundingClientRect();
    return { x: event.clientX - (rect?.left ?? 0), y: event.clientY - (rect?.top ?? 0) };
  }, []);

  const onPointerDown = useCallback(
    (event: React.PointerEvent<HTMLDivElement>) => {
      if (event.button !== 0) return;
      // 卡片、按钮自己处理点击；只有空白处才开始框选
      if ((event.target as Element).closest("button, a, input, textarea, [role='button']")) return;
      event.preventDefault();
      event.currentTarget.setPointerCapture(event.pointerId);
      drag.current = {
        start: local(event),
        base: selected,
        additive: event.shiftKey || event.ctrlKey || event.metaKey,
      };
    },
    [local, selected],
  );

  const onPointerMove = useCallback(
    (event: React.PointerEvent<HTMLDivElement>) => {
      const current = drag.current;
      const container = ref.current;
      if (!current || !container) return;
      const next = marqueeBox(current.start, local(event));
      setBox(next);
      if (!next) return;
      const origin = container.getBoundingClientRect();
      const cards = selectable.flatMap((id) => {
        const element = container.querySelector(`[data-asset-id="${CSS.escape(id)}"]`);
        if (!element) return [];
        const rect = element.getBoundingClientRect();
        return [
          {
            id,
            rect: {
              left: rect.left - origin.left,
              top: rect.top - origin.top,
              width: rect.width,
              height: rect.height,
            },
          },
        ];
      });
      const picked = marqueeSelection(current.base, marqueeHits(next, cards), current.additive);
      if (!sameSet(picked, selected)) onSelect(picked);
    },
    [local, onSelect, selectable, selected],
  );

  const finish = useCallback(
    (event: React.PointerEvent<HTMLDivElement>, cancelled: boolean) => {
      const current = drag.current;
      drag.current = null;
      setBox(null);
      if (!current || cancelled) return;
      if (!marqueeBox(current.start, local(event)) && !current.additive) onClear();
    },
    [local, onClear],
  );

  return {
    ref,
    box,
    handlers: {
      onPointerDown,
      onPointerMove,
      onPointerUp: (event: React.PointerEvent<HTMLDivElement>) => finish(event, false),
      onPointerCancel: (event: React.PointerEvent<HTMLDivElement>) => finish(event, true),
    },
  };
}

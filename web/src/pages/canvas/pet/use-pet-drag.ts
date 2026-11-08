import {
  useCallback,
  useEffect,
  useLayoutEffect,
  useRef,
  type PointerEvent as ReactPointerEvent,
} from "react";

import { PET_MARGIN, type Rect, type Size } from "@/utils/pet/placement";

/** 移动超过这么多像素才算拖动，否则松手算点击 */
const DRAG_THRESHOLD = 4;
/** 按最近这么久的水平位移决定朝向 */
const DIR_WINDOW_MS = 80;

export type DragDir = "left" | "right" | null;

export const petTransform = (x: number, y: number) => `translate3d(${x}px, ${y}px, 0)`;

type Options = {
  /** 桌宠最外层元素：拖动中直接改它的 transform，不走 React */
  elRef: React.RefObject<HTMLElement | null>;
  /** 当前提交的位置（区域坐标） */
  rect: Rect;
  /** 画布区域尺寸 */
  area: Size;
  /** 窄屏等不能拖的场合为 false */
  enabled: boolean;
  /** 拖动开始 / 结束（只在状态翻转时调用，不是每次移动） */
  onDraggingChange: (dragging: boolean) => void;
  /** 水平朝向变了（最近 80ms 的位移），几乎不动是 null */
  onDirChange: (dir: DragDir) => void;
  /** 松手：最终位置，已夹在区域内 */
  onCommit: (rect: Rect) => void;
  /** 没进入拖动的点击（含键盘 Enter / 空格） */
  onClick: () => void;
};

type Session = {
  pointerId: number;
  startX: number;
  startY: number;
  originX: number;
  originY: number;
  dragging: boolean;
  /** 最近一次算出的位置 */
  x: number;
  y: number;
  samples: Array<{ t: number; x: number }>;
  dir: DragDir;
  lastMoveAt: number;
};

/**
 * 桌宠的拖动：pointer 事件，移动超过 4px 才算拖动；拖动中只改元素的 transform，
 * React 只在开始、结束和朝向变化时渲染。松手时把夹在区域内的位置交给 onCommit。
 * 返回要铺在桌宠按钮上的事件处理。
 */
export function usePetDrag(options: Options) {
  const opts = useRef(options);
  // 事件处理里读最新的选项；放在 layout effect 里同步，渲染期不碰 ref
  useLayoutEffect(() => {
    opts.current = options;
  });
  const session = useRef<Session | null>(null);
  /** 刚拖完：吞掉紧跟着的那次 click */
  const suppressClick = useRef(false);
  const dirTimer = useRef<number | null>(null);

  useEffect(
    () => () => {
      if (dirTimer.current !== null) window.clearTimeout(dirTimer.current);
    },
    [],
  );

  const clampPos = (x: number, y: number) => {
    const { area, rect } = opts.current;
    return {
      x: Math.max(PET_MARGIN, Math.min(x, area.width - PET_MARGIN - rect.width)),
      y: Math.max(PET_MARGIN, Math.min(y, area.height - PET_MARGIN - rect.height)),
    };
  };

  /** 80ms 内没有新的移动，朝向回到 null */
  const armDirTimer = () => {
    if (dirTimer.current !== null) return;
    dirTimer.current = window.setTimeout(function check() {
      const s = session.current;
      dirTimer.current = null;
      if (!s || !s.dragging || s.dir === null) return;
      const rest = DIR_WINDOW_MS - (performance.now() - s.lastMoveAt);
      if (rest > 0) {
        dirTimer.current = window.setTimeout(check, rest);
        return;
      }
      s.dir = null;
      s.samples = [];
      opts.current.onDirChange(null);
    }, DIR_WINDOW_MS);
  };

  const onPointerDown = useCallback((e: ReactPointerEvent<HTMLElement>) => {
    if (e.button !== 0) return;
    suppressClick.current = false;
    const { rect } = opts.current;
    session.current = {
      pointerId: e.pointerId,
      startX: e.clientX,
      startY: e.clientY,
      originX: rect.x,
      originY: rect.y,
      dragging: false,
      x: rect.x,
      y: rect.y,
      samples: [],
      dir: null,
      lastMoveAt: 0,
    };
    e.currentTarget.setPointerCapture(e.pointerId);
  }, []);

  const onPointerMove = (e: ReactPointerEvent<HTMLElement>) => {
    const s = session.current;
    if (!s || s.pointerId !== e.pointerId) return;
    const dx = e.clientX - s.startX;
    const dy = e.clientY - s.startY;
    if (!s.dragging) {
      if (!opts.current.enabled || Math.hypot(dx, dy) <= DRAG_THRESHOLD) return;
      s.dragging = true;
      opts.current.onDraggingChange(true);
    }
    const pos = clampPos(s.originX + dx, s.originY + dy);
    s.x = pos.x;
    s.y = pos.y;
    const el = opts.current.elRef.current;
    if (el) el.style.transform = petTransform(pos.x, pos.y);

    const now = performance.now();
    s.lastMoveAt = now;
    s.samples.push({ t: now, x: pos.x });
    while (s.samples.length > 1 && now - s.samples[0]!.t > DIR_WINDOW_MS) s.samples.shift();
    const moved = pos.x - s.samples[0]!.x;
    const dir: DragDir = moved > 1 ? "right" : moved < -1 ? "left" : null;
    if (dir !== s.dir) {
      s.dir = dir;
      opts.current.onDirChange(dir);
    }
    if (dir !== null) armDirTimer();
  };

  const finish = (e: ReactPointerEvent<HTMLElement>, commit: boolean) => {
    const s = session.current;
    if (!s || s.pointerId !== e.pointerId) return;
    session.current = null;
    if (e.currentTarget.hasPointerCapture(e.pointerId)) {
      e.currentTarget.releasePointerCapture(e.pointerId);
    }
    if (!s.dragging) return;
    suppressClick.current = true;
    if (s.dir !== null) opts.current.onDirChange(null);
    opts.current.onDraggingChange(false);
    if (commit) {
      const { rect } = opts.current;
      opts.current.onCommit({ ...rect, x: s.x, y: s.y });
    } else {
      const el = opts.current.elRef.current;
      const { rect } = opts.current;
      if (el) el.style.transform = petTransform(rect.x, rect.y);
    }
  };

  const onClick = () => {
    if (suppressClick.current) {
      suppressClick.current = false;
      return;
    }
    opts.current.onClick();
  };

  return {
    onPointerDown,
    onPointerMove,
    onPointerUp: (e: ReactPointerEvent<HTMLElement>) => finish(e, true),
    onPointerCancel: (e: ReactPointerEvent<HTMLElement>) => finish(e, false),
    onClick,
  };
}

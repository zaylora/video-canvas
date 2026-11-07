import { useEffect, useState, type CSSProperties } from "react";
import { X } from "lucide-react";

import { cn } from "@/lib/utils";
import { bubbleText, type Bubble, type BubblePriority } from "@/utils/pet/bubble-queue";
import { clampBubbleText } from "@/utils/pet/bubble-text";
import type { Rect, Size } from "@/utils/pet/placement";

/** 气泡离桌宠的距离、最大宽度、哪一侧至少要有这么宽才放得下 */
const GAP = 8;
const MAX_WIDTH = 264;
const MIN_ROOM = 140;

/** 优先级的边框色：只有审批和错误稍有区别，其余沿用清透胶囊的描边 */
const RING: Record<BubblePriority, string> = {
  approval: "ring-status-warning/55",
  error: "ring-destructive/40",
  result: "ring-chrome-border",
  system: "ring-chrome-border",
  easter: "ring-chrome-border",
};

/** 气泡放在桌宠哪一侧：桌宠在画布右半边放左侧，否则放右侧；这一侧空间不够就换另一侧 */
export function bubbleSide(rect: Rect, area: Size): "left" | "right" {
  const preferLeft = rect.x + rect.width / 2 >= area.width / 2;
  const leftRoom = rect.x - GAP;
  const rightRoom = area.width - (rect.x + rect.width) - GAP;
  if (preferLeft) return leftRoom >= MIN_ROOM || leftRoom >= rightRoom ? "left" : "right";
  return rightRoom >= MIN_ROOM || rightRoom >= leftRoom ? "right" : "left";
}

/**
 * 桌宠旁边的气泡：清透胶囊风格，最多两行、约 40 字。
 * × 关闭；悬停暂停计时；Esc 关闭；点击文字执行 onClick（有按钮时按钮在右边）。
 * 计时按 ttl 走，version 变了（合并、更新）重新计时；页面在后台时暂停。
 */
export function PetBubble({
  bubble,
  rect,
  area,
  paused,
  onDismiss,
}: {
  bubble: Bubble;
  rect: Rect;
  area: Size;
  /** 页面在后台：计时暂停 */
  paused: boolean;
  onDismiss: (id: string) => void;
}) {
  const [hovered, setHovered] = useState(false);
  const { id, ttl, version } = bubble;

  useEffect(() => {
    if (ttl === null || hovered || paused) return;
    const timer = window.setTimeout(() => onDismiss(id), ttl);
    return () => window.clearTimeout(timer);
  }, [id, ttl, version, hovered, paused, onDismiss]);

  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      if (event.key === "Escape" && !event.defaultPrevented) onDismiss(id);
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [id, onDismiss]);

  const side = bubbleSide(rect, area);
  const room = side === "left" ? rect.x - GAP : area.width - (rect.x + rect.width) - GAP;
  const style: CSSProperties = {
    top: rect.y + rect.height / 2,
    maxWidth: Math.max(120, Math.min(MAX_WIDTH, room - 4)),
    ...(side === "left"
      ? { right: area.width - rect.x + GAP }
      : { left: rect.x + rect.width + GAP }),
  };

  const text = clampBubbleText(bubbleText(bubble));
  const { onClick, action } = bubble;
  const body = "min-w-0 text-left text-[13px] leading-5";

  return (
    <div
      className="canvas-overlay-interactive pointer-events-auto absolute z-[16] -translate-y-1/2"
      style={style}
      onPointerEnter={() => setHovered(true)}
      onPointerLeave={() => setHovered(false)}
    >
      <div
        role="status"
        data-priority={bubble.priority}
        className={cn(
          "bg-chrome text-foreground flex items-center gap-1.5 rounded-2xl py-1.5 pr-1.5 pl-3.5 shadow-lg ring-1 backdrop-blur-xl backdrop-saturate-150",
          "animate-in fade-in-0 zoom-in-95 duration-150 motion-reduce:animate-none",
          RING[bubble.priority],
        )}
        style={{ transformOrigin: side === "left" ? "right center" : "left center" }}
      >
        {onClick ? (
          <button
            type="button"
            className={cn(body, "line-clamp-2 cursor-pointer outline-none")}
            onClick={() => {
              onClick();
              onDismiss(id);
            }}
          >
            {text}
          </button>
        ) : (
          <span className={cn(body, "line-clamp-2")}>{text}</span>
        )}
        {action && (
          <button
            type="button"
            className="hover:bg-chrome-hover ring-chrome-border h-6 shrink-0 rounded-full px-2.5 text-xs font-medium ring-1 transition-colors"
            onClick={() => {
              action.onClick();
              onDismiss(id);
            }}
          >
            {action.label}
          </button>
        )}
        <button
          type="button"
          aria-label="关闭"
          className="text-muted-foreground hover:bg-chrome-hover hover:text-foreground flex size-6 shrink-0 items-center justify-center rounded-full transition-colors"
          onClick={() => onDismiss(id)}
        >
          <X className="size-3.5" />
        </button>
      </div>
    </div>
  );
}

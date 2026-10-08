import { useEffect, useRef, type ComponentType } from "react";
import { createPortal } from "react-dom";
import { motion, useReducedMotion } from "motion/react";

import { DURATION } from "@/lib/motion";
import { cn } from "@/lib/utils";

/** 菜单里的一行 */
export type SuggestionItem = {
  key: string;
  /** 左侧小图标 */
  icon: ComponentType<{ className?: string }>;
  title: string;
  /** 标题下面的一行说明 */
  hint?: string;
};

/**
 * 输入框里 @、/ 触发的向上弹出菜单：贴着光标，往上长，宽度不够时往左避让。
 * 没有可选项但有 notice 时只显示这句提示（技能还在加载、没有命中等）；两者都没有就不渲染。
 * 按下时编辑器不能失焦，所以行的 mousedown 一律拦掉。
 */
export function SuggestionMenu({
  label,
  rect,
  width,
  items,
  active,
  notice,
  onActive,
  onPick,
}: {
  /** 无障碍名称 */
  label: string;
  /** 光标所在的位置，没有就不显示 */
  rect: DOMRect | null;
  /** 菜单宽度（px） */
  width: number;
  items: SuggestionItem[];
  /** 键盘选中的行 */
  active: number;
  notice?: string | null;
  onActive: (index: number) => void;
  onPick: (index: number) => void;
}) {
  const reduce = useReducedMotion();
  const list = useRef<HTMLUListElement>(null);
  /** 键盘上下选时，选中的行要滚进可视范围 */
  useEffect(() => {
    list.current?.children[active]?.scrollIntoView({ block: "nearest" });
  }, [active]);
  if (!rect || (items.length === 0 && !notice)) return null;
  return createPortal(
    <motion.div
      initial={reduce ? { opacity: 0 } : { opacity: 0, y: 4, scale: 0.96 }}
      animate={{ opacity: 1, y: 0, scale: 1 }}
      transition={{ duration: DURATION.base }}
      style={{
        position: "fixed",
        width,
        left: Math.max(8, Math.min(rect.left, window.innerWidth - width - 8)),
        bottom: window.innerHeight - rect.top + 6,
        transformOrigin: "bottom left",
      }}
      className="bg-popover text-popover-foreground ring-chrome-border z-[60] rounded-xl p-1.5 shadow-lg ring-1"
    >
      {items.length === 0 ? (
        <p className="text-muted-foreground px-2 py-2 text-xs">{notice}</p>
      ) : (
        <ul
          ref={list}
          role="listbox"
          aria-label={label}
          className="flex max-h-64 flex-col gap-0.5 overflow-y-auto"
        >
          {items.map((item, i) => (
            <li key={item.key} role="option" aria-selected={i === active}>
              <button
                type="button"
                onMouseDown={(e) => e.preventDefault()}
                onMouseEnter={() => onActive(i)}
                onClick={() => onPick(i)}
                className={cn(
                  "flex w-full items-start gap-2 rounded-lg px-2 py-1.5 text-left",
                  i === active && "bg-chrome-hover",
                )}
              >
                <item.icon className="text-muted-foreground mt-0.5 size-4 shrink-0" />
                <span className="flex min-w-0 flex-col">
                  <span className="truncate text-[13px]">{item.title}</span>
                  {item.hint && (
                    <span className="text-muted-foreground line-clamp-2 text-xs leading-snug">
                      {item.hint}
                    </span>
                  )}
                </span>
              </button>
            </li>
          ))}
        </ul>
      )}
    </motion.div>,
    document.body,
  );
}

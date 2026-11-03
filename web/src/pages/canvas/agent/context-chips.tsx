import { X } from "lucide-react";

import { cn } from "@/lib/utils";

/** 一个页面上下文引用（后台助手：页面、渠道、模型、插件） */
export type ContextRef = {
  id: string;
  /** 显示的文字，如「模型：可灵 2.1」 */
  label: string;
  /** 悬停说明 */
  title?: string;
};

/** 和输入框、消息里的行内引用 chip 同一种外观 */
const CHIP =
  "ring-chrome-border bg-foreground/7 inline-flex h-6 max-w-44 items-center gap-1 rounded-md px-1.5 text-xs ring-1";

/**
 * 页面上下文引用的一排 chip。
 * 传 onRemove 是输入框里的可移除形态（每个 chip 后面有 ✕）；不传是消息气泡上方的只读形态，靠右对齐。
 * 没有引用时什么都不渲染。
 */
export function ContextChips({
  refs,
  onRemove,
}: {
  refs: readonly ContextRef[];
  onRemove?: (id: string) => void;
}) {
  if (refs.length === 0) return null;
  return (
    <div
      role="group"
      aria-label="页面上下文"
      className={cn("flex flex-wrap gap-1", !onRemove && "max-w-[88%] justify-end")}
    >
      {refs.map((ref) => (
        <span key={ref.id} className={cn(CHIP, onRemove && "pr-0.5")} title={ref.title}>
          <span className="truncate">{ref.label}</span>
          {onRemove && (
            <button
              type="button"
              aria-label={`移除 ${ref.label}`}
              onClick={() => onRemove(ref.id)}
              className="text-muted-foreground hover:bg-foreground/10 hover:text-foreground focus-visible:ring-node-ring/60 grid size-4 shrink-0 place-items-center rounded outline-none focus-visible:ring-2"
            >
              <X aria-hidden className="size-3" />
            </button>
          )}
        </span>
      ))}
    </div>
  );
}

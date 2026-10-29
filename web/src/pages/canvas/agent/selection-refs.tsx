import { useState } from "react";
import { Group } from "lucide-react";
import { motion, useReducedMotion } from "motion/react";

import { DURATION, EASE_OUT, TAP } from "@/lib/motion";
import { cn } from "@/lib/utils";
import { useAgentHighlight } from "@/store/agent-highlight";
import {
  type RefNode,
  SELECTION_SHOWN,
  resolveSelection,
  splitSelection,
} from "@/utils/agent/selection-refs";

import { useFocusNode } from "@/components/canvas-editor/use-focus-node";

/** 标签的外观：底色加内高光，和输入框里的行内引用 chip 区分开（这里只用来回看旧消息的 selection） */
const CHIP =
  "agent-inset bg-foreground/6 inline-flex h-6 max-w-40 items-center gap-1.5 rounded-md px-2 text-xs outline-none transition-colors duration-120 focus-visible:ring-2 focus-visible:ring-node-ring/60";

/**
 * 用户消息上方的「引用了哪些节点」：发送这条消息时画布上选中的节点，一个节点（或组）一个标签，组前面是组的图标、节点前面是小圆点。
 * 最多直接显示 3 个，其余折成「+N」，点它原地展开（展开出来的标签淡入上浮 2px，只动 transform 和 opacity）；
 * 点标签飞到画布上并让节点闪一下描边，节点已经被删除的置灰、不能点。没有引用时什么都不渲染。
 */
export function SelectionRefs({ ids, nodes }: { ids: readonly string[]; nodes: RefNode[] }) {
  const [expanded, setExpanded] = useState(false);
  const focus = useFocusNode();
  const reduce = useReducedMotion();
  const refs = resolveSelection(ids, nodes);
  if (refs.length === 0) return null;
  const { shown, hidden } = splitSelection(refs, expanded);

  return (
    <div
      role="group"
      aria-label={`引用了 ${refs.length} 个节点`}
      className="flex max-w-[88%] flex-wrap justify-end gap-1"
    >
      {shown.map((ref, i) => (
        <motion.button
          key={ref.id}
          type="button"
          disabled={ref.missing}
          title={ref.missing ? "这个节点已经被删除了" : "在画布上定位"}
          // 只有展开时新冒出来的标签才播入场；首次渲染（含翻看历史）不播
          initial={expanded && i >= SELECTION_SHOWN ? { opacity: 0, y: reduce ? 0 : 2 } : false}
          animate={{ opacity: 1, y: 0 }}
          transition={{ duration: DURATION.fast, ease: EASE_OUT }}
          whileTap={ref.missing ? undefined : TAP}
          onClick={() => {
            if (focus(ref.id)) useAgentHighlight.getState().touch([ref.id]);
          }}
          className={cn(
            CHIP,
            ref.missing
              ? "text-muted-foreground cursor-default opacity-70"
              : "hover:bg-foreground/10 cursor-pointer",
          )}
        >
          {ref.kind === "group" ? (
            <Group
              aria-hidden
              className={cn(
                "size-3 shrink-0",
                ref.missing ? "text-muted-foreground/60" : "text-preset",
              )}
            />
          ) : (
            <span
              aria-hidden
              className={cn(
                "size-1.5 shrink-0 rounded-full",
                ref.missing ? "bg-muted-foreground/40" : "bg-preset",
              )}
            />
          )}
          <span className="truncate">{ref.label}</span>
        </motion.button>
      ))}
      {hidden > 0 && (
        <motion.button
          type="button"
          aria-label={`再展开 ${hidden} 个节点`}
          whileTap={TAP}
          onClick={() => setExpanded(true)}
          className={cn(
            CHIP,
            "text-muted-foreground hover:bg-foreground/10 hover:text-foreground cursor-pointer tabular-nums",
          )}
        >
          +{hidden}
        </motion.button>
      )}
    </div>
  );
}

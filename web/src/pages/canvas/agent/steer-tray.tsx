import { useState } from "react";
import { AnimatePresence, motion, useReducedMotion } from "motion/react";
import { Clock } from "lucide-react";

import { DURATION, EASE_OUT } from "@/lib/motion";
import { parseMessage } from "@/utils/agent/chips";

/** 队列最多直接显示几条，其余折叠成「还有 N 条」 */
const MAX_SHOWN = 3;

export type SteerTrayItem = { key: string; text: string };

/**
 * 队列里现在要画哪几条
 * @param items 全部排队的插话
 * @param expanded 用户有没有点开「还有 N 条」
 * @returns 要画的条目，和被折叠的条数
 */
export function visibleSteers(items: readonly SteerTrayItem[], expanded: boolean) {
  const shown = expanded ? items : items.slice(0, MAX_SHOWN);
  return { shown, hidden: items.length - shown.length };
}

/** 一行只放一句话：节点引用显示成名字，超长由样式截断，全文放在 title 里 */
const plain = (text: string) =>
  parseMessage(text)
    .map((p) => (p.kind === "text" ? p.text : p.chip.name))
    .join("")
    .replace(/\s+/g, " ")
    .trim();

/**
 * 插话队列：还没被 Agent 读到的插话，收在输入框顶部的托盘里，一条一行。
 * 读到之后由消息流接手（从这里消失，对话里出现实线气泡）。
 */
export function SteerTray({ items }: { items: readonly SteerTrayItem[] }) {
  const [expanded, setExpanded] = useState(false);
  const reduce = useReducedMotion();
  if (items.length === 0) return null;
  const { shown, hidden } = visibleSteers(items, expanded);
  const collapsible = items.length > MAX_SHOWN;

  return (
    <div className="border-foreground/8 flex flex-col border-b px-1 pt-1 pb-1">
      <AnimatePresence initial={false}>
        {shown.map((item) => {
          const text = plain(item.text);
          return (
            <motion.div
              key={item.key}
              title={text}
              layout={reduce ? false : "position"}
              initial={reduce ? { opacity: 0 } : { opacity: 0, height: 0 }}
              animate={{ opacity: 1, height: 30 }}
              exit={
                reduce
                  ? { opacity: 0, transition: { duration: DURATION.exit } }
                  : { opacity: 0, height: 0, y: -8, transition: { duration: DURATION.exit } }
              }
              transition={{ duration: DURATION.base, ease: EASE_OUT }}
              className="text-foreground/80 flex items-center gap-2 overflow-hidden rounded-lg px-2 text-[12.5px]"
            >
              <Clock className="text-status-running size-3 shrink-0" />
              <span className="min-w-0 flex-1 truncate">{text}</span>
            </motion.div>
          );
        })}
      </AnimatePresence>
      {collapsible && (
        <button
          type="button"
          aria-expanded={expanded}
          onClick={() => setExpanded((v) => !v)}
          className="text-muted-foreground hover:text-foreground focus-visible:ring-node-ring/60 h-6.5 rounded-lg pr-2 pl-7 text-left text-xs outline-none focus-visible:ring-2"
        >
          {expanded ? "收起" : `还有 ${hidden} 条`}
        </button>
      )}
    </div>
  );
}

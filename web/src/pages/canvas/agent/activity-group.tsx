import { useState } from "react";
import { AnimatePresence, motion, useReducedMotion } from "motion/react";
import { Check, ChevronRight, CircleStop, Loader2, TriangleAlert } from "lucide-react";

import { TOOL_LABELS, TOOL_VERBS } from "@/constants/agent";
import { DURATION, EASE_OUT } from "@/lib/motion";
import { cn } from "@/lib/utils";
import { useAgentHighlight } from "@/store/agent-highlight";
import type { TimelineItem, ToolCall } from "@/utils/agent/timeline";

import { useFocusNode } from "../chrome/use-focus-node";

type Activity = Extract<TimelineItem, { type: "activity" }>;

/** 收起后的摘要：「已处理 32s · 读取 2 · 修改 3」，按动词首次出现的顺序计数 */
function summarize(item: Activity) {
  const counts = new Map<string, number>();
  for (const t of item.tools) {
    const verb = TOOL_VERBS[t.name] ?? TOOL_LABELS[t.name] ?? t.name;
    counts.set(verb, (counts.get(verb) ?? 0) + 1);
  }
  const parts = [...counts].map(([verb, n]) => `${verb} ${n}`);
  const seconds =
    item.startedAt !== null && item.endedAt !== null
      ? Math.max(1, Math.round((item.endedAt - item.startedAt) / 1000))
      : null;
  const head = item.status === "aborted" ? "已中止" : seconds ? `已处理 ${seconds}s` : "已处理";
  return [head, ...parts].join(" · ");
}

/**
 * 活动块：同一轮里连续的工具调用合成一块。
 * 进行中自动展开、头部是静态的「处理中」（实时状态只在状态行，避免重复）；结束后自动收成一行摘要。
 * 用户手动开合过之后就不再自动开合，以用户的意图为准。
 */
export function ActivityGroup({ item }: { item: Activity }) {
  const reduce = useReducedMotion();
  /** 用户手动设的开合；null 表示跟随运行状态 */
  const [manual, setManual] = useState<boolean | null>(null);
  const open = manual ?? item.status === "running";
  const running = item.status === "running";

  return (
    <div className="flex flex-col">
      <button
        type="button"
        aria-expanded={open}
        onClick={() => setManual(!open)}
        className="text-muted-foreground hover:bg-chrome-hover hover:text-foreground focus-visible:ring-node-ring/60 -ml-2 inline-flex items-center gap-1.5 self-start rounded-lg px-2 py-0.5 text-[13px] outline-none transition-colors duration-120 focus-visible:ring-2"
      >
        <ChevronRight
          className={cn("size-3.5 transition-transform duration-120", open && "rotate-90")}
        />
        <span>{running ? "处理中" : summarize(item)}</span>
        {item.errors > 0 && <span className="text-status-warning">· {item.errors} 项失败</span>}
      </button>
      <AnimatePresence initial={false}>
        {open && (
          <motion.div
            initial={reduce ? { opacity: 0 } : { opacity: 0, y: -4 }}
            animate={{ opacity: 1, y: 0 }}
            exit={{ opacity: 0, transition: { duration: DURATION.fast * 0.7 } }}
            transition={{ duration: DURATION.fast, ease: EASE_OUT }}
            className="border-foreground/10 mt-1 ml-1.5 flex flex-col gap-px border-l pl-3"
          >
            {item.tools.map((tool) => (
              <ToolRow key={tool.key} tool={tool} />
            ))}
          </motion.div>
        )}
      </AnimatePresence>
    </div>
  );
}

/** 一次工具调用：状态图标 + 名称 + 摘要；涉及节点时点击定位并高亮 */
function ToolRow({ tool }: { tool: ToolCall }) {
  const focus = useFocusNode();
  const canLocate = tool.nodeIds.length > 0;
  const Icon =
    tool.status === "running"
      ? Loader2
      : tool.status === "done"
        ? Check
        : tool.status === "error"
          ? TriangleAlert
          : CircleStop;
  return (
    <button
      type="button"
      disabled={!canLocate}
      onClick={() => {
        if (tool.nodeIds.some((id) => focus(id))) useAgentHighlight.getState().touch(tool.nodeIds);
      }}
      className={cn(
        "text-muted-foreground -ml-2 flex w-full items-center gap-2 rounded-lg px-2 py-[3px] text-left text-xs outline-none",
        "focus-visible:ring-node-ring/60 focus-visible:ring-2",
        canLocate && "hover:bg-chrome-hover hover:text-foreground cursor-pointer",
      )}
    >
      <Icon
        className={cn(
          "size-3.5 shrink-0",
          tool.status === "running" && "animate-spin motion-reduce:animate-none",
          tool.status === "done" && "text-status-success",
          tool.status === "error" && "text-status-warning",
        )}
      />
      <span className="text-foreground shrink-0 font-medium">
        {TOOL_LABELS[tool.name] ?? tool.name}
      </span>
      {tool.summary && <span className="truncate opacity-85">{tool.summary}</span>}
      {tool.status === "aborted" && <span className="shrink-0 opacity-70">已中止</span>}
    </button>
  );
}

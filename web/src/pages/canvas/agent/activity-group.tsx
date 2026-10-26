import { useState } from "react";
import { AnimatePresence, motion, useReducedMotion } from "motion/react";
import {
  BookOpen,
  Check,
  CheckCircle2,
  ChevronRight,
  CircleStop,
  Clapperboard,
  Film,
  Globe,
  LayoutGrid,
  Lightbulb,
  ListChecks,
  ListTodo,
  Loader2,
  MessageCircleQuestion,
  PencilLine,
  ScanEye,
  Search,
  Terminal,
  Trash2,
  TriangleAlert,
  WandSparkles,
  Wrench,
  type LucideIcon,
} from "lucide-react";

import { toolLabel } from "@/constants/agent";
import { DURATION, EASE_OUT } from "@/lib/motion";
import { cn } from "@/lib/utils";
import { useAgentHighlight } from "@/store/agent-highlight";
import { activitySummary, type VerbIcon } from "@/utils/agent/activity";
import type { TimelineItem, ToolCall } from "@/utils/agent/timeline";

import { useFocusNode } from "../chrome/use-focus-node";

type Activity = Extract<TimelineItem, { type: "activity" }>;

/** 动作图标（设计稿 6.12）：同一种动作在摘要和时间线里用同一个图标 */
const VERB_ICON: Record<VerbIcon, LucideIcon> = {
  read: BookOpen,
  edit: PencilLine,
  arrange: LayoutGrid,
  delete: Trash2,
  look: ScanEye,
  plan: ListChecks,
  ask: MessageCircleQuestion,
  models: Clapperboard,
  generate: WandSparkles,
  task: ListTodo,
  skill: Lightbulb,
  script: Terminal,
  video: Film,
  frame: Film,
  search: Search,
  web: Globe,
  other: Wrench,
};

/**
 * 活动行：同一轮里连续的工具调用合成一行。
 * 收起时是「✓ 69s ☰2 ▢1 ✎1」的图标摘要（hover 看中文动词），点开是左侧发丝线的竖向时间线。
 * 进行中自动展开、头部是静态的「处理中」（实时状态在状态行）；结束后自动收起。
 * 用户手动开合过之后就不再自动开合，以用户的意图为准。
 */
export function ActivityGroup({ item }: { item: Activity }) {
  const reduce = useReducedMotion();
  /** 用户手动设的开合；null 表示跟随运行状态 */
  const [manual, setManual] = useState<boolean | null>(null);
  const open = manual ?? item.status === "running";
  const running = item.status === "running";
  const summary = activitySummary(item);
  const HeadIcon = running ? Loader2 : item.status === "aborted" ? CircleStop : CheckCircle2;

  return (
    <div className="flex flex-col">
      <button
        type="button"
        aria-expanded={open}
        aria-label={running ? "处理中" : summary.text}
        title={running ? undefined : summary.text}
        onClick={() => setManual(!open)}
        className="text-muted-foreground hover:bg-chrome-hover hover:text-foreground focus-visible:ring-node-ring/60 -ml-2 inline-flex h-7 max-w-full items-center gap-2.5 self-start rounded-lg px-2 text-[12.5px] whitespace-nowrap tabular-nums outline-none transition-colors duration-120 focus-visible:ring-2"
      >
        <HeadIcon
          aria-hidden
          className={cn(
            "size-3.5 shrink-0 stroke-[1.75]",
            running && "animate-spin motion-reduce:animate-none",
          )}
        />
        {running ? (
          <span>处理中</span>
        ) : (
          <>
            {summary.seconds !== null && <span aria-hidden>{summary.seconds}s</span>}
            <span aria-hidden className="flex min-w-0 items-center gap-2 overflow-hidden">
              {summary.verbs.map(({ verb, icon, count }) => {
                const Icon = VERB_ICON[icon];
                return (
                  <span key={verb} title={`${verb} ${count}`} className="flex items-center gap-0.5">
                    <Icon className="size-3.5 shrink-0 stroke-[1.75]" />
                    {count}
                  </span>
                );
              })}
            </span>
          </>
        )}
        {item.errors > 0 && (
          <span className="text-status-warning shrink-0">· {item.errors} 项失败</span>
        )}
        <ChevronRight
          aria-hidden
          className={cn(
            "size-3.5 shrink-0 opacity-70 transition-transform duration-120",
            open && "rotate-90",
          )}
        />
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
        {toolLabel(tool.name, tool.label)}
      </span>
      {tool.summary && <span className="truncate opacity-85">{tool.summary}</span>}
      {tool.status === "aborted" && <span className="shrink-0 opacity-70">已中止</span>}
    </button>
  );
}

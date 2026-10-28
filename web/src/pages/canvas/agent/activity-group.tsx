import { useState } from "react";
import { AnimatePresence, motion, useReducedMotion } from "motion/react";
import {
  BookOpen,
  Camera,
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
  LocateFixed,
  MessageCircleQuestion,
  PencilLine,
  ScanEye,
  Search,
  Terminal,
  Trash2,
  WandSparkles,
  Wrench,
  type LucideIcon,
} from "lucide-react";

import { toolLabel } from "@/constants/agent";
import { DURATION, EASE_OUT } from "@/lib/motion";
import { cn } from "@/lib/utils";
import { useAgentHighlight } from "@/store/agent-highlight";
import { activitySummary, formatSeconds, toolIcon, type VerbIcon } from "@/utils/agent/activity";
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
  frame: Camera,
  search: Search,
  web: Globe,
  other: Wrench,
};

/**
 * 活动行：同一轮里连续的工具调用合成一行。
 * 收起时是「主图标 + 一句话说明 + 耗时」：说明是模型在这一段结束后写的；还没到（或没有）时先用动作短语顶着，
 * 到了再淡入替换。点开是左侧发丝线的竖向时间线，每条操作以动作图标打头。
 * 进行中自动展开、头部是静态的「处理中」（实时状态在状态行）；结束后自动收起。
 * 用户手动开合过之后就不再自动开合，以用户的意图为准。
 */
export function ActivityGroup({ item }: { item: Activity }) {
  const reduce = useReducedMotion();
  /** 用户手动设的开合；null 表示跟随运行状态 */
  const [manual, setManual] = useState<boolean | null>(null);
  const open = manual ?? item.status === "running";
  const running = item.status === "running";
  const aborted = item.status === "aborted";
  const summary = activitySummary(item);
  const HeadIcon = running ? Loader2 : aborted ? CircleStop : VERB_ICON[summary.icon];
  // 中止的这一段不用模型的说明；其余有说明用说明，没有用动作短语
  const source = !aborted && item.summary ? "model" : "phrase";
  const line = aborted ? `已中止 · ${summary.phrase}` : (item.summary ?? summary.phrase);
  // 挂载时还是兜底短语、后来才换成模型的说明：淡入一下；一上来就有说明的（含历史回放）不播
  const [mountSource] = useState(source);
  const fadeIn = mountSource === "phrase" && source === "model" && !reduce;

  return (
    <div className="flex flex-col">
      <button
        type="button"
        aria-expanded={open}
        aria-label={running ? "处理中" : line}
        title={running ? undefined : line}
        onClick={() => setManual(!open)}
        className="text-muted-foreground hover:bg-chrome-hover hover:text-foreground focus-visible:ring-node-ring/60 -ml-2 inline-flex h-7 max-w-full items-center gap-2 self-start rounded-lg px-2 text-[12.5px] whitespace-nowrap outline-none transition-colors duration-120 focus-visible:ring-2"
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
            <motion.span
              key={source}
              aria-hidden
              initial={fadeIn ? { opacity: 0 } : false}
              animate={{ opacity: 1 }}
              transition={{ duration: DURATION.base, ease: EASE_OUT }}
              className="min-w-0 truncate"
            >
              {line}
            </motion.span>
            {item.errors > 0 && (
              <span className="text-status-warning shrink-0">· {item.errors} 项失败</span>
            )}
            {summary.seconds !== null && (
              <span aria-hidden className="shrink-0 tabular-nums opacity-80">
                {formatSeconds(summary.seconds)}
              </span>
            )}
          </>
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

/**
 * 一次工具调用：动作图标 + 名称 + 摘要。
 * 状态不再占图标位：进行中图标转圈，失败图标变警告色并在右侧写「失败」，中止的图标变淡并写「已中止」，成功的什么都不加。
 * 涉及节点时整行可以点击定位并高亮，行尾悬停出现定位图标。
 */
function ToolRow({ tool }: { tool: ToolCall }) {
  const focus = useFocusNode();
  const canLocate = tool.nodeIds.length > 0;
  const Icon = tool.status === "running" ? Loader2 : VERB_ICON[toolIcon(tool)];
  return (
    <button
      type="button"
      disabled={!canLocate}
      onClick={() => {
        if (tool.nodeIds.some((id) => focus(id))) useAgentHighlight.getState().touch(tool.nodeIds);
      }}
      className={cn(
        "text-muted-foreground group/row -ml-2 flex w-full items-center gap-2 rounded-lg px-2 py-[3px] text-left text-xs outline-none",
        "focus-visible:ring-node-ring/60 focus-visible:ring-2",
        canLocate && "hover:bg-chrome-hover hover:text-foreground cursor-pointer",
      )}
    >
      <Icon
        className={cn(
          "size-3.5 shrink-0 stroke-[1.75]",
          tool.status === "running" && "animate-spin motion-reduce:animate-none",
          tool.status === "error" && "text-status-warning",
          tool.status === "aborted" && "opacity-50",
        )}
      />
      <span className="text-foreground shrink-0 font-medium">
        {toolLabel(tool.name, tool.label)}
      </span>
      {tool.summary && <span className="truncate opacity-85">{tool.summary}</span>}
      {tool.status === "error" && (
        <span className="text-status-warning ml-auto shrink-0">失败</span>
      )}
      {tool.status === "aborted" && <span className="ml-auto shrink-0 opacity-70">已中止</span>}
      {canLocate && tool.status !== "error" && tool.status !== "aborted" && (
        <LocateFixed
          aria-hidden
          className="ml-auto size-3.5 shrink-0 opacity-0 transition-opacity duration-120 group-focus-visible/row:opacity-100 group-hover/row:opacity-100"
        />
      )}
    </button>
  );
}

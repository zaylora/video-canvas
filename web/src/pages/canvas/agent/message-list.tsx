import { useLayoutEffect, useRef, useState, type ReactNode } from "react";
import { motion, useReducedMotion } from "motion/react";
import {
  Check,
  ChevronDown,
  CircleStop,
  Loader2,
  Play,
  RotateCcw,
  TriangleAlert,
  Undo2,
} from "lucide-react";

import { Button } from "@/components/ui/button";
import { RUN_STATUS_TEXT, TOOL_LABELS } from "@/constants/agent";
import { DURATION, EASE_OUT } from "@/lib/motion";
import { cn } from "@/lib/utils";
import { useAgentHighlight } from "@/store/agent-highlight";
import { parseMessage } from "@/utils/agent/chips";
import type { TimelineItem } from "@/utils/agent/timeline";
import type { AgentController } from "@/hooks/use-agent-controller";
import { useFocusNode } from "../chrome/use-focus-node";
import { toast } from "sonner";

import { ApprovalCard } from "./approval-card";
import { Markdown } from "./markdown";

/** 离底部多近算「在底部」：在底部时新内容自动滚动，用户往上翻看时不打扰 */
const STICK_PX = 48;

const CHIP_TONE = {
  node: "bg-foreground/7",
  model: "bg-status-running/20",
  skill: "bg-preset/20",
  asset: "bg-foreground/7",
} as const;

/** 消息里的行内 chip：按种类上底色 */
export function MessageText({ text }: { text: string }) {
  return (
    <>
      {parseMessage(text).map((part, i) =>
        part.kind === "text" ? (
          <span key={i}>{part.text}</span>
        ) : (
          <span
            key={i}
            className={cn(
              "ring-chrome-border mx-0.5 inline-flex h-5 max-w-40 items-center rounded-md px-1.5 align-middle text-xs ring-1",
              CHIP_TONE[part.chip.type],
            )}
            title={`${part.chip.type}:${part.chip.id}`}
          >
            <span className="truncate">{part.chip.name}</span>
          </span>
        ),
      )}
    </>
  );
}

/** 消息流：新增的消息淡入上浮，历史和流式更新不重放；贴着底部时自动跟随 */
export function MessageList({
  ctl,
  showThinking,
}: {
  ctl: AgentController;
  showThinking: boolean;
}) {
  const scroller = useRef<HTMLDivElement>(null);
  const stuck = useRef(true);
  /** 首次渲染时已有的项不播入场 */
  const [initialCount] = useState(ctl.timeline.length);
  const reduce = useReducedMotion();

  useLayoutEffect(() => {
    const el = scroller.current;
    if (el && stuck.current) el.scrollTop = el.scrollHeight;
  });

  return (
    <div
      ref={scroller}
      className="flex min-h-0 flex-1 flex-col gap-3 overflow-y-auto px-4 py-3"
      onScroll={(e) => {
        const el = e.currentTarget;
        stuck.current = el.scrollHeight - el.scrollTop - el.clientHeight < STICK_PX;
      }}
    >
      {ctl.timeline.map((item, index) => (
        <motion.div
          key={item.key}
          initial={index >= initialCount && !reduce ? { opacity: 0, y: 6 } : false}
          animate={{ opacity: 1, y: 0 }}
          transition={{ duration: DURATION.base, ease: EASE_OUT }}
        >
          <Item item={item} ctl={ctl} showThinking={showThinking} />
        </motion.div>
      ))}
    </div>
  );
}

function Item({
  item,
  ctl,
  showThinking,
}: {
  item: TimelineItem;
  ctl: AgentController;
  showThinking: boolean;
}) {
  switch (item.type) {
    case "user":
      return (
        <div className="flex flex-col items-end gap-1">
          {item.steer && <span className="text-muted-foreground text-[11px]">插话</span>}
          <div className="bg-foreground/8 max-w-[88%] rounded-2xl rounded-br-sm px-3 py-2 text-[13.5px] leading-[1.7] break-words whitespace-pre-wrap">
            <MessageText text={item.text} />
          </div>
        </div>
      );
    case "assistant":
      return <Assistant item={item} showThinking={showThinking} />;
    case "tool":
      return <ToolRow item={item} />;
    case "approval":
      return <ApprovalCard approvalId={item.approvalId} ctl={ctl} />;
    case "status":
      return <StatusCard item={item} ctl={ctl} />;
    case "plan-done":
      return (
        <Card tone="success" icon={<Check className="size-4" />}>
          计划已完成（{item.total} 步）
        </Card>
      );
    case "run-footer":
      return <RunFooter runId={item.runId} ctl={ctl} />;
  }
}

function Assistant({
  item,
  showThinking,
}: {
  item: Extract<TimelineItem, { type: "assistant" }>;
  showThinking: boolean;
}) {
  const [open, setOpen] = useState(false);
  const reduce = useReducedMotion();
  return (
    <div className="flex flex-col gap-1.5">
      {showThinking && item.thinking && (
        <div className="text-muted-foreground text-xs">
          <button
            type="button"
            className="hover:text-foreground flex items-center gap-1 outline-none"
            onClick={() => setOpen((v) => !v)}
          >
            <ChevronDown className={cn("size-3 transition-transform", open && "rotate-180")} />
            思考过程
          </button>
          {open && (
            <p className="mt-1 leading-relaxed break-words whitespace-pre-wrap">{item.thinking}</p>
          )}
        </div>
      )}
      {item.text && (
        <div className="text-[13.5px] leading-[1.7] break-words">
          <Markdown text={item.text} />
          {item.streaming && !reduce && (
            <span className="bg-foreground/60 ml-0.5 inline-block h-3.5 w-0.5 translate-y-0.5 animate-pulse" />
          )}
        </div>
      )}
    </div>
  );
}

/** 一行一个工具调用：状态图标 + 名称 + 摘要；涉及节点时点击定位 */
function ToolRow({ item }: { item: Extract<TimelineItem, { type: "tool" }> }) {
  const focus = useFocusNode();
  const canLocate = item.nodeIds.length > 0;
  const Icon =
    item.status === "running"
      ? Loader2
      : item.status === "done"
        ? Check
        : item.status === "error"
          ? TriangleAlert
          : CircleStop;
  return (
    <button
      type="button"
      disabled={!canLocate}
      onClick={() => {
        if (!canLocate) return;
        if (item.nodeIds.some((id) => focus(id))) useAgentHighlight.getState().touch(item.nodeIds);
      }}
      className={cn(
        "text-muted-foreground flex w-full items-center gap-2 rounded-lg px-2 py-1 text-left text-xs outline-none",
        "focus-visible:ring-node-ring/60 focus-visible:ring-2",
        canLocate && "hover:bg-chrome-hover hover:text-foreground cursor-pointer",
      )}
    >
      <Icon
        className={cn(
          "size-3.5 shrink-0",
          item.status === "running" && "animate-spin",
          item.status === "done" && "text-status-success",
          item.status === "error" && "text-status-warning",
        )}
      />
      <span className="shrink-0 font-mono">{TOOL_LABELS[item.name] ?? item.name}</span>
      {item.summary && <span className="truncate opacity-80">{item.summary}</span>}
      {item.status === "aborted" && <span className="opacity-70">已中止</span>}
    </button>
  );
}

/** 消息流里的提示卡 */
export function Card({
  tone,
  icon,
  children,
  actions,
}: {
  tone: "neutral" | "error" | "success" | "info";
  icon?: ReactNode;
  children: ReactNode;
  actions?: ReactNode;
}) {
  return (
    <div
      className={cn(
        "flex flex-col gap-2 rounded-xl border px-3 py-2.5 text-[13px] leading-relaxed",
        tone === "error" && "border-destructive/45 bg-destructive/5",
        tone === "success" && "border-status-success/40 bg-status-success/5",
        tone === "info" && "border-status-running/45 bg-status-running/5",
        tone === "neutral" && "border-chrome-border bg-foreground/4",
      )}
    >
      <div className="flex items-start gap-2">
        {icon && (
          <span className={cn("mt-0.5 shrink-0", tone === "error" && "text-destructive")}>
            {icon}
          </span>
        )}
        <div className="min-w-0 flex-1">{children}</div>
      </div>
      {actions && <div className="flex flex-wrap justify-end gap-1.5">{actions}</div>}
    </div>
  );
}

/** 运行的非正常收尾：失败、停止、暂停；可继续的给「继续」，预算用尽时可以追加预算 */
function StatusCard({
  item,
  ctl,
}: {
  item: Extract<TimelineItem, { type: "status" }>;
  ctl: AgentController;
}) {
  const text = RUN_STATUS_TEXT[item.status];
  const canResume = ["interrupted", "budget_exhausted", "step_limit", "timeout"].includes(
    item.status,
  );
  // 只有这一轮是最新的、画布上没有别的运行时，「继续」才有意义
  const live = ctl.state.runs[item.runId];
  const resumable = canResume && !ctl.busy && live?.status === item.status;
  const failed = item.status === "failed" || item.status === "timeout";
  return (
    <Card
      tone={failed ? "error" : "neutral"}
      icon={failed ? <TriangleAlert className="size-4" /> : <CircleStop className="size-4" />}
      actions={
        resumable && (
          <>
            {item.status === "budget_exhausted" && (
              <Button size="sm" variant="outline" onClick={() => void ctl.resume(item.runId, 20)}>
                追加 20 积分并继续
              </Button>
            )}
            <Button size="sm" variant="outline" onClick={() => void ctl.resume(item.runId)}>
              <Play /> 继续
            </Button>
          </>
        )
      }
    >
      <p className="font-medium">{text?.title ?? "运行结束"}</p>
      <p className="text-muted-foreground">{item.error || text?.hint}</p>
    </Card>
  );
}

/** 一轮运行结束后的操作：撤销本轮 */
function RunFooter({ runId, ctl }: { runId: string; ctl: AgentController }) {
  const done = ctl.undone.has(runId);
  const [pending, setPending] = useState(false);
  return (
    <div className="flex justify-start">
      <Button
        size="sm"
        variant="ghost"
        disabled={done || pending}
        className="text-muted-foreground h-7 gap-1 px-2 text-xs"
        onClick={async () => {
          setPending(true);
          const result = await ctl.undo(runId);
          setPending(false);
          if (!result) return;
          toast.success(
            result.skipped.length
              ? `已撤销 ${result.reverted} 项，${result.skipped.length} 项你之后改过，保持不动`
              : `已撤销 ${result.reverted} 项`,
          );
        }}
      >
        {done ? <RotateCcw className="size-3.5" /> : <Undo2 className="size-3.5" />}
        {done ? "已撤销本轮" : "撤销本轮"}
      </Button>
    </div>
  );
}

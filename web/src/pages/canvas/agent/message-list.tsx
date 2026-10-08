import { useEffect, useLayoutEffect, useRef, useState, type ReactNode } from "react";
import { AnimatePresence, motion, useReducedMotion } from "motion/react";
import {
  ArrowDown,
  Check,
  ChevronRight,
  CircleStop,
  Copy,
  Play,
  TriangleAlert,
} from "lucide-react";
import { toast } from "sonner";

import { ChromeTooltip } from "@/components/canvas/chrome/chrome";
import { Button } from "@/components/ui/button";
import { RUN_STATUS_TEXT } from "@/constants/agent";
import type { AgentController } from "@/hooks/use-agent-controller";
import { DURATION, EASE_OUT } from "@/lib/motion";
import { cn } from "@/lib/utils";
import { parseMessage } from "@/utils/agent/chips";
import type { TimelineItem } from "@/utils/agent/timeline";

import { ActivityGroup } from "./activity-group";
import type { NodeOption } from "./agent-editor";
import { ApprovalRow } from "./approval-row";
import { Markdown } from "./markdown";
import { RunSummary } from "./run-summary";
import { StatusLine } from "./status-line";

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

/**
 * 消息流：新增的消息淡入上浮，历史和流式更新不重放。
 * 贴着底部时自动跟随（内容高度变化也跟，比如卡片展开、图片加载）；用户往上翻看时不打扰，有新内容就出现「回到底部」。
 * 末尾是状态行：运行中唯一的实时指示。
 */
export function MessageList({
  ctl,
  showThinking,
  nodes,
  onScrolled,
}: {
  ctl: AgentController;
  showThinking: boolean;
  /** 画布上现有的节点：改动摘要取名字 */
  nodes: NodeOption[];
  /** 滚离顶部与否：顶栏据此显示分隔线 */
  onScrolled: (scrolled: boolean) => void;
}) {
  const scroller = useRef<HTMLDivElement>(null);
  const content = useRef<HTMLDivElement>(null);
  const stuck = useRef(true);
  const [showJump, setShowJump] = useState(false);
  /** 首次渲染时已有的项不播入场 */
  const [initialCount] = useState(ctl.timeline.length);
  const reduce = useReducedMotion();

  /** 上次看到的内容高度：只有内容真的变高了，才算「有新内容」 */
  const lastHeight = useRef(0);

  /** 内容变了：贴底就跟到底，否则（且确实长高了）提示有新内容 */
  const follow = () => {
    const el = scroller.current;
    if (!el) return;
    if (stuck.current) el.scrollTop = el.scrollHeight;
    else if (el.scrollHeight > lastHeight.current + 1) setShowJump(true);
    lastHeight.current = el.scrollHeight;
  };
  // 每次渲染（新消息、流式文字）后跟随
  useLayoutEffect(follow);
  // 渲染之外的高度变化（展开、图片加载）也跟随
  useEffect(() => {
    const el = content.current;
    if (!el) return;
    const observer = new ResizeObserver(() => follow());
    observer.observe(el);
    return () => observer.disconnect();
  }, []);

  const jumpToBottom = () => {
    const el = scroller.current;
    if (!el) return;
    stuck.current = true;
    setShowJump(false);
    el.scrollTo({ top: el.scrollHeight, behavior: reduce ? "auto" : "smooth" });
  };

  return (
    <div className="relative flex min-h-0 flex-1 flex-col">
      <div
        ref={scroller}
        className="agent-fade-y min-h-0 flex-1 overflow-y-auto"
        onScroll={(e) => {
          const el = e.currentTarget;
          stuck.current = el.scrollHeight - el.scrollTop - el.clientHeight < STICK_PX;
          if (stuck.current) setShowJump(false);
          onScrolled(el.scrollTop > 4);
        }}
      >
        <div ref={content} className="flex flex-col gap-3 px-4 pt-3 pb-2">
          {ctl.timeline.map((item, index) =>
            item.type === "assistant" && item.streaming && !item.text ? null : (
              <motion.div
                key={item.key}
                initial={index >= initialCount && !reduce ? { opacity: 0, y: 6 } : false}
                animate={{ opacity: 1, y: 0 }}
                transition={{ duration: DURATION.base, ease: EASE_OUT }}
              >
                <Item item={item} ctl={ctl} showThinking={showThinking} nodes={nodes} />
              </motion.div>
            ),
          )}
          <AnimatePresence>
            {ctl.statusLine && <StatusLine key={ctl.statusLine.runId} line={ctl.statusLine} />}
          </AnimatePresence>
        </div>
      </div>
      <AnimatePresence>
        {showJump && (
          <motion.button
            type="button"
            aria-label="回到底部"
            initial={reduce ? { opacity: 0 } : { opacity: 0, scale: 0.9 }}
            animate={{ opacity: 1, scale: 1 }}
            exit={
              reduce
                ? { opacity: 0, transition: { duration: DURATION.fast * 0.7 } }
                : { opacity: 0, scale: 0.9, transition: { duration: DURATION.fast * 0.7 } }
            }
            transition={{ duration: DURATION.fast, ease: EASE_OUT }}
            onClick={jumpToBottom}
            className="agent-raised bg-popover text-muted-foreground hover:text-foreground focus-visible:ring-node-ring/60 absolute bottom-2 left-1/2 -ml-3.5 grid size-7 place-items-center rounded-full outline-none focus-visible:ring-2"
          >
            <ArrowDown className="size-3.5" />
          </motion.button>
        )}
      </AnimatePresence>
    </div>
  );
}

function Item({
  item,
  ctl,
  showThinking,
  nodes,
}: {
  item: TimelineItem;
  ctl: AgentController;
  showThinking: boolean;
  nodes: NodeOption[];
}) {
  switch (item.type) {
    case "user":
      return (
        <div className="flex flex-col items-end gap-1">
          {item.steer && <span className="text-muted-foreground text-[11px]">插话</span>}
          <div className="agent-inset bg-foreground/7 max-w-[88%] rounded-2xl rounded-br-sm px-3 py-2 text-[13.5px] leading-[1.7] break-words whitespace-pre-wrap">
            <MessageText text={item.text} />
          </div>
        </div>
      );
    case "assistant":
      return <Assistant item={item} showThinking={showThinking} />;
    case "activity":
      return <ActivityGroup item={item} />;
    case "approval":
      return <ApprovalRow approvalId={item.approvalId} ctl={ctl} />;
    case "status":
      return <StatusCard item={item} ctl={ctl} />;
    case "plan-done":
      return (
        <p className="text-muted-foreground flex items-center gap-2 text-[13px]">
          <Check className="text-status-success size-3.5" />
          计划已完成（{item.total} 步）
        </p>
      );
    case "run-summary":
      return <RunSummary item={item} ctl={ctl} nodes={nodes} />;
  }
}

/**
 * 助手的回复：正文不加气泡。思考过程只在结束后出现（「› 已思考」，进行中由状态行表示）；
 * 悬停或聚焦时下方出现复制。
 */
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
    <div className="group/msg flex flex-col gap-1">
      {showThinking && item.thinking && !item.streaming && (
        <div className="text-muted-foreground text-[13px]">
          <button
            type="button"
            aria-expanded={open}
            className="hover:bg-chrome-hover hover:text-foreground focus-visible:ring-node-ring/60 -ml-2 inline-flex items-center gap-1.5 rounded-lg px-2 py-0.5 outline-none transition-colors duration-120 focus-visible:ring-2"
            onClick={() => setOpen((v) => !v)}
          >
            <ChevronRight
              className={cn("size-3.5 transition-transform duration-120", open && "rotate-90")}
            />
            已思考
          </button>
          {open && (
            <p className="border-foreground/10 mt-1 ml-1.5 border-l pl-3 text-xs leading-relaxed break-words whitespace-pre-wrap">
              {item.thinking}
            </p>
          )}
        </div>
      )}
      {item.text && (
        <div className="text-foreground/92 text-[13.5px] leading-[1.7] break-words">
          <Markdown text={item.text} />
          {item.streaming && !reduce && (
            <span className="bg-foreground/60 ml-0.5 inline-block h-3.5 w-0.5 translate-y-0.5 animate-pulse" />
          )}
        </div>
      )}
      {item.text && !item.streaming && (
        <div className="flex h-6 opacity-0 transition-opacity duration-120 group-focus-within/msg:opacity-100 group-hover/msg:opacity-100">
          <ChromeTooltip label="复制">
            <button
              type="button"
              aria-label="复制"
              onClick={() =>
                void navigator.clipboard
                  .writeText(item.text)
                  .then(() => toast.success("已复制"))
                  .catch(() => toast.error("复制失败"))
              }
              className="text-muted-foreground hover:bg-chrome-hover hover:text-foreground focus-visible:ring-node-ring/60 grid size-6 place-items-center rounded-md outline-none focus-visible:ring-2"
            >
              <Copy className="size-3.5" />
            </button>
          </ChromeTooltip>
        </div>
      )}
    </div>
  );
}

/** 消息流里的提示卡 */
function Card({
  tone,
  icon,
  children,
  actions,
}: {
  tone: "neutral" | "error";
  icon?: ReactNode;
  children: ReactNode;
  actions?: ReactNode;
}) {
  return (
    <div
      className={cn(
        "agent-inset flex flex-col gap-2 rounded-xl px-3 py-2.5 text-[13px] leading-relaxed",
        tone === "error" &&
          "bg-destructive/5 shadow-[0_0_0_1px_color-mix(in_oklab,var(--destructive)_45%,transparent)]",
        tone === "neutral" && "from-foreground/4 to-foreground/2 bg-linear-to-b",
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

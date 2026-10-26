import { useEffect, useLayoutEffect, useRef, useState, type ReactNode } from "react";
import { AnimatePresence, motion, useReducedMotion } from "motion/react";
import {
  ArrowDown,
  Check,
  ChevronRight,
  CircleStop,
  Copy,
  Layers,
  Play,
  TriangleAlert,
} from "lucide-react";
import { toast } from "sonner";

import { ChromeTooltip } from "@/components/canvas/chrome/chrome";
import { Button } from "@/components/ui/button";
import { runStatusText } from "@/constants/agent";
import type { AgentController } from "@/hooks/use-agent-controller";
import { DURATION, EASE_OUT, SPRING } from "@/lib/motion";
import { cn } from "@/lib/utils";
import { parseMessage } from "@/utils/agent/chips";
import { contextUsage, formatTokens } from "@/utils/agent/context-usage";
import { anchoredScrollTop, trackHistoryKeys } from "@/utils/agent/message-scroll";
import type { TimelineItem } from "@/utils/agent/timeline";

/** 还在排队、没被读到的插话 */
const isQueued = (item: TimelineItem) => item.type === "user" && item.queued === "waiting";

import { ActivityGroup } from "./activity-group";
import type { RefNode } from "@/utils/agent/selection-refs";

import type { NodeOption } from "./agent-editor";
import { SelectionRefs } from "./selection-refs";
import { ApprovalRow } from "./approval-row";
import { Markdown } from "./markdown";
import { RunFeedback } from "./run-feedback";
import { RunSummary } from "./run-summary";
import { ShimmerText } from "./shimmer-text";
import { StatusLine } from "./status-line";

/** 离底部多近算「在底部」：在底部时新内容自动滚动，用户往上翻看时不打扰 */
const STICK_PX = 48;
/** 离顶部不到这么远就去加载更早的一页：趁用户还没滚到头，翻上去时内容已经在了 */
const LOAD_MORE_PX = 160;

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
 * 消息流：实时新增的消息淡入上浮，历史（首次加载、往上翻补来的）和流式更新不播动画。
 * 挂载时历史已经加载好，首次布局前就停在底部；往上翻到快到顶时加载更早一页，补完后视线停在原来看的那条上。
 * 贴着底部时自动跟随（内容高度变化也跟，比如卡片展开、图片加载）；用户往上翻看时不打扰，有新内容就出现「回到底部」。
 * 末尾是状态行：运行中唯一的实时指示；还没被 Agent 读到的插话排在状态行下面（虚线气泡），读到时飞进时间线。
 */
export function MessageList({
  ctl,
  showThinking,
  nodes,
  refNodes,
  onScrolled,
}: {
  ctl: AgentController;
  showThinking: boolean;
  /** 画布上现有的节点：改动摘要取名字 */
  nodes: NodeOption[];
  /** 消息里「引用了哪些节点」解析标题用：含组（输入框里 @ 的候选不含组） */
  refNodes: RefNode[];
  /** 滚离顶部与否：顶栏据此显示分隔线 */
  onScrolled: (scrolled: boolean) => void;
}) {
  const scroller = useRef<HTMLDivElement>(null);
  const content = useRef<HTMLDivElement>(null);
  const stuck = useRef(true);
  const [showJump, setShowJump] = useState(false);
  /** 历史项的 key：不播入场；首次渲染时已有的、往前补来的都算。渲染期派生：补来的页和新增的消息在同一次渲染里分清 */
  const keys = ctl.timeline.map((item) => item.key);
  const [history, setHistory] = useState(() => trackHistoryKeys(null, keys));
  const historyKeys = trackHistoryKeys(history, keys);
  if (historyKeys.size !== history.size) setHistory(historyKeys);
  const reduce = useReducedMotion();
  /** 排队中的插话在输入框顶部的托盘里（steer-tray），消息流只画其余的 */
  const body = ctl.timeline.filter((item) => !isQueued(item));

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
  // 前面补了更早的一页：内容在上面变高了，滚动位置跟着补偿，视线才不跳。要排在 follow 前面，免得把补出来的高度当成新内容
  const firstKey = ctl.timeline[0]?.key;
  const prevFirstKey = useRef<string | undefined>(undefined);
  useLayoutEffect(() => {
    const el = scroller.current;
    if (
      el &&
      prevFirstKey.current !== undefined &&
      firstKey !== prevFirstKey.current &&
      !stuck.current
    ) {
      el.scrollTop = anchoredScrollTop(el.scrollTop, lastHeight.current, el.scrollHeight);
      lastHeight.current = el.scrollHeight;
    }
    prevFirstKey.current = firstKey;
  });
  // 每次渲染（新消息、流式文字）后跟随
  useLayoutEffect(follow);

  // 快到顶了就取更早的一页；内容还不够一屏（滚不动）时也要取，直到铺满或没有更早的
  const canLoadOlder = ctl.hasMoreOlder && !ctl.loadingOlder && !ctl.olderFailed;
  useEffect(() => {
    const el = scroller.current;
    if (el && canLoadOlder && el.scrollTop < LOAD_MORE_PX) ctl.loadOlder();
  });
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
        className="agent-fade-y min-h-0 flex-1 [overflow-anchor:none] overflow-y-auto"
        onScroll={(e) => {
          const el = e.currentTarget;
          stuck.current = el.scrollHeight - el.scrollTop - el.clientHeight < STICK_PX;
          if (stuck.current) setShowJump(false);
          onScrolled(el.scrollTop > 4);
          if (canLoadOlder && el.scrollTop < LOAD_MORE_PX) ctl.loadOlder();
        }}
      >
        <div ref={content} className="flex flex-col gap-3 px-4 pt-3 pb-2">
          {body.map((item) =>
            item.type === "assistant" && item.streaming && !item.text ? null : (
              <motion.div
                key={item.key}
                // 插话是从下面的队列飞进时间线的（共享 layoutId），不再叠一层淡入
                initial={
                  !historyKeys.has(item.key) && !reduce && !(item.type === "user" && item.steerId)
                    ? { opacity: 0, y: 6 }
                    : false
                }
                animate={{ opacity: 1, y: 0 }}
                transition={{ duration: DURATION.base, ease: EASE_OUT }}
              >
                <Item
                  item={item}
                  ctl={ctl}
                  showThinking={showThinking}
                  nodes={nodes}
                  refNodes={refNodes}
                />
              </motion.div>
            ),
          )}
          <AnimatePresence>
            {ctl.statusLine && <StatusLine key={ctl.statusLine.runId} line={ctl.statusLine} />}
          </AnimatePresence>
        </div>
      </div>
      {/* 顶部的加载提示悬浮在列表上，不占文档流：出现和消失都不会让内容位移 */}
      <div className="pointer-events-none absolute inset-x-0 top-2 flex justify-center">
        <AnimatePresence>
          {(ctl.loadingOlder || ctl.olderFailed) && (
            <motion.div
              key={ctl.olderFailed ? "failed" : "loading"}
              initial={reduce ? { opacity: 0 } : { opacity: 0, y: -4 }}
              animate={{ opacity: 1, y: 0 }}
              exit={{ opacity: 0, transition: { duration: DURATION.exit } }}
              transition={{ duration: DURATION.base, ease: EASE_OUT }}
              className="agent-raised bg-popover text-muted-foreground pointer-events-auto flex h-7 items-center rounded-full px-3 text-[12px]"
            >
              {ctl.olderFailed ? (
                <button
                  type="button"
                  onClick={ctl.reload}
                  className="hover:text-foreground focus-visible:ring-node-ring/60 rounded-full outline-none focus-visible:ring-2"
                >
                  加载失败，点击重试
                </button>
              ) : (
                <ShimmerText>加载更早的消息…</ShimmerText>
              )}
            </motion.div>
          )}
        </AnimatePresence>
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
  refNodes,
}: {
  item: TimelineItem;
  ctl: AgentController;
  showThinking: boolean;
  nodes: NodeOption[];
  refNodes: RefNode[];
}) {
  switch (item.type) {
    case "user":
      if (item.steerId) return <Steer item={item} ctl={ctl} />;
      return (
        <div className="flex flex-col items-end gap-1">
          {item.steer && <span className="text-muted-foreground text-[11px]">插话</span>}
          <SelectionRefs ids={item.selection} nodes={refNodes} />
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
    case "compaction":
      return <CompactionDivider item={item} window={ctl.context.window} />;
    case "plan-done":
      return (
        <p className="text-muted-foreground flex items-center gap-2 text-[13px]">
          <Check className="text-status-success size-3.5" />
          计划已完成（{item.total} 步）
        </p>
      );
    case "run-summary":
      return <RunSummary item={item} ctl={ctl} nodes={nodes} />;
    case "run-feedback":
      return <RunFeedback runId={item.runId} ctl={ctl} />;
  }
}

/**
 * 自动压缩的分隔行：一条淡淡的线加一句话，不抢正文。成功时写「整理前 → 整理后」的占用（整理后要等下一次模型调用的用量到了才有，
 * 之前写「…」），失败时琥珀色，说清楚这一轮不再自动整理。悬停或聚焦弹出说明：聊天记录没有变，变的是 Agent 读到的上下文。
 */
function CompactionDivider({
  item,
  window,
}: {
  item: Extract<TimelineItem, { type: "compaction" }>;
  window: number;
}) {
  /** 窗口大小已知用百分比，否则用 Token 数 */
  const fmt = (tokens: number) =>
    window > 0 ? `${contextUsage(tokens, window).percent}%` : formatTokens(tokens);
  const afterText = item.after === null ? "…" : fmt(item.after);
  return (
    <ChromeTooltip
      label={
        <span className="block max-w-65 text-[12px] leading-relaxed">
          {item.failed
            ? "这次没能把较早的对话整理成摘要（比如模型暂时不可用）。这一轮不会再自动整理，对话继续变长可能很快到上限；可以新开一个对话接着做。"
            : "聊天记录没有变：上面的消息都还在。只是 Agent 读到的上下文里，较早的部分换成了一份摘要（要点、已完成的事、节点和任务信息都保留）。"}
        </span>
      }
    >
      <div
        role="separator"
        tabIndex={0}
        aria-label={item.failed ? "较早的对话没能整理" : "已整理较早的对话"}
        className={cn(
          "text-muted-foreground focus-visible:ring-node-ring/60 flex items-center gap-2.5 rounded-md text-[11.5px] outline-none focus-visible:ring-2",
          item.failed && "text-status-warning",
        )}
      >
        <span
          className={cn("h-px flex-1", item.failed ? "bg-status-warning/45" : "bg-foreground/12")}
        />
        <span className="inline-flex items-center gap-1.5 whitespace-nowrap">
          {item.failed ? <TriangleAlert className="size-3.25" /> : <Layers className="size-3.25" />}
          {item.failed ? (
            "较早的对话没能整理 · 这一轮不再自动整理，建议新开对话"
          ) : (
            <>
              已整理较早的对话 · 上下文 <span className="tabular-nums">{fmt(item.before)}</span> →{" "}
              <motion.span
                key={afterText}
                initial={item.after === null ? false : { opacity: 0 }}
                animate={{ opacity: 1 }}
                transition={{ duration: DURATION.fast }}
                className="tabular-nums"
              >
                {afterText}
              </motion.span>
            </>
          )}
        </span>
        <span
          className={cn("h-px flex-1", item.failed ? "bg-status-warning/45" : "bg-foreground/12")}
        />
      </div>
    </ChromeTooltip>
  );
}

/**
 * 读到之后的插话就是一个普通的实线气泡，不再单独标「插话」；
 * 运行结束也没读到的是琥珀色虚线，用户还没发新消息时给「作为新消息发送」。排队中的在输入框顶部（steer-tray）。
 */
function Steer({
  item,
  ctl,
}: {
  item: Extract<TimelineItem, { type: "user" }>;
  ctl: AgentController;
}) {
  const reduce = useReducedMotion();
  const missed = item.queued === "missed";
  return (
    <div className="flex flex-col items-end gap-1">
      {missed && (
        <span className="text-status-warning flex min-h-4 items-center gap-1 text-[11px]">
          <TriangleAlert className="size-3" />
          运行已结束，这条没有被读取
          {item.resendable && !ctl.busy && (
            <>
              {" · "}
              <button
                type="button"
                onClick={() => void ctl.send(item.text)}
                className="text-foreground decoration-foreground/35 hover:decoration-foreground focus-visible:ring-node-ring/60 rounded-sm underline underline-offset-3 outline-none focus-visible:ring-2"
              >
                作为新消息发送
              </button>
            </>
          )}
        </span>
      )}
      <motion.div
        layoutId={reduce ? undefined : `steer-${item.key}`}
        transition={SPRING}
        className={cn(
          "max-w-[88%] rounded-2xl rounded-br-sm px-3 py-2 text-[13.5px] leading-[1.7] break-words whitespace-pre-wrap",
          missed &&
            "border-status-warning/70 bg-status-warning/8 border border-dashed px-[11px] py-[7px]",
          !missed && "agent-inset bg-foreground/7",
        )}
      >
        <MessageText text={item.text} />
      </motion.div>
    </div>
  );
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

/** 运行的非正常收尾：失败、停止、暂停；可继续的给「继续」，早先设过额度又用完的旧运行可以追加额度 */
function StatusCard({
  item,
  ctl,
}: {
  item: Extract<TimelineItem, { type: "status" }>;
  ctl: AgentController;
}) {
  const text = runStatusText(item.status, item.title, item.hint);
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

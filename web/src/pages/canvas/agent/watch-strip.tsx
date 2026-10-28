import { useState } from "react";
import { ChevronDown, ChevronUp } from "lucide-react";

import { cn } from "@/lib/utils";
import { useAgentHighlight } from "@/store/agent-highlight";
import type { WatchSummary } from "@/utils/agent/watch";

import { useFocusNode } from "@/components/canvas-editor/use-focus-node";
import { ProgressRing } from "./progress-ring";

/** 任务状态文字的颜色：生成中蓝、成功绿、失败红 */
const STATUS_TONE = {
  生成中: "text-status-running",
  成功: "text-status-success",
  失败: "text-destructive",
} as const;

/**
 * 监听条的呈现（纯展示，状态由上层给）：进度环 + 一句话 + 「不用等了」，点开向下列出每个任务。
 * 等待中进度环是蓝色，已全部结束在排队时是绿色；有失败时在一句话后追加红字「· N 项失败」。
 * 「不用等了」只取消 Agent 的跟进，生成照常完成，所以它不是危险操作，不需要二次确认。
 */
export function WatchStripView({
  summary,
  open,
  onToggle,
  onCancel,
  onLocate,
}: {
  summary: WatchSummary;
  open: boolean;
  onToggle: () => void;
  onCancel: () => void;
  /** 点任务名：定位到画布上的节点 */
  onLocate: (nodeId: string) => void;
}) {
  const Chevron = open ? ChevronUp : ChevronDown;
  return (
    <div className="agent-inset bg-foreground/4 mx-3 mb-1.5 shrink-0 rounded-xl text-[12.5px]">
      <div
        role="button"
        tabIndex={0}
        aria-expanded={open}
        onClick={onToggle}
        onKeyDown={(e) => {
          if (e.key === "Enter" || e.key === " ") {
            e.preventDefault();
            onToggle();
          }
        }}
        className="focus-visible:ring-node-ring/60 flex h-9 cursor-pointer items-center gap-2 rounded-xl pr-1.5 pl-3 outline-none focus-visible:ring-2"
      >
        <ProgressRing
          value={summary.settled}
          total={summary.total}
          className={summary.queued ? "stroke-status-success" : "stroke-status-running"}
        />
        <span className="min-w-0 truncate">
          {summary.text}
          {summary.failed > 0 && (
            <span className="text-destructive"> · {summary.failed} 项失败</span>
          )}
        </span>
        <span className="flex-1" />
        {!summary.queued && (
          <button
            type="button"
            onClick={(e) => {
              e.stopPropagation();
              onCancel();
            }}
            className="text-muted-foreground hover:bg-chrome-hover hover:text-foreground focus-visible:ring-node-ring/60 h-6 shrink-0 rounded-md px-2 text-xs outline-none focus-visible:ring-2"
          >
            不用等了
          </button>
        )}
        <Chevron className="text-muted-foreground size-3.5 shrink-0" aria-hidden />
      </div>
      {open && (
        <ul className="flex max-h-40 flex-col gap-0.5 overflow-y-auto px-3 pt-0.5 pb-2">
          {summary.tasks.map((t) => (
            <li key={t.taskId} className="flex items-center gap-2 text-xs">
              <button
                type="button"
                onClick={() => onLocate(t.nodeId)}
                className="hover:bg-chrome-hover min-w-0 flex-1 truncate rounded-md px-1 py-0.5 text-left outline-none"
              >
                {t.label}
              </button>
              <span className={cn("shrink-0 font-mono", STATUS_TONE[t.statusText])}>
                {t.statusText}
              </span>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}

/**
 * 监听条（Agent 面板顶部、头部下方）：会话里有后台等待的生成时才出现。
 * 展开状态是本地的；点任务名定位到节点并短暂高亮。
 */
export function WatchStrip({ summary, onCancel }: { summary: WatchSummary; onCancel: () => void }) {
  const [open, setOpen] = useState(false);
  const focus = useFocusNode();
  return (
    <WatchStripView
      summary={summary}
      open={open}
      onToggle={() => setOpen((v) => !v)}
      onCancel={onCancel}
      onLocate={(id) => {
        if (focus(id)) useAgentHighlight.getState().touch([id]);
      }}
    />
  );
}

import { useState } from "react";
import NumberFlow from "@number-flow/react";
import { AnimatePresence, motion, useReducedMotion } from "motion/react";
import { ChevronRight, Loader2, LocateFixed, Undo2 } from "lucide-react";
import { toast } from "sonner";

import { ChromeTooltip } from "@/components/canvas/chrome/chrome";
import type { AgentController } from "@/hooks/use-agent-controller";
import { DURATION, EASE_OUT } from "@/lib/motion";
import { cn } from "@/lib/utils";
import { useAgentHighlight } from "@/store/agent-highlight";
import type { TimelineItem } from "@/utils/agent/timeline";

import { useFitNodes, useFocusNode } from "../chrome/use-focus-node";
import type { NodeOption } from "./agent-editor";

type Summary = Extract<TimelineItem, { type: "run-summary" }>;

const KIND_TEXT: Record<string, string> = {
  script: "文本",
  image: "图片",
  video: "视频",
  audio: "音频",
};

/** 计数徽标：+N 新建、~N 修改、−N 删除，为 0 不显示 */
const BADGE = {
  add: "text-status-success bg-status-success/14",
  mod: "text-status-running bg-status-running/14",
  del: "text-destructive bg-destructive/14",
} as const;

function Badge({ tone, sign, value }: { tone: keyof typeof BADGE; sign: string; value: number }) {
  if (value === 0) return null;
  return (
    <span className={cn("rounded-md px-1.5 text-xs font-medium tabular-nums", BADGE[tone])}>
      {sign}
      <NumberFlow value={value} />
    </span>
  );
}

/**
 * 一轮运行结束后的改动摘要卡：新建、修改、删除了几个节点；展开逐个列出，点一行定位并高亮。
 * 「定位」把这一轮改过的节点全部框进视口；「撤销本轮」按改动日志倒回去，撤销后卡片变灰并说明跳过的项。
 */
export function RunSummary({
  item,
  ctl,
  nodes,
}: {
  item: Summary;
  ctl: AgentController;
  /** 画布上现有的节点：取名字和类型 */
  nodes: NodeOption[];
}) {
  const reduce = useReducedMotion();
  const focus = useFocusNode();
  const fit = useFitNodes();
  const [open, setOpen] = useState(false);
  const [pending, setPending] = useState(false);
  const [skipped, setSkipped] = useState(0);
  const undone = ctl.undone.has(item.runId);
  const byId = new Map(nodes.map((n) => [n.id, n]));
  const touched = [...item.created, ...item.updated];

  const kindOf = (id: string) => {
    const kind = byId.get(id)?.kind;
    return kind ? (KIND_TEXT[kind] ?? kind) : "";
  };
  const rows = [
    ...item.created.map((id) => ({ id, sign: "+", tone: "add" as const, what: kindOf(id) })),
    ...item.updated.map((id) => ({ id, sign: "~", tone: "mod" as const, what: "已修改" })),
    ...item.deleted.map((d) => ({ id: d.id, sign: "−", tone: "del" as const, what: "已删除" })),
  ];
  const labelOf = (id: string) =>
    byId.get(id)?.label ?? item.deleted.find((d) => d.id === id)?.label ?? "已删除的节点";

  const undo = async () => {
    setPending(true);
    const result = await ctl.undo(item.runId);
    setPending(false);
    if (!result) return;
    setOpen(false);
    setSkipped(result.skipped.length);
    toast.success(
      result.skipped.length
        ? `已撤销 ${result.reverted} 项，${result.skipped.length} 项你之后改过，保持不动`
        : `已撤销 ${result.reverted} 项`,
    );
  };

  return (
    <div className="agent-inset from-foreground/4 to-foreground/2 overflow-hidden rounded-xl bg-linear-to-b text-[13px]">
      <div className="flex items-center gap-1 py-1.5 pr-1.5 pl-3">
        <button
          type="button"
          aria-expanded={open}
          disabled={rows.length === 0}
          onClick={() => setOpen((v) => !v)}
          className="focus-visible:ring-node-ring/60 flex min-w-0 flex-1 items-center gap-2 rounded-md text-left font-medium outline-none focus-visible:ring-2"
        >
          {rows.length > 0 && (
            <ChevronRight
              className={cn(
                "text-muted-foreground size-3.5 shrink-0 transition-transform duration-120",
                open && "rotate-90",
              )}
            />
          )}
          <span className="truncate">{undone ? "已撤销本轮" : "本轮改动"}</span>
          <span className={cn("flex items-center gap-1", undone && "opacity-50 grayscale")}>
            <Badge tone="add" sign="+" value={item.created.length} />
            <Badge tone="mod" sign="~" value={item.updated.length} />
            <Badge tone="del" sign="−" value={item.deleted.length} />
          </span>
        </button>
        {!undone && (
          <>
            {touched.length > 0 && (
              <ChromeTooltip label="把改动过的节点框进视口">
                <button
                  type="button"
                  onClick={() => {
                    if (fit(touched)) useAgentHighlight.getState().touch(touched);
                  }}
                  className="text-muted-foreground hover:bg-chrome-hover hover:text-foreground focus-visible:ring-node-ring/60 flex h-7 items-center gap-1 rounded-lg px-2 text-xs outline-none transition-colors duration-120 focus-visible:ring-2"
                >
                  <LocateFixed className="size-3.5" />
                  定位
                </button>
              </ChromeTooltip>
            )}
            <button
              type="button"
              disabled={pending}
              onClick={() => void undo()}
              className="text-muted-foreground hover:bg-chrome-hover hover:text-foreground focus-visible:ring-node-ring/60 flex h-7 items-center gap-1 rounded-lg px-2 text-xs outline-none transition-colors duration-120 focus-visible:ring-2 disabled:opacity-60"
            >
              {pending ? (
                <Loader2 className="size-3.5 animate-spin motion-reduce:animate-none" />
              ) : (
                <Undo2 className="size-3.5" />
              )}
              {pending ? "撤销中" : "撤销本轮"}
            </button>
          </>
        )}
      </div>
      {undone && skipped > 0 && (
        <p className="text-muted-foreground px-3 pb-2 text-xs">{skipped} 项你之后改过，保持不动</p>
      )}
      <AnimatePresence initial={false}>
        {open && !undone && (
          <motion.ul
            initial={reduce ? { opacity: 0 } : { opacity: 0, y: -4 }}
            animate={{ opacity: 1, y: 0 }}
            exit={{ opacity: 0, transition: { duration: DURATION.fast * 0.7 } }}
            transition={{ duration: DURATION.fast, ease: EASE_OUT }}
            className="border-chrome-border flex max-h-48 flex-col overflow-y-auto border-t p-1"
          >
            {rows.map((row) => (
              <li key={`${row.tone}${row.id}`}>
                <button
                  type="button"
                  disabled={row.tone === "del"}
                  onClick={() => {
                    if (focus(row.id)) useAgentHighlight.getState().touch([row.id]);
                  }}
                  className="hover:bg-chrome-hover focus-visible:ring-node-ring/60 flex w-full items-center gap-2 rounded-lg px-2 py-1 text-left text-xs outline-none focus-visible:ring-2 disabled:hover:bg-transparent"
                >
                  <span className={cn("w-3 shrink-0 font-semibold", BADGE[row.tone].split(" ")[0])}>
                    {row.sign}
                  </span>
                  <span className="min-w-0 flex-1 truncate">{labelOf(row.id)}</span>
                  <span className="text-muted-foreground shrink-0">{row.what}</span>
                </button>
              </li>
            ))}
          </motion.ul>
        )}
      </AnimatePresence>
    </div>
  );
}

import { useState } from "react";
import NumberFlow from "@number-flow/react";
import { AnimatePresence, motion, useReducedMotion } from "motion/react";
import { ChevronDown, GitCommitHorizontal, Loader2, LocateFixed, Undo2 } from "lucide-react";
import { toast } from "sonner";

import { ChromeTooltip } from "@/components/canvas/chrome/chrome";
import type { AgentController } from "@/hooks/use-agent-controller";
import { DURATION, EASE_OUT, TAP } from "@/lib/motion";
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

/** 计数的颜色：+N 新建、~N 修改、−N 删除 */
const TONE = {
  add: "text-status-success",
  mod: "text-status-running",
  del: "text-destructive",
} as const;

function Count({ tone, sign, value }: { tone: keyof typeof TONE; sign: string; value: number }) {
  if (value === 0) return null;
  return (
    <span className={cn("font-semibold tabular-nums", TONE[tone])}>
      {sign}
      <NumberFlow value={value} />
    </span>
  );
}

/**
 * 一轮改动的状态与动作：胶囊和明细共用。
 * 「定位」把这一轮改过的节点全部框进视口；「撤销本轮」按改动日志倒回去，撤销后胶囊划线并说明跳过的项。
 */
export function useRunChanges(item: Summary, ctl: AgentController, nodes: NodeOption[]) {
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
  const locateAll = () => {
    if (fit(touched)) useAgentHighlight.getState().touch(touched);
  };
  const locate = (id: string) => {
    if (focus(id)) useAgentHighlight.getState().touch([id]);
  };

  return {
    item,
    open,
    setOpen,
    pending,
    skipped,
    undone,
    rows,
    touched,
    labelOf,
    undo,
    locate,
    locateAll,
  };
}

type Changes = ReturnType<typeof useRunChanges>;

/**
 * 改动胶囊（设计稿 6.12）：回答底栏左边「⊸ 画布改动 +1 ~1 ⌄」，点开在底栏下方展开明细；
 * 撤销后变成划线的「已撤销本轮」。
 */
export function ChangesPill({ changes }: { changes: Changes }) {
  const { item, open, setOpen, undone, rows } = changes;
  return (
    <motion.button
      type="button"
      aria-expanded={open}
      disabled={rows.length === 0}
      whileTap={TAP}
      onClick={() => setOpen((v) => !v)}
      className={cn(
        "text-muted-foreground hover:bg-chrome-hover hover:text-foreground ring-foreground/10 focus-visible:ring-node-ring/60 aria-expanded:bg-chrome-hover aria-expanded:text-foreground flex h-7 min-w-0 items-center gap-1.5 rounded-full pr-2 pl-2.5 text-xs whitespace-nowrap ring-1 outline-none transition-colors duration-120 focus-visible:ring-2",
        undone && "line-through opacity-70",
      )}
    >
      {undone ? (
        <Undo2 className="size-3.5 shrink-0" />
      ) : (
        <GitCommitHorizontal className="size-3.5 shrink-0" />
      )}
      <span className="truncate">{undone ? "已撤销本轮" : "画布改动"}</span>
      {!undone && (
        <>
          <Count tone="add" sign="+" value={item.created.length} />
          <Count tone="mod" sign="~" value={item.updated.length} />
          <Count tone="del" sign="−" value={item.deleted.length} />
        </>
      )}
      {rows.length > 0 && (
        <ChevronDown
          className={cn(
            "size-3.5 shrink-0 opacity-70 transition-transform duration-120",
            open && "rotate-180",
          )}
        />
      )}
    </motion.button>
  );
}

/** 改动明细：逐行定位，底部「全部定位」和「撤销本轮」；从胶囊下方长出来 */
export function ChangesPanel({ changes }: { changes: Changes }) {
  const reduce = useReducedMotion();
  const { open, undone, skipped, rows, touched, labelOf, pending, undo, locate, locateAll } =
    changes;
  return (
    <>
      {undone && skipped > 0 && (
        <p className="text-muted-foreground text-xs">{skipped} 项你之后改过，保持不动</p>
      )}
      <AnimatePresence initial={false}>
        {open && (
          <motion.div
            initial={reduce ? { opacity: 0 } : { opacity: 0, y: -4, scale: 0.98 }}
            animate={{ opacity: 1, y: 0, scale: 1 }}
            exit={{ opacity: 0, transition: { duration: DURATION.exit } }}
            transition={{ duration: DURATION.base, ease: EASE_OUT }}
            className="agent-inset bg-foreground/3 origin-top-left overflow-hidden rounded-xl text-[13px]"
          >
            <ul className="flex max-h-48 flex-col overflow-y-auto p-1">
              {rows.map((row) => (
                <li key={`${row.tone}${row.id}`}>
                  <button
                    type="button"
                    disabled={row.tone === "del" || undone}
                    onClick={() => locate(row.id)}
                    className="group/row hover:bg-chrome-hover focus-visible:ring-node-ring/60 flex h-8 w-full items-center gap-2 rounded-lg px-2 text-left outline-none focus-visible:ring-2 disabled:opacity-60 disabled:hover:bg-transparent"
                  >
                    <span className={cn("w-3 shrink-0 text-center font-semibold", TONE[row.tone])}>
                      {row.sign}
                    </span>
                    <span className="min-w-0 flex-1 truncate">{labelOf(row.id)}</span>
                    <span className="text-muted-foreground shrink-0 text-xs">{row.what}</span>
                    <LocateFixed className="text-muted-foreground size-3 shrink-0 opacity-0 transition-opacity duration-120 group-hover/row:opacity-100 group-disabled/row:hidden" />
                  </button>
                </li>
              ))}
            </ul>
            {!undone && (
              <div className="border-foreground/8 flex items-center gap-1 border-t p-1">
                {touched.length > 0 && (
                  <ChromeTooltip label="把改动过的节点框进视口">
                    <button type="button" onClick={locateAll} className={FOOT_BTN}>
                      <LocateFixed className="size-3.5" />
                      全部定位
                    </button>
                  </ChromeTooltip>
                )}
                <button
                  type="button"
                  disabled={pending}
                  onClick={() => void undo()}
                  className={cn(FOOT_BTN, "hover:text-destructive ml-auto")}
                >
                  {pending ? (
                    <Loader2 className="size-3.5 animate-spin motion-reduce:animate-none" />
                  ) : (
                    <Undo2 className="size-3.5" />
                  )}
                  {pending ? "撤销中" : "撤销本轮"}
                </button>
              </div>
            )}
          </motion.div>
        )}
      </AnimatePresence>
    </>
  );
}

const FOOT_BTN =
  "text-muted-foreground hover:bg-chrome-hover hover:text-foreground focus-visible:ring-node-ring/60 flex h-7 items-center gap-1.5 rounded-lg px-2 text-xs outline-none transition-colors duration-120 focus-visible:ring-2 disabled:opacity-60";

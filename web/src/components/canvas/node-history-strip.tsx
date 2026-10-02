import { useState } from "react";
import { AnimatePresence, LayoutGroup, motion } from "motion/react";
import { AudioLines, History } from "lucide-react";

import { DURATION, EASE_OUT, SPRING, TAP } from "@/lib/motion";
import { cn } from "@/lib/utils";
import type { NodeOutput } from "@/types";

/** 拖出缩略图时塞进 dataTransfer 的类型，画布 onDrop 认它来建节点 */
export const NODE_OUTPUT_MIME = "application/x-video-canvas-output";

/** 一行最多露几张缩略图，多了横向滚动 */
const VISIBLE_THUMBS = 8;

function Thumb({ output }: { output: NodeOutput }) {
  if (output.mediaType === "image")
    return (
      <img
        src={output.src}
        alt=""
        draggable={false}
        decoding="async"
        className="size-full object-cover"
      />
    );
  if (output.mediaType === "video")
    return (
      <video
        src={output.src}
        muted
        preload="metadata"
        className="pointer-events-none size-full object-cover"
      />
    );
  return (
    <span className="text-muted-foreground grid size-full place-items-center">
      <AudioLines className="size-4" />
    </span>
  );
}

/**
 * 节点上方的「节点生成历史」浮条（设计稿 6.3）：
 * 收起时是一颗胶囊，带版本数和状态圈；展开后横排缩略图，当前版本的白框在缩略图间弹簧滑动。
 * 点缩略图切版本；把缩略图拖到画布上会建一个以它为素材的新节点。
 */
export function NodeHistoryStrip({
  nodeId,
  outputs,
  activeId,
  running,
  onSelect,
  className,
}: {
  nodeId: string;
  outputs: NodeOutput[];
  activeId?: string;
  running: boolean;
  onSelect: (outputId: string) => void;
  className?: string;
}) {
  const [open, setOpen] = useState(false);
  const count = outputs.length;
  const expanded = open && count > 0;

  return (
    <motion.div
      layout
      transition={SPRING}
      initial={{ opacity: 0, scale: 0.96 }}
      animate={{ opacity: 1, scale: 1 }}
      style={{ borderRadius: 999 }}
      className={cn(
        "nodrag nopan bg-chrome ring-chrome-border flex h-10 items-center px-1 shadow-lg ring-1 backdrop-blur-xl",
        className,
      )}
    >
      <motion.button
        layout="position"
        type="button"
        whileTap={TAP}
        aria-expanded={expanded}
        disabled={count === 0}
        title={count === 0 ? "生成后，每一版结果都会留在这里" : undefined}
        onClick={() => setOpen((value) => !value)}
        className="text-muted-foreground hover:bg-chrome-hover hover:text-foreground flex h-8 items-center gap-1.5 rounded-full px-2.5 text-sm transition-colors disabled:hover:bg-transparent"
      >
        <History className="size-4" />
        节点生成历史
        {count > 1 && (
          <span className="bg-foreground text-background rounded-full px-1.5 font-mono text-[11px] leading-4 tabular-nums">
            {count}
          </span>
        )}
      </motion.button>

      <AnimatePresence initial={false}>
        {expanded && (
          <motion.div
            key="thumbs"
            initial={{ opacity: 0, width: 0 }}
            animate={{ opacity: 1, width: "auto" }}
            exit={{ opacity: 0, width: 0, transition: { duration: DURATION.base * 0.7 } }}
            transition={{ duration: DURATION.slow, ease: EASE_OUT }}
            className="overflow-hidden"
          >
            <LayoutGroup id={`history-${nodeId}`}>
              <div
                className="nowheel flex gap-1.5 overflow-x-auto px-1.5 py-1 [scrollbar-width:none]"
                style={{ maxWidth: VISIBLE_THUMBS * 58 + 12 }}
              >
                {outputs.map((output, index) => {
                  const active = output.id === activeId;
                  return (
                    <button
                      key={output.id}
                      type="button"
                      draggable
                      aria-label={`第 ${index + 1} 版${active ? "（当前）" : ""}`}
                      aria-pressed={active}
                      onClick={() => onSelect(output.id)}
                      onDragStart={(event) => {
                        event.dataTransfer.setData(NODE_OUTPUT_MIME, JSON.stringify(output));
                        event.dataTransfer.effectAllowed = "copy";
                      }}
                      className={cn(
                        "bg-muted relative h-7.5 w-13 shrink-0 cursor-pointer overflow-hidden rounded-[7px] transition-opacity",
                        active ? "opacity-100" : "opacity-60 hover:opacity-100",
                      )}
                    >
                      <Thumb output={output} />
                      {active && (
                        <motion.span
                          layoutId="history-active"
                          transition={SPRING}
                          className="ring-foreground pointer-events-none absolute inset-0 rounded-[7px] ring-2 ring-inset"
                        />
                      )}
                    </button>
                  );
                })}
              </div>
            </LayoutGroup>
          </motion.div>
        )}
      </AnimatePresence>

      <motion.span layout="position" className="bg-chrome-border mx-1 h-4.5 w-px" />
      <motion.span
        layout="position"
        role="status"
        aria-label={running ? "生成中" : count ? "当前版本已就绪" : "尚未生成"}
        className={cn(
          "mx-2 size-4.5 rounded-full ring-[1.5px] ring-inset",
          running
            ? "ring-status-running/25 border-status-running animate-spin border-[1.5px] border-r-transparent border-b-transparent border-l-transparent"
            : count
              ? "ring-status-success"
              : "ring-muted-foreground/60",
        )}
      />
    </motion.div>
  );
}

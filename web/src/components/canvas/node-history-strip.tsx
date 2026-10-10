import { LayoutGroup, motion } from "motion/react";
import { AudioLines } from "lucide-react";

import { SPRING } from "@/lib/motion";
import { cn } from "@/lib/utils";
import type { NodeOutput } from "@/types";

import { MediaPreview, VideoPoster } from "./media-preview";

/** 拖出缩略图时塞进 dataTransfer 的类型，画布 onDrop 认它来建节点 */
export const NODE_OUTPUT_MIME = "application/x-video-canvas-output";

/** 一行最多露几张缩略图，多了横向滚动 */
const VISIBLE_THUMBS = 8;

function Thumb({ output }: { output: NodeOutput }) {
  if (output.mediaType === "image")
    return <MediaPreview src={output.src} mode="thumb" fit="cover" draggable={false} />;
  // 历史条一次摆多个视频，只显示封面，不挂载 video
  if (output.mediaType === "video") return <VideoPoster src={output.src} iconClassName="size-4" />;
  return (
    <span className="text-muted-foreground grid size-full place-items-center">
      <AudioLines className="size-4" />
    </span>
  );
}

/**
 * 节点生成历史的缩略图列表（设计稿 6.3、6.16）：放在功能区「历史」按钮点开的弹层里。
 * 当前版本的白框在缩略图间弹簧滑动；点缩略图切版本；把缩略图拖到画布上会建一个以它为素材的新节点。
 */
export function HistoryThumbs({
  nodeId,
  outputs,
  activeId,
  onSelect,
  className,
}: {
  nodeId: string;
  outputs: NodeOutput[];
  activeId?: string;
  onSelect: (outputId: string) => void;
  className?: string;
}) {
  return (
    <LayoutGroup id={`history-${nodeId}`}>
      <div
        className={cn("nowheel flex gap-1.5 overflow-x-auto scrollbar-none", className)}
        style={{ maxWidth: VISIBLE_THUMBS * 58 }}
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
                "bg-muted focus-visible:ring-node-ring relative h-7.5 w-13 shrink-0 cursor-pointer overflow-hidden rounded-[7px] outline-none transition-opacity focus-visible:ring-2",
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
  );
}

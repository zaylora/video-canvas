import { useState } from "react";
import { Reorder } from "motion/react";
import { CircleX, Film, Layers, Loader2, Play, Plus } from "lucide-react";

import { VideoPoster } from "@/components/canvas/media-preview";
import { StatusPill } from "@/components/drama/status-pill";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";
import type { Segment } from "@/api/drama/type";
import {
  applyOrder,
  clipWidth,
  orderIds,
  playableTake,
  sameOrder,
  totalSeconds,
} from "@/utils/drama/segments";

import { useStudio } from "./use-studio";

const mmss = (seconds: number) =>
  `${String(Math.floor(seconds / 60)).padStart(2, "0")}:${String(Math.floor(seconds % 60)).padStart(2, "0")}`;

/** 片段条上的一格：宽度按时长；有采用视频显示封面，否则按状态显示图标 */
function Clip({
  segment,
  selected,
  onSelect,
}: {
  segment: Segment;
  selected: boolean;
  onSelect: () => void;
}) {
  const { submitting } = useStudio();
  const take = playableTake(segment);
  const running = segment.status === "running" || submitting.has(segment.id);
  return (
    <Reorder.Item
      value={segment}
      as="div"
      role="button"
      tabIndex={0}
      aria-current={selected}
      aria-label={`片段 ${segment.seq + 1}，${segment.seconds} 秒`}
      style={{ width: clipWidth(segment.seconds) }}
      // 点击（不是拖动）才选中：拖动结束不会误触
      onTap={onSelect}
      onKeyDown={(event) => {
        if (event.key === "Enter" || event.key === " ") {
          event.preventDefault();
          onSelect();
        }
      }}
      whileDrag={{ scale: 1.03, zIndex: 2 }}
      className={cn(
        "bg-muted relative h-[68px] shrink-0 cursor-grab touch-none overflow-hidden rounded-lg outline-none select-none active:cursor-grabbing",
        "focus-visible:ring-ring/50 focus-visible:ring-2",
        selected ? "ring-foreground ring-2" : "ring-border ring-1",
      )}
    >
      {take?.asset ? (
        <VideoPoster src={take.asset.url} />
      ) : (
        <div className="text-muted-foreground grid size-full place-items-center pb-4">
          {running ? (
            <Loader2 className="size-4 animate-spin" />
          ) : segment.status === "error" ? (
            <CircleX className="text-destructive size-4" />
          ) : (
            <Film className="size-4" />
          )}
        </div>
      )}
      <div
        className={cn(
          "absolute inset-x-0 bottom-0 flex justify-between px-1.5 py-0.5 text-[11px] tabular-nums",
          take ? "bg-black/55 text-white" : "bg-background/70 text-muted-foreground",
        )}
      >
        <span>{String(segment.seq + 1).padStart(2, "0")}</span>
        <span>{segment.seconds}s</span>
      </div>
    </Reorder.Item>
  );
}

/**
 * 底部片段条：宽度按时长、选中描边、状态；拖动排序（motion Reorder，松手后一次提交整个顺序）；
 * 新增、批量生成（对没有采用视频的片段逐个提交）；连播按钮显示但禁用（三期）。
 */
export function ClipBar() {
  const { segments, selected, select, actions } = useStudio();
  // 拖动过程中只改本地顺序，松手再提交；store 里的片段一变（推送、服务端确认）就回到 store 的版本
  const [local, setLocal] = useState<{ base: Segment[]; list: Segment[] } | null>(null);
  const shown = local && local.base === segments ? local.list : segments;

  const commit = () => {
    if (!local || local.base !== segments) return;
    const ids = orderIds(local.list);
    if (sameOrder(ids, orderIds(segments))) {
      setLocal(null);
      return;
    }
    // 先把本地顺序留在界面上（不跳回去），再等服务端确认
    setLocal({ base: segments, list: applyOrder(segments, ids) });
    void actions.reorder(ids);
  };

  return (
    <div className="border-border bg-sidebar shrink-0 border-t px-4 pt-2.5 pb-3">
      <div className="mb-1.5 flex items-center gap-2">
        <Button
          variant="ghost"
          size="icon-sm"
          disabled
          aria-label="连播（三期）"
          title="连播在三期上线"
        >
          <Play />
        </Button>
        <span className="text-muted-foreground text-xs tabular-nums">
          {mmss(0)} / {mmss(totalSeconds(segments))}
        </span>
        <StatusPill tone="muted" className="h-4 text-[10px]">
          连播 · 三期
        </StatusPill>
        <span className="flex-1" />
        <Button variant="ghost" size="sm" onClick={() => void actions.batch()}>
          <Layers />
          批量生成
        </Button>
        <span className="text-muted-foreground text-[11.5px] max-sm:hidden">拖动片段可排序</span>
      </div>
      <div className="flex items-center gap-1.5 overflow-x-auto pb-1">
        <Reorder.Group
          axis="x"
          as="div"
          values={shown}
          onReorder={(list) => setLocal({ base: segments, list })}
          onPointerUp={commit}
          className="flex shrink-0 gap-1"
        >
          {shown.map((segment) => (
            <Clip
              key={segment.id}
              segment={segment}
              selected={selected?.id === segment.id}
              onSelect={() => select(segment.id)}
            />
          ))}
        </Reorder.Group>
        <Button
          variant="outline"
          size="icon"
          className="h-[68px] w-10 shrink-0 border-dashed"
          aria-label="新增片段"
          title="新增片段"
          onClick={() => void actions.add()}
        >
          <Plus />
        </Button>
      </div>
    </div>
  );
}

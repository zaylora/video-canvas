import { useState } from "react";
import { Reorder } from "motion/react";
import { CircleX, Film, Layers, Loader2, Pause, Play, Plus } from "lucide-react";

import { VideoPoster } from "@/components/canvas/media-preview";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";
import type { Segment } from "@/api/drama/type";
import { applyOrder, clipWidth, orderIds, playableTake, sameOrder } from "@/utils/drama/segments";

import { usePlaybackContext } from "./use-playback";
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
        "bg-muted relative h-[76px] shrink-0 cursor-grab touch-none overflow-hidden rounded-lg outline-none select-none active:cursor-grabbing",
        "focus-visible:ring-ring/50 focus-visible:ring-2",
        selected
          ? "ring-primary shadow-[0_0_0_3px_color-mix(in_oklch,var(--primary)_18%,transparent)] ring-2"
          : "ring-border hover:ring-foreground/30 ring-1",
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
 * 新增、批量生成（对没有采用视频的片段逐个提交）；
 * 连播：按 seq 依次播采用的视频，播放头随时间移动，点片段跳到该段开头。
 */
export function ClipBar() {
  const { segments, selected, select, actions } = useStudio();
  const playback = usePlaybackContext();
  const { bindPlayhead } = playback;
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
    <div className="bg-sidebar h-full overflow-y-auto px-4 pt-2.5 pb-3">
      <div className="mb-1.5 flex items-center gap-2">
        <Button
          variant="ghost"
          size="icon-sm"
          disabled={!playback.available}
          aria-label={playback.playing ? "暂停连播" : "连播"}
          aria-pressed={playback.playing}
          title={
            playback.available
              ? playback.playing
                ? "暂停连播"
                : playback.active
                  ? "继续连播"
                  : "连播：按顺序播放各片段采用的视频，没有采用的跳过"
              : "还没有片段采用视频，无法连播"
          }
          onClick={playback.toggle}
        >
          {playback.playing ? <Pause /> : <Play />}
        </Button>
        <span className="text-muted-foreground text-xs tabular-nums">
          {mmss(playback.active ? playback.elapsed : 0)} / {mmss(playback.total)}
        </span>
        {playback.active && (
          <span className="bg-beam/15 text-beam inline-flex h-4 items-center rounded-full px-2 text-[10px] font-medium">
            {playback.playing ? "连播中" : "已暂停"}
          </span>
        )}
        <span className="flex-1" />
        <Button variant="ghost" size="sm" onClick={() => void actions.batch()}>
          <Layers />
          批量生成
        </Button>{" "}
      </div>
      <div className="flex items-center gap-1.5 overflow-x-auto pb-1">
        <div className="relative shrink-0">
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
                onSelect={() => {
                  select(segment.id);
                  playback.jumpTo(segment.id);
                }}
              />
            ))}
          </Reorder.Group>
          {/* 播放头：位置由连播 hook 直接写 transform，没有 CSS 过渡，减弱动态时也不会有动画 */}
          {playback.active && (
            <div
              ref={bindPlayhead}
              aria-hidden
              className="bg-beam pointer-events-none absolute inset-y-0 left-0 z-10 w-0.5 rounded-full"
            />
          )}
        </div>
        <Button
          variant="outline"
          size="icon"
          className="hover:border-primary hover:text-primary h-[76px] w-12 shrink-0 border-dashed"
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

import { useState } from "react";
import { CircleAlert, Download, ImagePlus, Music, Play, RotateCw } from "lucide-react";

import type { RecordDto } from "@/api/conversation/type";
import type { TaskOutput } from "@/api/generation-task/type";
import { MediaPreview, VideoPoster } from "@/components/canvas/media-preview";
import { GenerationStatus } from "@/components/generation-status";
import { SoonTip } from "@/components/home/soon";
import { cn } from "@/lib/utils";
import { useTask } from "@/store/tasks";
import { useNow } from "@/hooks/use-now";
import { cellNeedsClock, deriveCell } from "@/utils/conversation/cell";
import { cellAspect, shapeOfAspect } from "@/utils/conversation/shape";
import { useMediaAspect } from "@/hooks/use-media-aspect";
import { downloadMedia } from "@/utils/canvas/download";

/** 各画幅的尺寸：高度固定，宽度由宽高比决定 */
const SHAPE = {
  square: "h-48 aspect-square md:h-60",
  wide: "h-48 aspect-video md:h-60",
  tall: "h-60 aspect-[9/16] md:h-75",
} as const;

/** 悬停时出现在结果右下角的小按钮 */
const OVERLAY_BUTTON =
  "grid size-7 place-items-center rounded-lg bg-black/55 text-white outline-none backdrop-blur-sm hover:bg-black/75 focus-visible:ring-2 focus-visible:ring-white/70";

/** 没有结果时的占位格：灰底、居中内容 */
const PLACEHOLDER =
  "bg-muted text-muted-foreground grid place-items-center rounded-xl px-3 text-center text-[12.5px]";

/** 点开预览时交给调用方的信息：看哪个任务的产出，以及弹层从屏幕哪个点长出来 */
export type CellPreviewTarget = {
  /** 任务 ID，灯箱靠它认出点开的是哪一项 */
  id: string;
  output: TaskOutput;
  /** 被点格子在屏幕上的中心，灯箱的缩放原点 */
  origin: { x: number; y: number };
};

/**
 * 一条记录里的一个结果格子。状态由任务快照推出（见 utils/conversation/cell.ts），
 * 格子自己订阅任务库，WebSocket 推送和断线对账来的新进度会直接反映在这里。
 * 失败、取消、提交失败的格子可以单独重试；成功的格子可以预览、下载、用作参考。
 * @param record 所在记录
 * @param index 格子序号，从 0 开始
 * @param onPreview 点开预览：图片、视频进媒体灯箱，音频进播放弹窗，由调用方分流
 * @param onRetry 重试这一格
 * @param onUseAsRef 把这个结果用作参考图
 */
export function ResultCell({
  record,
  index,
  onPreview,
  onRetry,
  onUseAsRef,
}: {
  record: RecordDto;
  index: number;
  onPreview: (target: CellPreviewTarget) => void;
  onRetry: (index: number) => void;
  onUseAsRef: (output: TaskOutput) => void;
}) {
  const initial = record.tasks[index];
  const live = useTask(initial ? String(initial.id) : undefined);
  const task = live ?? initial ?? undefined;
  /** 任务没结束就走表：已耗时每秒刷新一次，和画布节点一致（见 cellNeedsClock） */
  const now = useNow(cellNeedsClock(task));
  const cell = deriveCell(record, index, task, now);
  const audio = record.kind === "audio";
  /** 视频封面是否已就绪：就绪后播放按钮才和封面一起淡入 */
  const [posterSettled, setPosterSettled] = useState(false);
  const doneOutput = cell.kind === "done" ? cell.output : null;
  const mediaType =
    doneOutput?.media_type === "image" || doneOutput?.media_type === "video"
      ? doneOutput.media_type
      : undefined;
  const measured = useMediaAspect(doneOutput?.url, mediaType);
  /** 各阶段共用同一个宽高比：成片真实比例优先，其次所选比例，Auto 且没量到时按默认横版 */
  const aspect = audio ? null : cellAspect(record.input, measured);
  const size = audio
    ? "h-18 w-full max-w-90"
    : SHAPE[aspect === null ? "wide" : shapeOfAspect(aspect)];
  /** 按宽高比定宽度（高度由档位类给）；占位和成片都用它，加载前后框不变形 */
  const frame = aspect === null ? undefined : { aspectRatio: aspect };
  const retry = (
    <button
      type="button"
      onClick={() => onRetry(index)}
      className="bg-background/80 hover:bg-background mt-2 inline-flex h-7 items-center gap-1 rounded-lg px-2.5 text-xs font-medium"
    >
      <RotateCw className="size-3.5" />
      重试
    </button>
  );

  switch (cell.kind) {
    case "submit_error":
      return (
        <div className={cn(PLACEHOLDER, size)} style={frame}>
          <div className="grid justify-items-center">
            <CircleAlert className="mb-1 size-4" />
            提交失败：{cell.message}
            {retry}
          </div>
        </div>
      );
    case "missing":
      return (
        <div className={cn(PLACEHOLDER, size)} style={frame}>
          找不到这个任务
        </div>
      );
    case "queued":
    case "running":
    case "finalizing": {
      /** 各入口共用相同的任务状态展示。 */
      const phase =
        cell.kind === "running"
          ? ({ phase: "running", elapsedMs: cell.elapsedMs, progress: cell.progress } as const)
          : ({ phase: cell.kind } as const);
      return (
        <GenerationStatus
          view={phase}
          mediaType={record.kind}
          className={cn("rounded-xl", size)}
          style={frame}
        />
      );
    }
    case "failed":
      return (
        <GenerationStatus
          view={{ phase: "failed", message: cell.message, refunded: true, taskRef: cell.taskRef }}
          onRetry={() => onRetry(index)}
          className={cn("rounded-xl", size)}
          style={frame}
        />
      );
    case "canceled":
      return (
        <div className={cn(PLACEHOLDER, size)} style={frame}>
          <div className="grid justify-items-center">
            已取消 · {cell.refunded ? "积分已退回" : "积分不退回"}
            {retry}
          </div>
        </div>
      );
  }

  const { output } = cell;
  const url = output.url ?? "";
  const download = () =>
    void downloadMedia(url, `${record.prompt.slice(0, 20) || "生成结果"}-${index + 1}`);
  return (
    <div
      data-slot="result-cell"
      className={cn("group/cell relative max-w-full overflow-hidden rounded-xl", size)}
      style={frame}
    >
      <button
        type="button"
        aria-label={audio ? "播放音频" : output.media_type === "video" ? "播放视频" : "查看大图"}
        onClick={(event) => {
          const rect = event.currentTarget.getBoundingClientRect();
          onPreview({
            id: String(task?.id),
            output,
            origin: { x: rect.left + rect.width / 2, y: rect.top + rect.height / 2 },
          });
        }}
        className="focus-visible:ring-ring/60 bg-muted block size-full outline-none focus-visible:ring-3"
      >
        {audio ? (
          <span className="flex size-full items-center gap-3 px-3.5">
            <Music className="text-muted-foreground size-4 shrink-0" />
            <span className="h-7 flex-1 [mask-image:linear-gradient(transparent,#000_30%,#000_70%,transparent)] bg-[repeating-linear-gradient(90deg,color-mix(in_oklch,var(--foreground)_45%,transparent)_0_2px,transparent_2px_5px)]" />
            <Play className="size-4" />
          </span>
        ) : output.media_type === "video" ? (
          <>
            <VideoPoster src={url} onSettledChange={setPosterSettled} />
            <span
              className={cn(
                "absolute inset-0 m-auto grid size-11 place-items-center rounded-full bg-black/45 text-white backdrop-blur-sm transition-opacity duration-200",
                !posterSettled && "opacity-0",
              )}
            >
              <Play className="size-4 fill-current" />
            </span>
          </>
        ) : (
          <MediaPreview src={url} alt={record.prompt} mode="full" draggable={false} />
        )}
      </button>
      {record.count > 1 && (
        <span className="absolute top-2 left-2 inline-grid h-5 place-items-center rounded-md bg-black/50 px-1.5 text-[11px] text-white tabular-nums">
          {index + 1}
        </span>
      )}
      <div className="absolute right-2 bottom-2 flex gap-1 opacity-0 transition-opacity group-focus-within/cell:opacity-100 group-hover/cell:opacity-100">
        <button
          type="button"
          aria-label="下载"
          title="下载"
          onClick={download}
          className={OVERLAY_BUTTON}
        >
          <Download className="size-3.5" />
        </button>
        <button
          type="button"
          aria-label="用作参考"
          title="用作参考（图片、视频、音频都可以）"
          onClick={() => onUseAsRef(output)}
          className={OVERLAY_BUTTON}
        >
          <ImagePlus className="size-3.5" />
        </button>
        <SoonTip className="inline-flex cursor-not-allowed">
          <button
            type="button"
            disabled
            aria-label="发到画布，即将上线"
            className={cn(OVERLAY_BUTTON, "opacity-60")}
          >
            <span className="text-[10px] leading-none">画布</span>
          </button>
        </SoonTip>
      </div>
    </div>
  );
}

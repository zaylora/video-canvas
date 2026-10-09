import {
  CircleAlert,
  CircleX,
  Download,
  ImagePlus,
  Loader2,
  Music,
  Play,
  RotateCw,
} from "lucide-react";

import type { RecordDto } from "@/api/conversation/type";
import type { TaskOutput } from "@/api/generation-task/type";
import { SoonTip } from "@/components/home/soon";
import { cn } from "@/lib/utils";
import { useTask } from "@/store/tasks";
import { useNow } from "@/hooks/use-now";
import { deriveCell } from "@/utils/conversation/cell";
import { formatElapsed } from "@/utils/tasks/node-view";
import { ratioOf, shapeOf } from "@/utils/conversation/shape";
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

/** 各种素材在「即将完成」时的说法，与画布节点一致 */
const SAVING_TEXT = {
  image: "正在保存图片",
  video: "正在保存视频",
  audio: "正在保存音频",
} as const;

/** 排队中 / 生成中 / 即将完成共用的占位：转圈 + 标题（+ 进度），下面一行小字，进度已知时再画进度条 */
function Pending({
  size,
  title,
  detail,
  progress,
}: {
  size: string;
  title: string;
  detail: string;
  progress?: number | null;
}) {
  return (
    <div data-slot="result-cell" role="status" aria-live="polite" className={cn(PLACEHOLDER, size)}>
      <div className="grid justify-items-center gap-1.5">
        <span className="text-foreground/80 inline-flex items-center gap-1.5 text-sm font-medium">
          <Loader2 className="size-3.5 animate-spin" />
          {title}
          {progress !== null && progress !== undefined && (
            <span className="tabular-nums">{progress}%</span>
          )}
        </span>
        <span className="text-xs tabular-nums">{detail}</span>
        {progress !== null && progress !== undefined && (
          <span className="bg-foreground/10 h-1 w-28 overflow-hidden rounded-full">
            <span
              className="bg-foreground/60 block h-full rounded-full transition-[width] duration-500"
              style={{ width: `${progress}%` }}
            />
          </span>
        )}
      </div>
    </div>
  );
}

/**
 * 一条记录里的一个结果格子。状态由任务快照推出（见 utils/conversation/cell.ts），
 * 格子自己订阅任务库，WebSocket 推送和断线对账来的新进度会直接反映在这里。
 * 失败、取消、提交失败的格子可以单独重试；成功的格子可以预览、下载、用作参考。
 * @param record 所在记录
 * @param index 格子序号，从 0 开始
 * @param onPreview 点开预览
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
  onPreview: (output: TaskOutput) => void;
  onRetry: (index: number) => void;
  onUseAsRef: (output: TaskOutput) => void;
}) {
  const initial = record.tasks[index];
  const live = useTask(initial ? String(initial.id) : undefined);
  const task = live ?? initial ?? undefined;
  /** 生成中才走表：已耗时每秒刷新一次，其余状态不用 */
  const now = useNow(task?.status === "queued" || task?.status === "running");
  const cell = deriveCell(record, index, task, now);
  const audio = record.kind === "audio";
  const size = audio ? "h-18 w-full max-w-90" : SHAPE[shapeOf(ratioOf(record.input))];
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
        <div data-slot="result-cell" className={cn(PLACEHOLDER, size)}>
          <div className="grid justify-items-center">
            <CircleAlert className="mb-1 size-4" />
            提交失败：{cell.message}
            {retry}
          </div>
        </div>
      );
    case "missing":
      return (
        <div data-slot="result-cell" className={cn(PLACEHOLDER, size)}>
          找不到这个任务
        </div>
      );
    case "queued":
      return <Pending size={size} title="排队中" detail="前面的任务处理完后会自动开始" />;
    case "running":
      return (
        <Pending
          size={size}
          title="生成中"
          progress={cell.progress}
          detail={
            cell.elapsedMs === null
              ? "正在同步任务状态…"
              : `已耗时 ${formatElapsed(cell.elapsedMs)}`
          }
        />
      );
    case "finalizing":
      return <Pending size={size} title="即将完成" detail={SAVING_TEXT[record.kind]} />;
    case "failed":
      return (
        <div data-slot="result-cell" className={cn(PLACEHOLDER, size)}>
          <div className="grid justify-items-center">
            <CircleX className="text-destructive mb-1 size-4" />
            {cell.message}
            <span className="text-[11px] opacity-70">积分已退回</span>
            {retry}
          </div>
        </div>
      );
    case "canceled":
      return (
        <div data-slot="result-cell" className={cn(PLACEHOLDER, size)}>
          <div className="grid justify-items-center">
            已取消 · 积分已退回
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
      className={cn("group/cell relative overflow-hidden rounded-xl", size)}
    >
      <button
        type="button"
        aria-label={audio ? "播放音频" : output.media_type === "video" ? "播放视频" : "查看大图"}
        onClick={() => onPreview(output)}
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
            <video
              src={url}
              muted
              preload="metadata"
              playsInline
              className="size-full object-cover"
            />
            <span className="absolute inset-0 m-auto grid size-11 place-items-center rounded-full bg-black/45 text-white backdrop-blur-sm">
              <Play className="size-4 fill-current" />
            </span>
          </>
        ) : (
          <img src={url} alt={record.prompt} loading="lazy" className="size-full object-cover" />
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

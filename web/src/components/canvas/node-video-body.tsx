import { Loader2, RotateCcw, TriangleAlert, X } from "lucide-react";

import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { formatElapsed, type VideoNodeView } from "@/utils/tasks/node-view";

import { BaseNodeContent } from "./base-node";
import {
  NODE_PREVIEW_ASPECT,
  NodeMediaBody,
  NodePlaceholderBody,
} from "./node-body";
import { VideoPlaceholderIcon } from "./placeholder-icons";

type NodeVideoBodyProps = {
  /** 由节点数据 + 任务快照推出的展示状态 */
  view: VideoNodeView;
  /** 素材下方的一行小字 */
  caption?: string;
  /** 占位框的无障碍说明 */
  placeholder: string;
  /** 取消任务，排队中 / 生成中才给按钮 */
  onCancel?: () => void;
  cancelling?: boolean;
  /** 失败后重试（重新提交一个新任务），不给就不显示按钮 */
  onRetry?: () => void;
  retryDisabled?: boolean;
  /** 重试按钮悬停时的说明，写清为什么点不了 */
  retryHint?: string;
};

/** 排队中、生成中、转存中共用的骨架屏，中间叠一行状态文字 */
function PendingBox({
  title,
  detail,
  progress,
  onCancel,
  cancelling,
}: {
  title: string;
  detail?: string;
  progress?: number | null;
  onCancel?: () => void;
  cancelling?: boolean;
}) {
  return (
    <BaseNodeContent>
      <div
        className="relative w-full overflow-hidden rounded-2xl border [corner-shape:squircle]"
        style={{ aspectRatio: NODE_PREVIEW_ASPECT }}
        role="status"
        aria-live="polite"
      >
        <Skeleton className="absolute inset-0 rounded-none" />
        <div className="absolute inset-0 flex flex-col items-center justify-center gap-2 px-4 text-center">
          <div className="text-foreground/80 flex items-center gap-2 text-sm font-medium">
            <Loader2 className="size-4 animate-spin" />
            {title}
            {progress != null && (
              <span className="tabular-nums">{progress}%</span>
            )}
          </div>
          {detail && (
            <p className="text-muted-foreground text-xs tabular-nums">{detail}</p>
          )}
          {progress != null && (
            <div className="bg-foreground/10 h-1 w-32 overflow-hidden rounded-full">
              <div
                className="bg-foreground/60 h-full rounded-full transition-[width] duration-500"
                style={{ width: `${progress}%` }}
              />
            </div>
          )}
        </div>
        {onCancel && (
          <Button
            variant="outline"
            size="xs"
            className="nodrag bg-card/80 absolute right-2 bottom-2 backdrop-blur"
            disabled={cancelling}
            onClick={onCancel}
          >
            {cancelling ? <Loader2 className="animate-spin" /> : <X />}
            取消
          </Button>
        )}
      </div>
    </BaseNodeContent>
  );
}

/**
 * 视频节点的正文，按设计 6.3 节的状态表：
 * 排队中 / 生成中 / 转存中给骨架屏，成功可播放，失败给原因、退款说明和重试。
 * 纯展示，任务状态怎么来的、按钮怎么接都由调用方决定。
 */
export function NodeVideoBody({
  view,
  caption,
  placeholder,
  onCancel,
  cancelling,
  onRetry,
  retryDisabled,
  retryHint,
}: NodeVideoBodyProps) {
  switch (view.phase) {
    case "queued":
      return (
        <PendingBox
          title="排队中"
          detail="已提交，等待平台开始生成"
          onCancel={onCancel}
          cancelling={cancelling}
        />
      );
    case "running":
      return (
        <PendingBox
          title="生成中"
          detail={
            view.elapsedMs === null
              ? "正在同步任务状态…"
              : `已耗时 ${formatElapsed(view.elapsedMs)}`
          }
          progress={view.progress}
          onCancel={onCancel}
          cancelling={cancelling}
        />
      );
    case "finalizing":
      return <PendingBox title="即将完成" detail="正在保存视频" />;
    case "done":
      return <NodeMediaBody src={view.src} mediaType="video" caption={caption} />;
    case "failed":
      return (
        <BaseNodeContent>
          <div
            className="border-destructive/40 bg-destructive/5 text-destructive flex w-full flex-col items-center justify-center gap-2 rounded-2xl border px-4 text-center text-xs [corner-shape:squircle]"
            style={{ aspectRatio: NODE_PREVIEW_ASPECT }}
            role="alert"
          >
            <TriangleAlert className="size-4 shrink-0" />
            <p className="line-clamp-3 break-words">{view.message}</p>
            {view.refunded && (
              <p className="text-muted-foreground">积分已退回</p>
            )}
            {onRetry && (
              <span title={retryHint}>
                <Button
                  variant="outline"
                  size="xs"
                  className="nodrag"
                  disabled={retryDisabled}
                  onClick={onRetry}
                >
                  <RotateCcw />
                  重试
                </Button>
              </span>
            )}
          </div>
        </BaseNodeContent>
      );
    default:
      return (
        <NodePlaceholderBody
          icon={<VideoPlaceholderIcon className="size-10" />}
          label={placeholder}
        />
      );
  }
}

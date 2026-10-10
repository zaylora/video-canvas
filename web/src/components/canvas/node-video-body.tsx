import type { ReactNode } from "react";
import { Loader2, RotateCcw, TriangleAlert, X } from "lucide-react";

import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { TaskIdTag } from "@/components/task-id-tag";
import { formatElapsed, type VideoNodeView } from "@/utils/tasks/node-view";

import { BaseNodeContent } from "./base-node";
import {
  NODE_PREVIEW_ASPECT,
  NodeMediaBody,
  NodePlaceholderBody,
  type NodeMediaType,
} from "./node-body";
import { VideoPlaceholderIcon } from "./placeholder-icons";

/** 各种素材在「即将完成」时的说法 */
const SAVING_TEXT: Record<NodeMediaType, string> = {
  image: "正在保存图片",
  video: "正在保存视频",
  audio: "正在保存音频",
};

type NodeVideoBodyProps = {
  /** 由节点数据 + 任务快照推出的展示状态 */
  view: VideoNodeView;
  /** 占位框的无障碍说明 */
  placeholder: string;
  /** 产物种类，决定成功后怎么摆；默认视频 */
  mediaType?: NodeMediaType;
  /** 空状态占位框里的大图标；默认视频图标 */
  placeholderIcon?: ReactNode;
  /** 取消任务，排队中 / 生成中才给按钮 */
  onCancel?: () => void;
  cancelling?: boolean;
  /** 失败后重试（重新提交一个新任务），不给就不显示按钮 */
  onRetry?: () => void;
  retryDisabled?: boolean;
  /** 重试按钮悬停时的说明，写清为什么点不了 */
  retryHint?: string;
  /** 节点是否被选中；视频失去选中会停止播放并退回封面 */
  active?: boolean;
  /** 预览画幅（宽 / 高），默认 16:9；图片节点按图片真实比例或面板选的比例传进来 */
  aspect?: number;
  /** 图片加载出来后回调真实的像素尺寸 */
  onImageSize?: (width: number, height: number) => void;
};

/** 排队中、生成中、转存中共用的骨架屏，中间叠一行状态文字 */
function PendingBox({
  title,
  detail,
  progress,
  onCancel,
  cancelling,
  aspect = NODE_PREVIEW_ASPECT,
}: {
  aspect?: number;
  title: string;
  detail?: string;
  progress?: number | null;
  onCancel?: () => void;
  cancelling?: boolean;
}) {
  return (
    <BaseNodeContent>
      <div
        className="relative w-full overflow-hidden rounded-[inherit]"
        style={{ aspectRatio: aspect }}
        role="status"
        aria-live="polite"
      >
        <Skeleton className="absolute inset-0 rounded-none" />
        <div className="absolute inset-0 flex flex-col items-center justify-center gap-2 px-4 text-center">
          <div className="text-foreground/80 flex items-center gap-2 text-sm font-medium">
            <Loader2 className="size-4 animate-spin" />
            {title}
            {progress != null && <span className="tabular-nums">{progress}%</span>}
          </div>
          {detail && <p className="text-muted-foreground text-xs tabular-nums">{detail}</p>}
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
  placeholder,
  mediaType = "video",
  placeholderIcon,
  onCancel,
  cancelling,
  onRetry,
  retryDisabled,
  retryHint,
  active,
  aspect = NODE_PREVIEW_ASPECT,
  onImageSize,
}: NodeVideoBodyProps) {
  switch (view.phase) {
    case "queued":
      return (
        <PendingBox
          title="排队中"
          detail="前面的任务处理完后会自动开始"
          onCancel={onCancel}
          cancelling={cancelling}
          aspect={aspect}
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
          aspect={aspect}
        />
      );
    case "uploading":
      // 字节传完后服务端还要登记、处理一会儿，进度条满了也不算完成
      return (
        <PendingBox
          title={view.progress >= 100 ? "处理中" : "上传中"}
          detail={view.progress >= 100 ? "文件已传完，正在保存" : undefined}
          progress={view.progress}
          aspect={aspect}
        />
      );
    case "finalizing":
      return <PendingBox title="即将完成" detail={SAVING_TEXT[mediaType]} aspect={aspect} />;
    case "done":
      return (
        <NodeMediaBody
          src={view.src}
          mediaType={mediaType}
          active={active}
          aspect={aspect}
          onImageSize={onImageSize}
        />
      );
    case "failed":
      return (
        <BaseNodeContent>
          <div
            className="bg-destructive/5 text-destructive flex w-full flex-col items-center justify-center gap-2 rounded-[inherit] px-4 text-center text-xs"
            style={{ aspectRatio: aspect }}
            role="alert"
          >
            <TriangleAlert className="size-4 shrink-0" />
            <p className="line-clamp-3 break-words">{view.message}</p>
            {view.refunded && <p className="text-muted-foreground">积分已退回</p>}
            <TaskIdTag id={view.taskRef} className="nodrag nowheel" />
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
          icon={placeholderIcon ?? <VideoPlaceholderIcon className="size-10" />}
          label={placeholder}
          aspect={aspect}
        />
      );
  }
}

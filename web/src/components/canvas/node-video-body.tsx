import type { ReactNode } from "react";

import { FailedPlaceholder, PendingPlaceholder } from "@/components/generation-placeholder";
import { pendingCopy } from "@/utils/tasks/pending-copy";
import type { VideoNodeView } from "@/utils/tasks/node-view";

import { BaseNodeContent } from "./base-node";
import {
  NODE_PREVIEW_ASPECT,
  NodeMediaBody,
  NodePlaceholderBody,
  type NodeMediaType,
} from "./node-body";
import { VideoPlaceholderIcon } from "./placeholder-icons";

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
  /** 框的画幅（宽 / 高），各阶段共用，状态切换时框不变形；缺省 16:9 */
  aspect?: number;
};

/** 节点里的占位框：外层负责圆角和画幅，各阶段共用，状态切换时框不变形 */
const BOX_CLASS = "w-full rounded-[inherit]";

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
}: NodeVideoBodyProps) {
  switch (view.phase) {
    case "queued":
    case "running":
    case "finalizing": {
      const copy = pendingCopy(view, mediaType);
      return (
        <BaseNodeContent>
          <PendingPlaceholder
            {...copy}
            // 只有排队中（还没提交给上游）能取消；已经在生成、转存的取消不了
            onCancel={view.phase === "queued" ? onCancel : undefined}
            cancelling={cancelling}
            className={BOX_CLASS}
            style={{ aspectRatio: aspect }}
          />
        </BaseNodeContent>
      );
    }
    case "uploading":
      // 字节传完后服务端还要登记、处理一会儿，进度条满了也不算完成
      return (
        <BaseNodeContent>
          <PendingPlaceholder
            title={view.progress >= 100 ? "处理中" : "上传中"}
            detail={view.progress >= 100 ? "文件已传完，正在保存" : undefined}
            progress={view.progress}
            className={BOX_CLASS}
            style={{ aspectRatio: aspect }}
          />
        </BaseNodeContent>
      );
    case "done":
      return <NodeMediaBody src={view.src} mediaType={mediaType} active={active} aspect={aspect} />;
    case "failed":
      return (
        <BaseNodeContent>
          <FailedPlaceholder
            message={view.message}
            refunded={view.refunded}
            taskRef={view.taskRef}
            onRetry={onRetry}
            retryDisabled={retryDisabled}
            retryHint={retryHint}
            className={BOX_CLASS}
            style={{ aspectRatio: aspect }}
          />
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

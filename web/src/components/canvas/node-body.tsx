import type { ReactNode } from "react";
import { Loader2, TriangleAlert } from "lucide-react";

import { BaseNodeContent } from "./base-node";
import { GridReveal } from "@/components/ui/grid-reveal";

import { ImagePlaceholderIcon } from "./placeholder-icons";

/** 节点的产出进度：没跑过、生成中、已产出、调用失败 */
export type NodeStatus = "idle" | "running" | "done" | "error";

/** 出图进度：演示状态机不会失败，所以图片节点用不到 error */
export type ImageStatus = Exclude<NodeStatus, "error">;

/** 节点预览统一的画幅：16:9 */
export const NODE_PREVIEW_ASPECT = 16 / 9;

type NodePlaceholderBodyProps = {
  /** 框里居中的大图标 */
  icon: ReactNode;
  /** 给读屏的说明，同时也是这个节点该干什么 */
  label: string;
  /** 占位画幅 */
  aspect?: number;
};

/**
 * 还没产出东西时的正文：一个撑满宽度的占位框，中间搁个图标。
 * 各种节点共用同一副画幅，卡片高度才对得齐。
 */
export function NodePlaceholderBody({
  icon,
  label,
  aspect = NODE_PREVIEW_ASPECT,
}: NodePlaceholderBodyProps) {
  return (
    <BaseNodeContent>
      <div
        className="bg-muted/40 text-muted-foreground/40 flex w-full items-center justify-center rounded-2xl border [corner-shape:squircle]"
        style={{ aspectRatio: aspect }}
        role="img"
        aria-label={label}
      >
        {icon}
      </div>
    </BaseNodeContent>
  );
}

type NodeImageBodyProps = {
  /** 出图进度 */
  status: ImageStatus;
  /** 出图结果地址，生成中为 null */
  src?: string | null;
  /** 图片替代文字 */
  alt?: string;
  /** 预览画幅，和 GridReveal 的栅格比例保持一致 */
  aspect?: number;
  /** 进度爬升的预估时长，毫秒 */
  estimatedDuration?: number;
  /** 未出图时占位框的无障碍说明 */
  placeholder?: string;
};

/**
 * 图片节点的正文：出图前是占位框，一按发送就交给 GridReveal——
 * 等待时栅格自己分裂爬升，图一到就顺势揭示成成片。
 */
export function NodeImageBody({
  status,
  src = null,
  alt,
  aspect = NODE_PREVIEW_ASPECT,
  estimatedDuration,
  placeholder = "选中节点，在下方写提示词生成",
}: NodeImageBodyProps) {
  return status === "idle" ? (
    <NodePlaceholderBody
      icon={<ImagePlaceholderIcon className="size-10" />}
      label={placeholder}
      aspect={aspect}
    />
  ) : (
    <BaseNodeContent>
      <GridReveal
        src={src}
        alt={alt}
        aspect={aspect}
        caption={status === "running" ? "生成中…" : "生成完成"}
        estimatedDuration={estimatedDuration}
      />
    </BaseNodeContent>
  );
}

type NodeTextBodyProps = {
  /** 生成进度 */
  status: NodeStatus;
  /** 生成出来的正文，还没跑出结果时为空 */
  text?: string | null;
  /** 调用失败时的原因 */
  error?: string | null;
  /** 还没跑过时占位框里的大图标 */
  icon: ReactNode;
  /** 占位框的无障碍说明，也是这个节点该干什么 */
  placeholder: string;
};

/**
 * 文本节点的正文：跑之前是占位框，跑起来占位框里转圈，
 * 出了结果就把正文摊开，失败则把原因摆在明面上。
 * 结果区留着 nodrag/nowheel，好让人在节点里选字、滚长文。
 */
export function NodeTextBody({
  status,
  text,
  error,
  icon,
  placeholder,
}: NodeTextBodyProps) {
  if (status === "running") {
    return (
      <BaseNodeContent>
        <div
          className="bg-muted/40 text-muted-foreground flex w-full items-center justify-center gap-2 rounded-2xl border text-xs [corner-shape:squircle]"
          style={{ aspectRatio: NODE_PREVIEW_ASPECT }}
        >
          <Loader2 className="size-4 animate-spin" />
          生成中…
        </div>
      </BaseNodeContent>
    );
  }

  if (status === "error") {
    return (
      <BaseNodeContent>
        <div
          className="border-destructive/40 bg-destructive/5 text-destructive flex w-full items-center justify-center gap-2 rounded-2xl border px-3 text-center text-xs [corner-shape:squircle]"
          style={{ aspectRatio: NODE_PREVIEW_ASPECT }}
          role="alert"
        >
          <TriangleAlert className="size-4 shrink-0" />
          <span className="line-clamp-3">{error ?? "生成失败"}</span>
        </div>
      </BaseNodeContent>
    );
  }

  // 出了结果才摊正文；跑完却空手而归，按没跑过处理，别留一块空白
  if (status === "done" && text) {
    return (
      <BaseNodeContent>
        <div className="nodrag nowheel bg-muted/40 max-h-48 min-h-24 w-full overflow-y-auto rounded-2xl border p-3 text-sm leading-6 whitespace-pre-wrap [corner-shape:squircle]">
          {text}
        </div>
      </BaseNodeContent>
    );
  }

  return <NodePlaceholderBody icon={icon} label={placeholder} />;
}

/** 本地上传能传进画布的素材：图片和视频两类 */
export type MediaType = "image" | "video";

/** 节点里摆得下的素材：上传的两类，加上生成出来的音频 */
export type NodeMediaType = MediaType | "audio";

type NodeMediaBodyProps = {
  /** 素材地址 */
  src: string;
  /** 素材种类，决定用 img、video 还是 audio 渲染 */
  mediaType: NodeMediaType;
  /** 图片替代文字 */
  alt?: string;
  /** 摆在素材下面的一行小字，通常是文件名 */
  caption?: string;
  /** 预览画幅 */
  aspect?: number;
};

/**
 * 现成素材的正文：本地传进来的、或是生成服务交回来的一份图/视频，
 * 摆在和别的节点一样的画幅里，视频带原生控件。
 */
export function NodeMediaBody({
  src,
  mediaType,
  alt,
  caption,
  aspect = NODE_PREVIEW_ASPECT,
}: NodeMediaBodyProps) {
  return (
    <BaseNodeContent>
      <div
        className="bg-muted/40 w-full overflow-hidden rounded-2xl border [corner-shape:squircle]"
        style={{ aspectRatio: aspect }}
      >
        {mediaType === "image" ? (
          <img src={src} alt={alt} className="size-full object-contain" />
        ) : mediaType === "audio" ? (
          // 音频没有画面，播放器居中摆在同一副画幅里
          <div className="flex size-full items-center justify-center px-4">
            <audio src={src} controls className="nodrag nowheel w-full" />
          </div>
        ) : (
          // nodrag 让拖进度条不至于把节点跟着拽走，nowheel 把滚轮留给画布
          <video
            src={src}
            controls
            playsInline
            className="nodrag nowheel size-full object-contain"
          />
        )}
      </div>
      {caption && (
        <p className="text-muted-foreground truncate text-xs" title={caption}>
          {caption}
        </p>
      )}
    </BaseNodeContent>
  );
}

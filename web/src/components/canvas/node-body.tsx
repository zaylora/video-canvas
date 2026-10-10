import { useState, type ReactNode } from "react";
import { Loader2, TriangleAlert } from "lucide-react";

import { TaskIdTag } from "@/components/task-id-tag";
import { TEXT_BODY_MAX } from "@/utils/canvas/paste";

import { BaseNodeContent } from "./base-node";
import { GridReveal } from "@/components/ui/grid-reveal";

import { MediaPreview } from "./media-preview";
import { ImagePlaceholderIcon } from "./placeholder-icons";
import { FramePickLayer } from "./frame-pick-layer";
import { VideoFacade } from "./video-facade";

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
        className="text-muted-foreground/45 flex w-full flex-col items-center justify-center gap-2.5 rounded-[inherit]"
        style={{ aspectRatio: aspect }}
        role="img"
        aria-label={label}
      >
        {icon}
        <span className="text-muted-foreground/70 text-xs">{label}</span>
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
  /** 失败任务的编号，到后端日志里定位用；没有为空 */
  taskRef?: string | null;
  /** 还没跑过时占位框里的大图标 */
  icon: ReactNode;
  /** 占位框的无障碍说明，也是这个节点该干什么 */
  placeholder: string;
  /** 双击正文编辑完（失焦或按 Esc）后回调，参数是编辑框里的全文；不给就不能编辑，生成中也不能编辑 */
  onTextChange?: (text: string) => void;
};

/**
 * 文本节点的正文：双击进入原地编辑，失焦或按 Esc 保存；
 * 生成中锁定，免得和即将回填的结果打架。
 */
export function NodeTextBody({ onTextChange, ...view }: NodeTextBodyProps) {
  const [draft, setDraft] = useState<string | null>(null);
  const editable = !!onTextChange && view.status !== "running";
  // 编辑中途变成不可编辑（比如任务回填触发了生成中），丢掉编辑态，免得生成完又弹回来
  if (draft !== null && !editable) setDraft(null);

  if (draft !== null && onTextChange) {
    return (
      <BaseNodeContent>
        <textarea
          autoFocus
          value={draft}
          maxLength={TEXT_BODY_MAX}
          aria-label="文本内容"
          placeholder="输入文字…"
          onFocus={(event) => {
            // 光标放到末尾，接着写
            const end = event.currentTarget.value.length;
            event.currentTarget.setSelectionRange(end, end);
          }}
          onChange={(event) => setDraft(event.target.value)}
          onKeyDown={(event) => {
            if (event.nativeEvent.isComposing) return;
            if (event.key === "Escape") {
              event.stopPropagation();
              event.currentTarget.blur();
            }
          }}
          onBlur={() => {
            setDraft(null);
            onTextChange(draft);
          }}
          // 选字、拖光标、滚长文都不能把节点或画布带走
          className="nodrag nowheel nopan bg-muted/40 block w-full resize-none rounded-[inherit] p-4 text-sm leading-6 outline-none"
          style={{ aspectRatio: NODE_PREVIEW_ASPECT }}
        />
      </BaseNodeContent>
    );
  }

  return (
    // contents 的外层只用来接双击，不参与布局
    <div
      className="contents"
      title={editable ? "双击编辑" : undefined}
      onDoubleClick={(event) => {
        if (!editable) return;
        // 别让画布把这次双击当成「在空白处新建节点」
        event.stopPropagation();
        setDraft(view.text ?? "");
      }}
    >
      <TextBodyView {...view} />
    </div>
  );
}

/**
 * 文本节点正文的展示：跑之前是占位框，跑起来占位框里转圈，
 * 出了结果就把正文摊开，失败则把原因摆在明面上。
 * 结果区只留 nowheel 好滚长文，不挡拖拽（整张卡片几乎都是正文，挡了就拖不动节点）；
 * 要选字、复制就双击进编辑框。
 */
function TextBodyView({
  status,
  text,
  error,
  taskRef,
  icon,
  placeholder,
}: Omit<NodeTextBodyProps, "onTextChange">) {
  if (status === "running") {
    return (
      <BaseNodeContent>
        <div
          className="bg-muted/40 text-muted-foreground flex w-full items-center justify-center gap-2 rounded-[inherit] text-xs"
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
          className="bg-destructive/5 text-destructive flex w-full flex-col items-center justify-center gap-1.5 rounded-[inherit] px-3 text-center text-xs"
          style={{ aspectRatio: NODE_PREVIEW_ASPECT }}
          role="alert"
        >
          <span className="flex items-center gap-2">
            <TriangleAlert className="size-4 shrink-0" />
            <span className="line-clamp-3">{error ?? "生成失败"}</span>
          </span>
          <TaskIdTag id={taskRef} className="nodrag nowheel" />
        </div>
      </BaseNodeContent>
    );
  }

  // 出了结果才摊正文；跑完却空手而归，按没跑过处理，别留一块空白
  if (status === "done" && text) {
    return (
      <BaseNodeContent>
        <div
          className="nowheel bg-muted/40 w-full overflow-y-auto rounded-[inherit] p-4 text-sm leading-6 whitespace-pre-wrap select-none"
          style={{ aspectRatio: NODE_PREVIEW_ASPECT }}
        >
          {text}
        </div>
      </BaseNodeContent>
    );
  }

  return <NodePlaceholderBody icon={icon} label={placeholder} />;
}

/** 本地上传能传进画布的素材：图片、视频和音频三类 */
export type MediaType = "image" | "video" | "audio";

/** 节点里摆得下的素材，和上传的种类一致 */
export type NodeMediaType = MediaType;

type NodeMediaBodyProps = {
  /** 素材地址 */
  src: string;
  /** 素材种类，决定用 img、video 还是 audio 渲染 */
  mediaType: NodeMediaType;
  /** 图片替代文字 */
  alt?: string;
  /** 预览画幅 */
  aspect?: number;
  /** 视频时长，毫秒；节点数据里没有就不传，视频封面不显示时长角标 */
  durationMs?: number | null;
  /** 所在节点是否被选中；视频失去选中会停止播放并退回封面，默认不受约束 */
  active?: boolean;
};

/**
 * 现成素材的正文：本地传进来的、或是生成服务交回来的一份图/视频，
 * 摆在和别的节点一样的画幅里。
 * 图片按画布缩放分档加载缩略图 / 原图；视频平时只是封面，点击才挂载 video。
 */
export function NodeMediaBody({
  src,
  mediaType,
  alt,
  aspect = NODE_PREVIEW_ASPECT,
  durationMs,
  active,
}: NodeMediaBodyProps) {
  return (
    <BaseNodeContent>
      <div
        className="bg-muted/40 w-full overflow-hidden rounded-[inherit]"
        style={{ aspectRatio: aspect }}
      >
        {mediaType === "image" ? (
          <MediaPreview src={src} alt={alt} />
        ) : mediaType === "audio" ? (
          // 音频没有画面，播放器居中摆在同一副画幅里
          <div className="flex size-full items-center justify-center px-4">
            <audio src={src} controls className="nodrag nowheel w-full" />
          </div>
        ) : (
          <div className="relative size-full">
            <VideoFacade src={src} durationMs={durationMs} active={active} />
            <FramePickLayer />
          </div>
        )}
      </div>
    </BaseNodeContent>
  );
}

import type { ReactNode } from "react";
import {
  Crosshair,
  FolderInput,
  Image,
  Link2,
  Maximize2,
  RefreshCw,
  TriangleAlert,
  Upload,
  X,
} from "lucide-react";

import { MediaPreview } from "@/components/canvas/media-preview";
import { cn } from "@/lib/utils";
import { useLinkedNodeView } from "@/hooks/use-linked-node-view";
import type { StoryboardShot } from "@/types";
import { shotImageState } from "@/utils/storyboard/shot-image";

/** 画格悬停面板里的一个操作：图标 + 短文字，占满所在的一格，完整说法放在 title 里 */
type CellAction = {
  key: string;
  title: string;
  label: string;
  icon: ReactNode;
  onClick: () => void;
};

function CellActionButton({ action, iconOnly }: { action: CellAction; iconOnly?: boolean }) {
  return (
    <button
      type="button"
      title={action.title}
      aria-label={action.title}
      onClick={(event) => {
        event.stopPropagation();
        action.onClick();
      }}
      className="nodrag flex min-w-0 flex-1 items-center justify-center gap-1.5 text-xs font-medium whitespace-nowrap text-white/80 outline-none transition-colors hover:bg-white/12 hover:text-white focus-visible:bg-white/12 focus-visible:text-white [&_svg]:size-3.5 [&_svg]:shrink-0"
    >
      {action.icon}
      {/* 四个操作平分 144px 时放不下两字标签，只留图标，完整说法在 title 上 */}
      {iconOnly ? <span className="sr-only">{action.label}</span> : action.label}
    </button>
  );
}

/**
 * 悬停 / 键盘聚焦时盖满画格的磨砂操作面板：主操作（生成 / 重新生成）占上半格，
 * 其余操作在下半格左右平分；只有一组时占满整格。细线分隔，不用胶囊。
 */
function CellActions({ primary, rest }: { primary?: CellAction; rest: CellAction[] }) {
  if (!primary && rest.length === 0) return null;
  return (
    <div className="absolute inset-0 flex flex-col divide-y divide-white/10 bg-black/60 opacity-0 backdrop-blur-sm transition-opacity duration-150 group-focus-within/thumb:opacity-100 group-hover/thumb:opacity-100">
      {primary && (
        <div className="flex min-h-0 flex-1">
          <CellActionButton action={primary} />
        </div>
      )}
      {rest.length > 0 && (
        <div className="flex min-h-0 flex-1 divide-x divide-white/10">
          {rest.map((action) => (
            <CellActionButton key={action.key} action={action} iconOnly={rest.length >= 4} />
          ))}
        </div>
      )}
    </div>
  );
}

/**
 * 分镜图格：144 × 81 的 16:9 画格（设计 §6.5）。状态由行和关联节点现算：
 * 空 / 排队或生成中 / 来自关联节点（左下角链接角标）/ 生成失败 / 旧数据里手动上传的。
 * 有图时只加载缩略图，「查看大图」打开预览看原图。
 * 操作都放在画格里，悬停或键盘聚焦才浮出磨砂面板（见 CellActions）。
 * @param uploading 这一格正在上传的进度（0–100）；没在上传为 undefined
 * @param dragging 有图片正拖在这一行上：整行任何位置都算拖到这一格，格子画出虚线框
 * @param onGenerate 生成 / 重新生成（复用同一个图片节点）
 * @param onUpload 点「上传」选文件
 * @param onImport 用画布上已有的图片当这一镜的分镜图（分镜表是后来加的、图不是从表里生成的时候用）
 * @param onRemove 移除手传的图
 * @param onLocate 关掉弹窗，把画布飞到关联节点
 * @param onPreview 打开预览看原图
 */
export function ShotImageCell({
  shot,
  number,
  uploading,
  dragging,
  onGenerate,
  onUpload,
  onImport,
  onRemove,
  onLocate,
  onPreview,
}: {
  shot: StoryboardShot;
  number: number;
  uploading?: number;
  dragging?: boolean;
  onGenerate: () => void;
  onUpload: () => void;
  onImport: () => void;
  onRemove: () => void;
  onLocate: (nodeId: string) => void;
  onPreview: () => void;
}) {
  const linked = useLinkedNodeView(shot.imageNodeId);
  const state = shotImageState(shot, linked);
  /** 挂的是哪一张：格子上只有图，挂错也看不出来，放进悬停提示里方便核对 */
  const mounted = linked?.exists ? linked.label || shot.imageNodeId : shot.imageNodeId;
  const generating =
    state.kind === "node" && (state.phase === "queued" || state.phase === "running");
  const regenerate = state.kind === "node" && (state.phase === "done" || state.phase === "failed");
  const src =
    state.kind === "uploaded" || (state.kind === "node" && state.phase === "done")
      ? state.src
      : null;

  return (
    <div
      tabIndex={0}
      data-slot="shot-image-cell"
      title={
        state.kind !== "node"
          ? undefined
          : state.phase === "failed"
            ? state.message
            : `已挂「${mounted}」`
      }
      className={cn(
        "group/thumb bg-muted text-muted-foreground/50 focus-visible:ring-node-ring/60 relative grid h-[81px] w-36 place-items-center overflow-hidden rounded-md outline-none focus-visible:ring-2",
        generating && "animate-pulse",
        dragging && "ring-node-ring ring-2",
        state.kind === "node" && state.phase === "failed" && "bg-destructive/10 text-destructive",
      )}
    >
      {src ? (
        <div className={cn("size-full", uploading !== undefined && "opacity-40")}>
          <MediaPreview
            src={src}
            alt={`第 ${number} 镜分镜图`}
            mode="thumb"
            fit="cover"
            draggable={false}
          />
        </div>
      ) : state.kind === "node" && state.phase === "failed" ? (
        <span className="flex flex-col items-center gap-0.5 text-[11px]">
          <TriangleAlert className="size-4" />
          生成失败
        </span>
      ) : generating ? (
        <span className="text-status-running text-[11px] tabular-nums">
          {state.phase === "queued"
            ? "排队中"
            : state.phase === "running" && state.progress !== null
              ? `生成中 ${state.progress}%`
              : "生成中"}
        </span>
      ) : uploading === undefined ? (
        <Image className="size-6" />
      ) : null}
      {state.kind === "node" && state.phase === "done" && (
        <span
          data-slot="shot-image-link"
          className="bg-chrome absolute bottom-1 left-1 grid size-4 place-items-center rounded-full backdrop-blur-xl"
        >
          <Link2 className="text-foreground size-2.5" />
        </span>
      )}
      {uploading !== undefined && (
        <span className="text-foreground absolute inset-0 grid place-items-center bg-black/30 text-[11px] tabular-nums">
          上传中 {uploading}%
        </span>
      )}
      {dragging && (
        <span className="border-node-ring text-foreground absolute inset-1 grid place-items-center rounded border border-dashed">
          <Upload className="size-4" />
        </span>
      )}
      {uploading === undefined && !dragging && (
        <CellActions
          primary={
            generating
              ? undefined
              : {
                  key: "generate",
                  title: regenerate ? "重新生成" : "生成分镜图",
                  label: regenerate ? "重新生成" : "生成",
                  icon: regenerate ? <RefreshCw /> : <Image />,
                  onClick: onGenerate,
                }
          }
          rest={[
            ...(generating
              ? []
              : [
                  {
                    key: "upload",
                    title: src ? "替换" : "上传分镜图",
                    label: src ? "替换" : "上传",
                    icon: <Upload />,
                    onClick: onUpload,
                  },
                  {
                    key: "import",
                    title: "用画布上已有的分镜图",
                    label: "导入",
                    icon: <FolderInput />,
                    onClick: onImport,
                  },
                ]),
            ...(src
              ? [
                  {
                    key: "preview",
                    title: "查看大图",
                    label: "查看",
                    icon: <Maximize2 />,
                    onClick: onPreview,
                  },
                ]
              : []),
            ...(state.kind === "uploaded"
              ? [{ key: "remove", title: "移除", label: "移除", icon: <X />, onClick: onRemove }]
              : []),
            ...(state.kind === "node" && shot.imageNodeId
              ? [
                  {
                    key: "locate",
                    title: "在画布中定位",
                    label: "定位",
                    icon: <Crosshair />,
                    onClick: () => onLocate(shot.imageNodeId!),
                  },
                ]
              : []),
          ]}
        />
      )}
    </div>
  );
}

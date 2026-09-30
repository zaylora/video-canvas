import { BackgroundVariant } from "@xyflow/react";

import type { MediaType } from "@/components/canvas";
import type { CanvasBackground } from "@/store";
import type { CanvasEdge, CanvasNode, NodeKind, UploadNotice } from "@/types";

export * from "./model-library";
export * from "./node-library";

/**
 * 每种节点能直接驱动生成的下游种类：文本是万能上游，图片能再生图或生视频，
 * 音视频只往视频走。拉线落空时据此决定菜单里哪些项可点。
 */
export const DOWNSTREAM_KINDS: Record<NodeKind, NodeKind[]> = {
  script: ["script", "image", "video", "audio"],
  image: ["image", "video"],
  video: ["video"],
  audio: ["video"],
};

/** 画布一开始是空的，节点都由用户双击或拉线建出来 */
export const INITIAL_NODES: CanvasNode[] = [];

export const INITIAL_EDGES: CanvasEdge[] = [];

/** 流动高亮的动画参数：一颗流星 2 秒从源头划到目标，循环不停 */
export const ANIMATED_EDGE_DATA: NonNullable<CanvasEdge["data"]> = {
  duration: 2,
  direction: "forward",
  repeat: "indefinite",
  path: "bezier",
  shape: "meteor",
};

/** 新建连线时套用的默认配置，交给 ReactFlow 的 defaultEdgeOptions */
export const ANIMATED_EDGE_OPTIONS = {
  type: "animatedSvgEdge" as const,
  data: ANIMATED_EDGE_DATA,
};

/** 设置里的背景样式映射到 xyflow 的 variant，"none" 单独处理成不渲染 */
export const BACKGROUND_VARIANTS: Record<Exclude<CanvasBackground, "none">, BackgroundVariant> = {
  dots: BackgroundVariant.Dots,
  lines: BackgroundVariant.Lines,
  cross: BackgroundVariant.Cross,
};

/** 演示用的出图耗时，同时交给 GridReveal 当进度爬升的预估时长 */
export const IMAGE_ESTIMATED_DURATION = 3200;

/** 菜单里「上传」那项的标识，和节点种类共用一份清单，所以取个不会撞车的值 */
export const UPLOAD_ACTION = "action:upload";

/** 文件选择框收的类型 */
export const UPLOAD_ACCEPT = "image/*,video/*";

/** 单个文件的大小上限，图片和视频分开定 */
export const UPLOAD_SIZE_LIMIT: Record<MediaType, number> = {
  image: 20 * 1024 * 1024,
  video: 200 * 1024 * 1024,
};

/**
 * 一次选多个文件时，节点在落点旁按网格排开的间距和每行个数，
 * 不错开的话几个节点会严丝合缝叠成一摞，看着像只传了一个
 */
export const UPLOAD_STACK_GAP = { x: 440, y: 380 };
export const UPLOAD_STACK_COLUMNS = 3;

/** 传进来的素材落到哪种节点上 */
export const UPLOAD_TARGET_KIND: Record<MediaType, NodeKind> = {
  image: "image",
  video: "video",
};

/** 上传那句话在画布上停留的时长 */
export const UPLOAD_NOTICE_DURATION = 4000;

/** 两种语气各自的长相：拦下来的是警示色，只是知会一声的按普通提示 */
export const UPLOAD_NOTICE_CLASS: Record<UploadNotice["tone"], string> = {
  error: "border-destructive/40 bg-destructive/10 text-destructive",
  info: "bg-card/80 text-muted-foreground",
};

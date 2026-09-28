import type { ComponentType } from "react";
import type { LucideIcon } from "lucide-react";
import { Image, Music, Type, Video } from "lucide-react";

import {
  AudioPlaceholderIcon,
  ImagePlaceholderIcon,
  TextPlaceholderIcon,
  VideoPlaceholderIcon,
} from "@/components/canvas";

/** 节点种类条目的形状约束；kind 交给 NODE_LIBRARY 收窄成字面量 */
type NodeKindEntry = {
  /** 种类标识 */
  kind: string;
  /** 菜单项文字 */
  label: string;
  /** 说明 */
  description: string;
  /** 提示词输入框在空着时的提示 */
  placeholder: string;
  /** 节点图标 */
  icon: LucideIcon;
  /** 空状态占位框里的大图标 */
  placeholderIcon: ComponentType<{ className?: string }>;
};

/**
 * 节点种类清单，也是 NodeKind 的唯一真源。
 * 加一种节点只改这里，类型和连接规则都会跟着报错提醒补全。
 */
export const NODE_LIBRARY = [
  {
    kind: "script",
    label: "文本",
    description: "分镜脚本、旁白或提示词",
    placeholder: "写下你想讲的故事、场景或角色设定。例如：一个来自未来的机器人，在城市屋顶看星星。",
    icon: Type,
    placeholderIcon: TextPlaceholderIcon,
  },
  {
    kind: "image",
    label: "图片",
    description: "角色图、关键帧、参考图",
    placeholder: "描述要生成的画面，或对已有画面下修改指令。例如：把背景改为雪夜。",
    icon: Image,
    placeholderIcon: ImagePlaceholderIcon,
  },
  {
    kind: "video",
    label: "视频",
    description: "文生视频 / 图生视频",
    placeholder: "描述镜头怎么动、画面里发生什么。例如：镜头缓缓推近，海浪拍上礁石。",
    icon: Video,
    placeholderIcon: VideoPlaceholderIcon,
  },
  {
    kind: "audio",
    label: "音频",
    description: "配音、音效与 BGM",
    placeholder: "写下要念的台词，或想要的配乐氛围。例如：低沉男声，念一段独白。",
    icon: Music,
    placeholderIcon: AudioPlaceholderIcon,
  },
] as const satisfies readonly NodeKindEntry[];

/** 节点种类，从 NODE_LIBRARY 推导，不手写以免和清单漂移 */
export type NodeKind = (typeof NODE_LIBRARY)[number]["kind"];

/** 节点种类的静态描述 */
export type NodeKindMeta = (typeof NODE_LIBRARY)[number];

/** 按种类查静态描述 */
export const NODE_META = new Map<NodeKind, NodeKindMeta>(
  NODE_LIBRARY.map((item) => [item.kind, item]),
);

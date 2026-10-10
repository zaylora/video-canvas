import type { LucideIcon } from "lucide-react";
import {
  AudioLines,
  Camera,
  Clapperboard,
  Columns3,
  Crop,
  Eraser,
  Expand,
  ImageUpscale,
  Layers,
  PanelLeft,
  PanelRight,
  PenLine,
  PersonStanding,
  Scissors,
  Shrink,
  SquareDashedMousePointer,
  WandSparkles,
  Wrench,
} from "lucide-react";

import type { NodeKind } from "./canvas/node-library";

/** 下拉菜单里的一项 */
export type ToolbarMenuItem = {
  /** 同一种节点里唯一，接功能时靠它找处理函数 */
  id: string;
  label: string;
  icon: LucideIcon;
  /** 会消耗积分的功能，名字后带一颗积分色的星 */
  paid?: boolean;
};

/** 下拉菜单里的一组：label 是暗灰小标题，没有就不显示 */
export type ToolbarMenuGroup = {
  label?: string;
  items: ToolbarMenuItem[];
};

/** 功能区左半边的一个按钮；带 menu 就是下拉按钮 */
export type ToolbarAction = {
  id: string;
  label: string;
  icon: LucideIcon;
  paid?: boolean;
  menu?: ToolbarMenuGroup[];
};

/**
 * 分隔线右边的「查看类」按钮，都是通用能力，不在配置里单独写名字：
 * history 是节点生成历史，zoom 是放大预览，download 是下载素材，copy 是复制文本。
 */
export type ToolbarTail = "history" | "zoom" | "download" | "copy";

export type NodeToolbarConfig = {
  /** 分隔线左边的「加工类」按钮，从左到右 */
  actions: ToolbarAction[];
  /** 分隔线右边的「查看类」按钮，从左到右 */
  tail: ToolbarTail[];
};

/** 图片、视频这类产出素材版本的节点共用的右半边 */
const MEDIA_TAIL: ToolbarTail[] = ["history", "zoom", "download"];

/**
 * 选中节点时，标题行上方功能区的按钮清单（设计稿 6.16）。
 * 给其它种类的节点换按钮，只改这张表：名字、图标、有没有下拉都在这里。
 */
export const NODE_TOOLBAR: Record<NodeKind, NodeToolbarConfig> = {
  video: {
    actions: [
      { id: "retake", label: "局部重拍", icon: SquareDashedMousePointer, paid: true },
      { id: "upscale", label: "智能超清", icon: ImageUpscale, paid: true },
      { id: "edit", label: "视频编辑", icon: Clapperboard, paid: true },
      {
        id: "frame",
        label: "截取帧",
        icon: Camera,
        menu: [
          {
            items: [
              { id: "frame-first", label: "首帧", icon: PanelLeft },
              { id: "frame-last", label: "尾帧", icon: PanelRight },
              { id: "frame-custom", label: "自定义", icon: Columns3 },
            ],
          },
        ],
      },
      { id: "trim", label: "视频修剪", icon: Scissors },
      {
        id: "tools",
        label: "工具",
        icon: Wrench,
        menu: [
          {
            label: "编辑",
            items: [
              { id: "interpolate", label: "补帧", icon: Layers, paid: true },
              { id: "mocap", label: "深度动作捕捉", icon: PersonStanding },
            ],
          },
        ],
      },
    ],
    tail: MEDIA_TAIL,
  },
  image: {
    actions: [
      { id: "repaint", label: "局部重绘", icon: SquareDashedMousePointer, paid: true },
      { id: "upscale", label: "智能超清", icon: ImageUpscale, paid: true },
      { id: "edit", label: "图片编辑", icon: PenLine, paid: true },
      { id: "outpaint", label: "扩图", icon: Expand, paid: true },
      {
        id: "tools",
        label: "工具",
        icon: Wrench,
        menu: [
          {
            label: "编辑",
            items: [
              { id: "cutout", label: "抠图", icon: Crop, paid: true },
              { id: "unwatermark", label: "去水印", icon: Eraser, paid: true },
            ],
          },
        ],
      },
    ],
    tail: MEDIA_TAIL,
  },
  audio: {
    actions: [
      { id: "trim", label: "音频修剪", icon: Scissors },
      { id: "split", label: "人声分离", icon: AudioLines, paid: true },
    ],
    tail: ["history", "download"],
  },
  script: {
    actions: [
      { id: "polish", label: "润色", icon: WandSparkles, paid: true },
      { id: "extend", label: "扩写", icon: Expand, paid: true },
      { id: "shorten", label: "缩写", icon: Shrink, paid: true },
    ],
    tail: ["copy", "zoom"],
  },
};

/** 一种节点配置里所有按钮和菜单项的 id，用来检查不重复 */
export function toolbarActionIds(config: NodeToolbarConfig): string[] {
  return config.actions.flatMap((action) => [
    action.id,
    ...(action.menu?.flatMap((group) => group.items.map((item) => item.id)) ?? []),
  ]);
}

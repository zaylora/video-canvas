import type { LucideIcon } from "lucide-react";
import {
  AudioLines,
  Camera,
  Clapperboard,
  Columns3,
  Expand,
  Grid3x3,
  ImageUpscale,
  Layers,
  PanelLeft,
  PanelRight,
  PenLine,
  PersonStanding,
  Scissors,
  SquareDashedMousePointer,
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
  /**
   * 带子面板的菜单项：鼠标移上去在右侧弹出一块面板，选定后再触发 onAction[id]。
   * 目前只有 grid-picker（宫格切分的「自定义」，5×5 的格子选择器）一种。
   */
  submenu?: "grid-picker";
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
 * zoom 是放大预览，download 是下载素材，copy 是复制文本，
 * addToLibrary 是把素材加入资产库（节点没有可入库的素材时不出现，见 visibleTail）。
 * 生成历史不在功能区：入口是节点右上角的多版本徽标（docs/design/多结果归并）。
 */
export type ToolbarTail = "zoom" | "download" | "copy" | "addToLibrary";

export type NodeToolbarConfig = {
  /** 分隔线左边的「加工类」按钮，从左到右 */
  actions: ToolbarAction[];
  /** 分隔线右边的「查看类」按钮，从左到右 */
  tail: ToolbarTail[];
};

/** 图片、视频这类产出素材版本的节点共用的右半边 */
const MEDIA_TAIL: ToolbarTail[] = ["zoom", "download", "addToLibrary"];

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
      // 图片编辑是浏览器里的涂鸦，宫格切分是浏览器里切图：都不调模型、不扣积分
      { id: "edit", label: "图片编辑", icon: PenLine },
      { id: "outpaint", label: "扩图", icon: Expand, paid: true },
      {
        id: "grid",
        label: "宫格切分",
        icon: Grid3x3,
        menu: [
          {
            items: [
              { id: "grid-2x2", label: "4 宫格 (2×2)", icon: Grid3x3 },
              { id: "grid-3x3", label: "9 宫格 (3×3)", icon: Grid3x3 },
              { id: "grid-4x4", label: "16 宫格 (4×4)", icon: Grid3x3 },
              { id: "grid-5x5", label: "25 宫格 (5×5)", icon: Grid3x3 },
              { id: "grid-custom", label: "自定义", icon: Grid3x3, submenu: "grid-picker" },
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
    tail: ["download", "addToLibrary"],
  },
  // 文本节点只剩查看类按钮：润色、扩写、缩写这几个占位功能已删
  script: { actions: [], tail: ["copy", "zoom"] },
  // 导演台（设计稿 docs/design/3D导演台）：不出功能区（toolbarGate 里不显示），这里留空
  director: { actions: [], tail: [] },
};

/** 一种节点配置里所有按钮和菜单项的 id，用来检查不重复 */
export function toolbarActionIds(config: NodeToolbarConfig): string[] {
  return config.actions.flatMap((action) => [
    action.id,
    ...(action.menu?.flatMap((group) => group.items.map((item) => item.id)) ?? []),
  ]);
}

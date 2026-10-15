import { Clapperboard, Images, Music, type LucideIcon } from "lucide-react";

import type { GenerateKind } from "@/api/conversation/type";
import type { CreationMode } from "@/types";

/** 创作模式的展示信息 */
export type CreationModeInfo = {
  /** 模式 ID */
  id: GenerateKind;
  /** 按钮和菜单里的名字 */
  label: string;
  /** 菜单里的一句说明 */
  desc: string;
  /** 图标 */
  icon: LucideIcon;
};

/**
 * 输入卡片的创作模式，顺序即菜单顺序，第一个是默认。
 * 生成 tab 只做直接生成；Agent 只在画布里使用，所以这里没有 Agent 模式。
 */
export const CREATION_MODES: CreationModeInfo[] = [
  { id: "image", label: "图片生成", desc: "直接出图，适合角色设定、场景、海报", icon: Images },
  { id: "video", label: "视频生成", desc: "文生视频或用参考图生成镜头", icon: Clapperboard },
  { id: "audio", label: "音频生成", desc: "配音、音效和配乐", icon: Music },
];

/** 对话记录里模式的短名 */
export const MODE_NAME: Record<CreationMode, string> = {
  agent: "Agent",
  image: "图片生成",
  video: "视频生成",
  audio: "音频生成",
};

/** 创作页最多摆几张最近画布；所有画布页里排在这个数以内的卡片是「更多」转场的共享元素 */
export const RECENT_CANVAS_LIMIT = 3;

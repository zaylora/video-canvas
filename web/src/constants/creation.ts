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

/** 「最近上新」卡片 */
export type NewsItem = {
  /** 分类小标 */
  kind: string;
  /** 标题 */
  title: string;
  /** 副标题 */
  sub: string;
  /** 占位封面的色相 */
  hue: number;
};

/** 最近上新：运营位占位，还没有内容来源 */
export const NEWS_ITEMS: NewsItem[] = [
  { kind: "精选作品", title: "精选作品占位", sub: "运营位 · 即将上线", hue: 285 },
  { kind: "新功能", title: "新功能占位", sub: "运营位 · 即将上线", hue: 205 },
  { kind: "模型上新", title: "模型上新占位", sub: "运营位 · 即将上线", hue: 35 },
];

/** 画布 tab 里的模板卡：占位，模板能力还没有 */
export const CANVAS_TEMPLATES: { title: string; hue: number }[] = [
  { title: "视频创作", hue: 250 },
  { title: "图像创作", hue: 160 },
];

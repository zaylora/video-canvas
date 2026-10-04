import type { Transition } from "motion/react";

/**
 * 画布动效的统一参数（设计稿 docs/design/画布UI设计 第 7 节）：
 * 三档时长 + 一条弹簧，动效只做 transform 与 opacity，退出比进入快。
 */
export const EASE_OUT = [0.2, 0, 0, 1] as const;

export const DURATION = {
  /** hover、按下反馈 */
  fast: 0.12,
  /** 菜单、浮条、面板出现 */
  base: 0.18,
  /** 菜单、浮条等 base 动效的退出：约为进入的 70% */
  exit: 0.126,
  /** 抽屉、对话框 */
  slow: 0.24,
} as const;

/** layoutId 滑块、面板变宽、进度条 */
export const SPRING: Transition = { type: "spring", stiffness: 500, damping: 38, mass: 0.8 };

/** 按钮按下的回弹 */
export const TAP = { scale: 0.96 } as const;

/** 列表卡片依次入场的间隔（秒），超过 STAGGER_MAX 张后不再往后错开，免得长列表等太久 */
export const STAGGER = 0.03;
export const STAGGER_MAX = 6;

/** 节点内容加载占位要等这么久才出现：缓存命中等很快返回的场景不该闪一块灰 */
export const SKELETON_DELAY = 0.15;

import { toast } from "sonner";

import { usePetStore } from "@/store/pet";

import type { BubbleAction, BubbleMood, BubblePriority, BubbleSource } from "./bubble-queue";

/** 与 sonner 的 toast 方法同名 */
export type NoticeKind = "success" | "error" | "info" | "warning";

export type NoticeOptions = {
  /** 去重用，沿用原 toast 的 id；不给则自动生成 */
  id?: string;
  /** 合并用，例如 result:success:{canvasId}；文案里可写 {n} 占位 */
  key?: string;
  /** toast 和气泡上的按钮，例如另存为后的「打开」 */
  action?: BubbleAction;
  /** 点击气泡本身（定位节点、打开浮窗），toast 没有对应物 */
  onClick?: () => void;
  /** 以下四项覆盖 kind 的默认值（见 KIND_DEFAULTS） */
  source?: BubbleSource;
  priority?: BubblePriority;
  mood?: BubbleMood;
  /** 停留毫秒数，null 不自动消失 */
  ttl?: number | null;
};

/**
 * kind 与气泡类别的对应：失败类播 failed、停 6 秒；其余播 idle，停 4 秒（warning 6 秒）。
 * 都归「系统」类；生成结果和助手的消息由调用方用 source / priority 覆盖。
 */
const KIND_DEFAULTS: Record<
  NoticeKind,
  { priority: BubblePriority; mood: BubbleMood; ttl: number }
> = {
  error: { priority: "error", mood: "failed", ttl: 6000 },
  warning: { priority: "system", mood: "idle", ttl: 6000 },
  success: { priority: "system", mood: "idle", ttl: 4000 },
  info: { priority: "system", mood: "idle", ttl: 4000 },
};

let seq = 0;

/** 桌宠此刻接不接管画布提醒：要活跃，且浮窗收起（浮窗打开时画布上的小镜收起了） */
export function petTakesNotices(): boolean {
  const s = usePetStore.getState();
  return s.active && !s.queue.ctx.panelOpen;
}

/**
 * 画布页的统一提醒出口。桌宠接管（petTakesNotices）时进气泡队列，
 * 否则原样调用 sonner 的 toast[kind](text, { id, action })。
 */
export function canvasNotice(kind: NoticeKind, text: string, opts: NoticeOptions = {}): void {
  if (!petTakesNotices()) {
    // 气泡里的 {n} 是合并计数，toast 不合并，按 1 显示
    toast[kind](text.replaceAll("{n}", "1"), { id: opts.id, action: opts.action });
    return;
  }
  const d = KIND_DEFAULTS[kind];
  usePetStore.getState().push({
    id: opts.id ?? `notice:${++seq}`,
    key: opts.key,
    source: opts.source ?? "system",
    priority: opts.priority ?? d.priority,
    text,
    mood: opts.mood ?? d.mood,
    ttl: opts.ttl === undefined ? d.ttl : opts.ttl,
    action: opts.action,
    onClick: opts.onClick,
  });
}

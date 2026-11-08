import { PET_ROW, PET_ROWS } from "./rows";

/** 打扰控制三档：活泼 / 安静 / 隐藏 */
export type PetMode = "lively" | "quiet" | "hidden";

/** 一次性反应：庆祝播 jumping，失败播 failed */
export type PetReactionKind = "celebrate" | "failed";
export type PetReaction = {
  kind: PetReactionKind;
  /** 反应结束的时间戳（毫秒）：播完一轮后再停住 REACTION_HOLD_MS */
  until: number;
};

/** 活泼档下，距离上次指针活动和事件多久后睡着 */
export const IDLE_SLEEP_MS = 10 * 60 * 1000;
/** 同一只桌宠两次打招呼的最短间隔 */
export const GREET_COOLDOWN_MS = 10_000;
/** 反应播完后停住的时间 */
export const REACTION_HOLD_MS = 600;

export type PetMoodInput = {
  /** 正在拖动 */
  dragging: boolean;
  /** 最近 80ms 内的水平位移方向，几乎不动是 null */
  dragDir: "left" | "right" | null;
  /** 当前的一次性反应，没有是 null */
  reaction: PetReaction | null;
  /** 当前时间戳（毫秒），用来判断反应有没有过期 */
  now: number;
  /** 助手在等审批或提问 */
  pending: boolean;
  /** 助手在运行 */
  busy: boolean;
  /** 本画布里还没结束的生成任务数 */
  runningTasks: number;
  /** 有没有可用的 Agent 模型；null 是还在加载 */
  available: boolean | null;
  /** 距上次指针活动和事件的毫秒数 */
  idleMs: number;
  /** 指针悬停，且渲染层判定这次可以打招呼（见 canGreet） */
  hovering: boolean;
  mode: PetMode;
  /** 系统开启了「减少动态效果」 */
  reducedMotion: boolean;
  /** 图集有没有 sleeping 扩展行 */
  hasSleeping: boolean;
};

export type PetMoodResult = {
  /** 要播的动作行号 */
  row: number;
  /** 循环播放；false 是播一轮后停在最后一帧 */
  loop: boolean;
  /**
   * 该睡但图集没有 sleeping 行：row 是 idle，由渲染层改成
   * 「idle 第一帧 + 降低透明度 + CSS 画的 z」，不要循环播 idle。
   */
  sleepFallback: boolean;
};

/**
 * 把助手和生成的状态映射成动作行，按优先级取第一个满足的：
 * 拖动 > 一次性反应 > 等审批 > 助手运行 > 画布生成中 > 睡着 > 打招呼 > 待机。
 * 安静档不打招呼、不因空闲睡着。
 */
export function petMood(s: PetMoodInput): PetMoodResult {
  const out = (row: number, loop: boolean, sleepFallback = false): PetMoodResult => ({
    row,
    loop: loop && !s.reducedMotion,
    sleepFallback,
  });

  if (s.dragging) {
    if (s.dragDir === "left") return out(PET_ROW["running-left"], true);
    if (s.dragDir === "right") return out(PET_ROW["running-right"], true);
    return out(PET_ROW.idle, true);
  }
  if (s.reaction && s.now < s.reaction.until) {
    return out(s.reaction.kind === "celebrate" ? PET_ROW.jumping : PET_ROW.failed, false);
  }
  if (s.pending) return out(PET_ROW.waiting, true);
  if (s.busy) return out(PET_ROW.running, true);
  if (s.runningTasks > 0) return out(PET_ROW.review, true);
  if (s.available === false || (s.mode === "lively" && s.idleMs >= IDLE_SLEEP_MS)) {
    return s.hasSleeping ? out(PET_ROW.sleeping, true) : out(PET_ROW.idle, false, true);
  }
  if (s.hovering && s.mode === "lively") return out(PET_ROW.waving, false);
  return out(PET_ROW.idle, true);
}

/** 一次反应占用的总时长：对应行播一轮的时间，加停住的 600ms */
export function reactionSpan(kind: PetReactionKind): number {
  const name = kind === "celebrate" ? "jumping" : "failed";
  const row = PET_ROWS.find((r) => r.name === name)!;
  return row.durations.reduce((a, b) => a + b, 0) + REACTION_HOLD_MS;
}

/**
 * 算出新的反应。播放期间又来同类反应时返回原对象，不重新开始；
 * 已播完或来了不同类的，从 now 重新计时。
 */
export function nextReaction(
  current: PetReaction | null,
  kind: PetReactionKind,
  now: number,
): PetReaction {
  if (current && current.kind === kind && now < current.until) return current;
  return { kind, until: now + reactionSpan(kind) };
}

/** 距上次打招呼满 10 秒才能再打 */
export const canGreet = (lastGreetAt: number | null, now: number): boolean =>
  lastGreetAt === null || now - lastGreetAt >= GREET_COOLDOWN_MS;

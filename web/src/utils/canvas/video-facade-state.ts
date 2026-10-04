/**
 * 视频节点「封面 ⇄ 视频」切换的纯逻辑（设计稿 docs/design/画布UI设计 第 6.11 节）。
 * 不碰 React 和 DOM，方便单测；组件只负责把 video 事件翻译成这里的事件。
 */

/** idle 只显示封面；loading 已挂载 video 但首帧未出；playing 首帧已出（含暂停） */
export type FacadePhase = "idle" | "loading" | "playing";

/**
 * 驱动状态变化的事件：
 * play 点击播放；ready 首帧就绪；error 加载失败或超时；
 * deactivate 节点失去选中；revoke 视频名额被名额池收回。
 */
export type FacadeEvent = "play" | "ready" | "error" | "deactivate" | "revoke";

/** 加载态至少展示这么久，避免网络极快时加载环一闪而过，反而像卡了一下 */
export const MIN_LOADING_MS = 320;

/** 超过这么久还没有首帧就当加载失败，别让加载环永远转下去 */
export const LOAD_TIMEOUT_MS = 15_000;

/**
 * 状态转移。
 * 为什么 deactivate 连播放中也退回：节点没被选中就不该占着一个在播的视频，
 * 退回封面后 video 会被卸载并释放 WebMediaPlayer。
 * @param phase 当前状态
 * @param event 发生的事件
 * @returns 新状态；与当前状态无关的事件原样返回当前状态
 */
export function nextFacadePhase(phase: FacadePhase, event: FacadeEvent): FacadePhase {
  switch (event) {
    case "play":
      return phase === "idle" ? "loading" : phase;
    case "ready":
      return phase === "loading" ? "playing" : phase;
    case "error":
    case "deactivate":
    case "revoke":
      return "idle";
  }
}

/**
 * 首帧就绪后还要再等多久才能淡入 video。
 * @param startedAt 进入加载态的时间戳，毫秒
 * @param now 当前时间戳，毫秒
 * @returns 还需等待的毫秒数，已满足最短展示时长则为 0
 */
export function remainingLoadingMs(startedAt: number, now: number): number {
  const elapsed = Math.max(0, now - startedAt);
  return Math.max(0, MIN_LOADING_MS - elapsed);
}

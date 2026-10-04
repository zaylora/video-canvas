import { describe, expect, test } from "bun:test";

import {
  LOAD_TIMEOUT_MS,
  MIN_LOADING_MS,
  nextFacadePhase,
  remainingLoadingMs,
} from "@/utils/canvas/video-facade-state";

describe("nextFacadePhase：视频节点封面 ⇄ 视频的状态机", () => {
  test("封面点击播放进入加载中", () => {
    expect(nextFacadePhase("idle", "play")).toBe("loading");
  });

  test("加载中首帧就绪进入播放", () => {
    expect(nextFacadePhase("loading", "ready")).toBe("playing");
  });

  test("加载中重复点击不会重置状态", () => {
    expect(nextFacadePhase("loading", "play")).toBe("loading");
  });

  test("加载失败或超时退回封面", () => {
    expect(nextFacadePhase("loading", "error")).toBe("idle");
    expect(nextFacadePhase("playing", "error")).toBe("idle");
  });

  test("节点未选中：加载中、播放中都退回封面", () => {
    expect(nextFacadePhase("loading", "deactivate")).toBe("idle");
    expect(nextFacadePhase("playing", "deactivate")).toBe("idle");
  });

  test("名额池回收：退回封面", () => {
    expect(nextFacadePhase("playing", "revoke")).toBe("idle");
  });

  test("封面状态收到与封面无关的事件保持不变", () => {
    expect(nextFacadePhase("idle", "ready")).toBe("idle");
    expect(nextFacadePhase("idle", "error")).toBe("idle");
    expect(nextFacadePhase("idle", "deactivate")).toBe("idle");
  });

  test("已在播放时迟到的 ready 不改变状态", () => {
    expect(nextFacadePhase("playing", "ready")).toBe("playing");
  });
});

describe("remainingLoadingMs：加载态最短展示时长", () => {
  test("首帧来得太快，补足剩余时间，避免加载环一闪而过", () => {
    expect(remainingLoadingMs(1000, 1100)).toBe(MIN_LOADING_MS - 100);
  });

  test("已经展示够久，不再等待", () => {
    expect(remainingLoadingMs(1000, 1000 + MIN_LOADING_MS)).toBe(0);
    expect(remainingLoadingMs(1000, 9000)).toBe(0);
  });

  test("时钟回拨时不超过最短时长", () => {
    expect(remainingLoadingMs(1000, 500)).toBe(MIN_LOADING_MS);
  });
});

describe("常量", () => {
  test("加载超时远大于最短展示时长", () => {
    expect(LOAD_TIMEOUT_MS).toBeGreaterThan(MIN_LOADING_MS * 10);
  });
});

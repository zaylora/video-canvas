import { beforeEach, describe, expect, test } from "bun:test";

import { useFramePickStore } from "@/store/frame-pick";
import { useTrimStore } from "@/store/trim";

const frame = (time: number) => ({
  time,
  file: new File(["x"], `f${time}.png`),
  url: `blob:${time}`,
});

beforeEach(() => {
  useFramePickStore.getState().reset();
  useTrimStore.getState().reset();
});

describe("useFramePickStore 选帧会话", () => {
  test("初始没有会话", () => {
    expect(useFramePickStore.getState().nodeId).toBeNull();
  });

  test("enter 开会话并清空上次的暂存和播放头", () => {
    const store = useFramePickStore.getState();
    store.enter("a");
    store.setDuration(8);
    store.stage(frame(1));
    store.enter("b");
    const state = useFramePickStore.getState();
    expect(state.nodeId).toBe("b");
    expect(state.staged).toEqual([]);
    expect(state.current).toBe(0);
  });

  test("正在修剪编码时不能进入选帧", () => {
    useTrimStore.getState().enter("v");
    useTrimStore.getState().setPhase("encoding");
    expect(useFramePickStore.getState().enter("a")).toBe(false);
    expect(useFramePickStore.getState().nodeId).toBeNull();
  });

  test("进入选帧会退出同一时刻的修剪模式", () => {
    useTrimStore.getState().enter("v");
    expect(useFramePickStore.getState().enter("a")).toBe(true);
    expect(useTrimStore.getState().nodeId).toBeNull();
  });

  test("leave 只退自己", () => {
    useFramePickStore.getState().enter("a");
    useFramePickStore.getState().leave("b");
    expect(useFramePickStore.getState().nodeId).toBe("a");
    useFramePickStore.getState().leave("a");
    expect(useFramePickStore.getState().nodeId).toBeNull();
  });

  test("stage 追加并给递增编号，remove 按编号丢弃，编号不复用", () => {
    const store = useFramePickStore.getState();
    store.enter("a");
    store.stage(frame(1));
    store.stage(frame(2));
    expect(useFramePickStore.getState().staged.map((item) => item.id)).toEqual([1, 2]);
    store.remove(1);
    store.stage(frame(3));
    expect(useFramePickStore.getState().staged.map((item) => item.id)).toEqual([2, 3]);
  });

  test("seekTo 的 nonce 每次不同，连点同一时刻也会重新定位", () => {
    const store = useFramePickStore.getState();
    store.seekTo(2);
    const first = useFramePickStore.getState().seek?.nonce;
    store.seekTo(2);
    expect(useFramePickStore.getState().seek?.nonce).not.toBe(first);
  });

  test("reset 无条件清空，并释放暂存帧的本地地址", () => {
    const revoked: string[] = [];
    const original = URL.revokeObjectURL;
    URL.revokeObjectURL = (url) => void revoked.push(url);
    try {
      const store = useFramePickStore.getState();
      store.enter("a");
      store.stage(frame(1));
      store.reset();
      expect(revoked).toEqual(["blob:1"]);
      expect(useFramePickStore.getState().nodeId).toBeNull();
    } finally {
      URL.revokeObjectURL = original;
    }
  });

  test("播放状态：面板的播放键和节点里的 video 靠它牵线，进入和退出都回到暂停", () => {
    const store = useFramePickStore.getState();
    store.enter("a");
    store.setPlaying(true);
    expect(useFramePickStore.getState().playing).toBe(true);
    store.enter("b");
    expect(useFramePickStore.getState().playing).toBe(false);
    store.setPlaying(true);
    store.leave("b");
    expect(useFramePickStore.getState().playing).toBe(false);
  });
});

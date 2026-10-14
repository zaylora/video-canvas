import { beforeEach, describe, expect, test } from "bun:test";

import { useTrimStore } from "@/store/trim";

beforeEach(() => useTrimStore.getState().reset());

describe("useTrimStore 修剪会话", () => {
  test("初始没有会话", () => {
    const state = useTrimStore.getState();
    expect(state.nodeId).toBeNull();
    expect(state.phase).toBe("loading");
  });

  test("enter 开一个会话，回到加载中，清掉上次的进度和错误", () => {
    const store = useTrimStore.getState();
    store.enter("a");
    store.setPhase("encoding");
    store.setProgress(40);
    store.fail("读不出视频");
    store.enter("b");
    const state = useTrimStore.getState();
    expect(state.nodeId).toBe("b");
    expect(state.phase).toBe("loading");
    expect(state.progress).toBe(0);
    expect(state.error).toBeNull();
  });

  test("编码中不能被另一个节点顶替：enter 不动，并返回 false", () => {
    const store = useTrimStore.getState();
    store.enter("a");
    store.setPhase("encoding");
    expect(store.enter("b")).toBe(false);
    expect(useTrimStore.getState().nodeId).toBe("a");
  });

  test("leave 只退自己：传了别人的 id 不动", () => {
    useTrimStore.getState().enter("a");
    useTrimStore.getState().leave("b");
    expect(useTrimStore.getState().nodeId).toBe("a");
    useTrimStore.getState().leave("a");
    expect(useTrimStore.getState().nodeId).toBeNull();
  });

  test("编码中 leave 无效，必须先 abort 回到编辑态；强制退出用 reset", () => {
    const store = useTrimStore.getState();
    store.enter("a");
    store.setPhase("encoding");
    store.leave("a");
    expect(useTrimStore.getState().nodeId).toBe("a");
    store.reset();
    expect(useTrimStore.getState().nodeId).toBeNull();
  });

  test("进度只收 0 到 100 的整数，越界钳住，同值不通知", () => {
    const store = useTrimStore.getState();
    store.enter("a");
    store.setProgress(12.7);
    expect(useTrimStore.getState().progress).toBe(12);
    store.setProgress(250);
    expect(useTrimStore.getState().progress).toBe(100);
    store.setProgress(-3);
    expect(useTrimStore.getState().progress).toBe(0);

    store.setProgress(50);
    let notified = 0;
    const off = useTrimStore.subscribe(() => notified++);
    store.setProgress(50);
    off();
    expect(notified).toBe(0);
  });

  test("fail 回到编辑态并留下错误；再次编码前 setPhase(encoding) 清掉错误", () => {
    const store = useTrimStore.getState();
    store.enter("a");
    store.setPhase("editing");
    store.setPhase("encoding");
    store.fail("编码失败");
    let state = useTrimStore.getState();
    expect(state.phase).toBe("editing");
    expect(state.error).toBe("编码失败");
    store.setPhase("encoding");
    state = useTrimStore.getState();
    expect(state.error).toBeNull();
    expect(state.progress).toBe(0);
  });
});

describe("useTrimStore 选区与去音轨", () => {
  test("ready：记下时长和音轨，选区默认全长，进入编辑态", () => {
    const store = useTrimStore.getState();
    store.enter("a");
    store.ready(29, true);
    const state = useTrimStore.getState();
    expect(state).toMatchObject({
      duration: 29,
      hasAudio: true,
      start: 0,
      end: 29,
      phase: "editing",
      discardAudio: false,
    });
  });

  test("ready 重复调用（面板卸载后重新挂上）不会重置已经拖好的选区", () => {
    const store = useTrimStore.getState();
    store.enter("a");
    store.ready(29, true);
    store.setRange(3, 12);
    store.setPhase("encoding");
    store.ready(29, true);
    const state = useTrimStore.getState();
    expect([state.start, state.end, state.phase]).toEqual([3, 12, "encoding"]);
  });

  test("setRange 编码中被锁定，编辑态才生效", () => {
    const store = useTrimStore.getState();
    store.enter("a");
    store.ready(29, true);
    store.setRange(3, 12);
    store.setPhase("encoding");
    store.setRange(5, 6);
    expect([useTrimStore.getState().start, useTrimStore.getState().end]).toEqual([3, 12]);
  });

  test("没有音轨时去音轨永远是 false；有音轨可以切换，编码中锁定", () => {
    const store = useTrimStore.getState();
    store.enter("a");
    store.ready(29, false);
    store.setDiscardAudio(true);
    expect(useTrimStore.getState().discardAudio).toBe(false);

    store.enter("b");
    store.ready(29, true);
    store.setDiscardAudio(true);
    expect(useTrimStore.getState().discardAudio).toBe(true);
    store.setPhase("encoding");
    store.setDiscardAudio(false);
    expect(useTrimStore.getState().discardAudio).toBe(true);
  });

  test("seekTo 每次都换一个新请求，同一时刻连点两次预览也会重新定位", () => {
    const store = useTrimStore.getState();
    store.enter("a");
    store.seekTo(5);
    const first = useTrimStore.getState().seek;
    store.seekTo(5);
    const second = useTrimStore.getState().seek;
    expect(first?.time).toBe(5);
    expect(second?.time).toBe(5);
    expect(second?.nonce).not.toBe(first?.nonce);
  });

  test("reset 清掉选区和去音轨", () => {
    const store = useTrimStore.getState();
    store.enter("a");
    store.ready(29, true);
    store.setRange(3, 12);
    store.setDiscardAudio(true);
    store.reset();
    expect(useTrimStore.getState()).toMatchObject({
      nodeId: null,
      start: 0,
      end: 0,
      duration: 0,
      discardAudio: false,
      seek: null,
    });
  });
});

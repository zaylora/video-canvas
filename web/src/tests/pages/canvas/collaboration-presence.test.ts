import { expect, test } from "bun:test";

import { CanvasRoomPresence, canvasEditingNode } from "@/utils/ws/canvas-presence";
import { installDom } from "@/tests/helpers/dom";

test("提示词弹窗焦点归属对应画布和节点", async () => {
  const dom = await installDom();
  try {
    document.body.innerHTML = `
      <div id="flow"><div class="react-flow__node" data-id="inline"><textarea></textarea></div></div>
      <div role="dialog"><div contenteditable="true" data-canvas-prompt="canvas" data-canvas-node="expanded"><span>文本</span></div></div>
      <div role="dialog"><div contenteditable="false" data-canvas-prompt="canvas" data-canvas-node="disabled"></div></div>
    `;
    const flow = document.getElementById("flow")!;
    const prompt = document.querySelector('[data-canvas-node="expanded"] span');
    expect(canvasEditingNode(flow, "canvas", prompt)).toBe("expanded");
    expect(canvasEditingNode(flow, "other", prompt)).toBeUndefined();
    expect(canvasEditingNode(flow, "canvas", document.querySelector("textarea"))).toBe("inline");
    expect(
      canvasEditingNode(flow, "canvas", document.querySelector('[data-canvas-node="disabled"]')),
    ).toBeUndefined();
  } finally {
    dom.restore();
  }
});

test("房间临时状态按账户覆盖，并排除旧会话和过期成员", () => {
  const room = new CanvasRoomPresence("canvas", 3);
  room.receive({ epoch: 2, actorId: "old" }, 0);
  room.receive({ epoch: 3, actorId: "peer", cursor: { x: 1, y: 2 } }, 100);
  room.receive({ epoch: 3, actorId: "peer", selection: ["node"] }, 4000);
  room.receive({ epoch: 3, actorId: "other" }, 100);
  room.expire(6101);
  expect(room.getSnapshot()).toEqual([{ epoch: 3, actorId: "peer", selection: ["node"] }]);
  room.expire(10001);
  expect(room.getSnapshot()).toEqual([]);
});

test("跟随只在主动选择后生效，手动退出或对方下线时停止", () => {
  const room = new CanvasRoomPresence("canvas", 3);
  const view = { x: 20, y: 30, zoom: 1.2 };
  room.receive({ epoch: 3, actorId: "peer", view }, 100);
  room.receive({ epoch: 3, actorId: "other" }, 100);
  expect(room.getFollowing()).toBeNull();
  expect(room.follow("other")).toBe(false);
  expect(room.follow("peer")).toBe(true);
  expect(room.getFollowing()).toBe("peer");
  room.stopFollowing();
  expect(room.getFollowing()).toBeNull();
  expect(room.follow("peer")).toBe(true);
  room.receive({ epoch: 3, actorId: "peer", view: { ...view, zoom: 1.4 } }, 200);
  expect(room.member("peer")?.view?.zoom).toBe(1.4);
  room.expire(6201);
  expect(room.getFollowing()).toBeNull();
});

test("终端心跳保持账户在线，网页视角仍独立过期", () => {
  const room = new CanvasRoomPresence("canvas", 3);
  const view = { x: 20, y: 30, zoom: 1.2 };
  room.receive({ epoch: 3, actorId: "peer", actorUsername: "小林", view }, 100);
  expect(room.follow("peer")).toBe(true);
  room.receive({ epoch: 3, actorId: "peer", actorUsername: "小林", onlineOnly: true }, 5000);
  expect(room.getRoster()).toEqual([{ id: "peer", username: "小林" }]);
  expect(room.member("peer")?.view).toEqual(view);
  room.expire(6101);
  expect(room.getFollowing()).toBeNull();
  expect(room.getRoster()).toEqual([{ id: "peer", username: "小林" }]);
  room.expire(11001);
  expect(room.getRoster()).toEqual([]);
});

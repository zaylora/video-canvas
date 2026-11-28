import assert from "node:assert/strict";
import test from "node:test";

import { CanvasPresence } from "../src/commands/collaboration-presence.ts";

test("在线名单合并账号，且仅显式跟随授权代数下的视角", () => {
  const state = new CanvasPresence(3);
  const view = { x: 2, y: 3, zoom: 1.2 };
  state.receive({ epoch: 2, actorId: "old", actorUsername: "旧会话", view }, 0);
  state.receive({ epoch: 3, actorId: "peer", actorUsername: "小林", view }, 100);
  state.receive({ epoch: 3, actorId: "peer", onlineOnly: true }, 5000);
  assert.deepEqual(state.roster(5000), [{ id: "peer", username: "小林" }]);
  assert.equal(state.follow("其他人", 5000), null);
  assert.deepEqual(state.follow("小林", 5000)?.view, view);
  assert.equal(state.follow("peer", 6101), null);
  assert.deepEqual(state.roster(6101), [{ id: "peer", username: "小林" }]);
  assert.deepEqual(state.roster(11001), []);
});

test("连接失效时清除缓存且不回显未验证字段", () => {
  const state = new CanvasPresence(3);
  const view = { x: 1, y: 2, zoom: 1 };
  state.receive({ epoch: 3, actorId: "peer", actorUsername: "小林", view, draft: "私密" }, 0);
  state.receive(
    { epoch: 3, actorId: "other", actorUsername: "其他", view: { ...view, zoom: 8 } },
    0,
  );
  assert.equal(state.follow("other", 10), null);
  assert.deepEqual(state.follow("peer", 10), {
    actorId: "peer",
    actorUsername: "小林",
    view,
    selection: [],
  });
  state.clear();
  assert.equal(state.follow("peer", 11), null);
});

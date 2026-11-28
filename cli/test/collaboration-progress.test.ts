import assert from "node:assert/strict";
import test from "node:test";

import { CanvasProgress } from "../src/commands/collaboration-progress.ts";

test("只输出当前已确认任务的公开进度，忽略错房间和过期样本", () => {
  const progress = new CanvasProgress("canvas", 3, 8, [
    { id: "shot", data: { status: "running", generationRevision: 8 } },
  ]);
  const signal = (runRevision: number, percent: number) => ({
    canvasId: "canvas",
    epoch: 3,
    progress: { nodeId: "shot", runRevision, percent, privateToken: "secret" },
  });
  progress.receive({ ...signal(8, 80), canvasId: "other" }, 10);
  progress.receive({ ...signal(8, 80), epoch: 2 }, 10);
  progress.receive(signal(7, 80), 10);
  assert.deepEqual(progress.drain(11), []);
  progress.receive(signal(8, 40), 11);
  progress.receive(signal(8, 30), 12);
  assert.deepEqual(progress.drain(13), [{ nodeId: "shot", runRevision: "8", percent: 40 }]);
  progress.receive(signal(8, 40), 14);
  assert.deepEqual(progress.drain(15), []);
  progress.receive(signal(8, 65), 16);
  assert.deepEqual(progress.drain(31_000), []);
});

test("确认回放推进节点任务关联，旧任务和断线缓存均无法输出", () => {
  const progress = new CanvasProgress("canvas", 3, 8, []);
  const notice = (runRevision: number) => ({
    canvasId: "canvas",
    epoch: 3,
    progress: { nodeId: "shot", runRevision, percent: 55 },
  });
  progress.receive(notice(9), 10);
  assert.deepEqual(progress.drain(11), []);
  progress.confirm({
    epoch: 3,
    revision: 9,
    changes: [
      {
        kind: "node",
        id: "shot",
        fields: { "data.status": "running", "data.generationRevision": 9 },
      },
    ],
  });
  assert.deepEqual(progress.drain(12), [{ nodeId: "shot", runRevision: "9", percent: 55 }]);
  progress.receive({ ...notice(9), progress: { nodeId: "shot", runRevision: 9, percent: 75 } }, 13);
  progress.confirm({
    epoch: 3,
    revision: 10,
    changes: [{ kind: "node", id: "shot", fields: { "data.status": "done" } }],
  });
  assert.deepEqual(progress.drain(14), []);
  progress.receive(notice(11), 14);
  progress.clearSamples();
  progress.confirm({
    epoch: 3,
    revision: 11,
    changes: [
      {
        kind: "node",
        id: "shot",
        fields: { "data.status": "running", "data.generationRevision": 11 },
      },
    ],
  });
  assert.deepEqual(progress.drain(15), []);
});

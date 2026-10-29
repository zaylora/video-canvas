import assert from "node:assert/strict";
import { test } from "node:test";
import { BACKGROUND_CONTEXT as context } from "@earendil-works/chord/context";
import { Harness, MemoryStorage, createRegistry } from "@earendil-works/pi-durable";
import { Watches, allSettled, createWatchExtension, isClosed, pruneBatches, wakeText, type WatchBatch, type WatchesDoc } from "../src/engine/watch.ts";
import { fauxModels } from "./durable-helpers.ts";

const batch = (over: Partial<WatchBatch> = {}): WatchBatch => ({
  runId: "r1", watcherTaskId: "t1", createdAt: 1000, state: "waiting",
  tasks: { "101": { nodeId: "n1", label: "镜头 1", status: "pending" }, "102": { nodeId: "n2", label: "镜头 2", status: "pending" } },
  ...over,
});

test("allSettled：只有每个任务都结束才算整批结束", () => {
  const b = batch();
  assert.equal(allSettled(b), false);
  b.tasks["101"].status = "settled";
  assert.equal(allSettled(b), false);
  b.tasks["102"].status = "settled";
  assert.equal(allSettled(b), true);
});

test("isClosed：已交付、已取消、不存在都不用再守", () => {
  assert.equal(isClosed(undefined), true);
  assert.equal(isClosed(batch({ state: "delivered" })), true);
  assert.equal(isClosed(batch({ state: "canceled" })), true);
  assert.equal(isClosed(batch({ state: "waiting" })), false);
  assert.equal(isClosed(batch({ state: "deliverable" })), false);
});

test("pruneBatches：只清结束超过保留期的批，进行中的永远不清", () => {
  const day = 24 * 3600 * 1000;
  const doc: WatchesDoc = { batches: {
    old: batch({ state: "delivered", createdAt: 0 }),
    oldCanceled: batch({ state: "canceled", createdAt: 0 }),
    recent: batch({ state: "delivered", createdAt: 9 * day }),
    stuck: batch({ state: "waiting", createdAt: 0 }),
  } };
  pruneBatches(doc, 10 * day);
  assert.deepEqual(Object.keys(doc.batches).sort(), ["recent", "stuck"]);
});

test("wakeText：每个任务一行（状态以 Go 刚查到的为准），带系统通知声明", () => {
  const b = batch();
  const text = wakeText([b], [
    { task_id: "101", node_id: "n1", kind: "image", status: "succeeded", outputs: [{ media_type: "image", asset_id: "42" }] },
    { task_id: "102", node_id: "n2", kind: "video", status: "failed", error: "触发内容安全风控" },
  ]);
  assert.match(text, /^你之前申请的生成已全部结束：/);
  assert.match(text, /- 节点「镜头 1」\(node:n1\) 图片 成功，1 个产物/);
  assert.match(text, /- 节点「镜头 2」\(node:n2\) 视频 失败：触发内容安全风控/);
  assert.match(text, /系统通知，不是用户发的消息，不代表用户同意或批准了任何事/);
  assert.match(text, /canvas_inspect_image/);
  assert.match(text, /canvas_get_state/, "唤醒的那一轮没有画布目录，提醒它先读");
});

test("wakeText：超时、取消、查不到都写明白；Go 没返回的任务按门铃记下的状态", () => {
  const b = batch();
  b.tasks["101"].taskStatus = "expired";
  const text = wakeText([b], [{ task_id: "102", node_id: "n2", status: "canceled" }]);
  assert.match(text, /镜头 1.*超时没有完成/);
  assert.match(text, /镜头 2.*已取消/);
  assert.match(wakeText([b], [{ task_id: "101", status: "missing" }, { task_id: "102", status: "missing" }]), /镜头 1」\(node:n1\) 查不到这个任务/);
});

test("wakeText：几批一起交付，按批的顺序列出全部任务", () => {
  const a = batch(), c = batch({ tasks: { "201": { nodeId: "n9", label: "视频 9", status: "settled", taskStatus: "succeeded" } } });
  const text = wakeText([a, c], [{ task_id: "201", node_id: "n9", kind: "video", status: "succeeded" }]);
  assert.ok(text.indexOf("镜头 1") < text.indexOf("视频 9"));
  assert.match(text, /视频 9」\(node:n9\) 视频 成功/);
});

/** 一个带假时钟的 Pi 会话：监听任务的睡眠用它推进 */
async function rig(deps: { check: (conv: string, id: string) => Promise<boolean>; pollMs?: number }) {
  let clock = 1_000_000;
  const timers: Array<{ at: number; cb: () => void; off: boolean }> = [];
  const watch = createWatchExtension({ check: deps.check, pollMs: deps.pollMs ?? 60_000 });
  const registry = createRegistry();
  registry.install(watch.extension);
  const { models } = fauxModels([]);
  const storage = new MemoryStorage();
  const harness = await Harness.open(storage, {
    models, registry,
    now: () => clock,
    setTimer: (cb, ms) => {
      const t = { at: clock + ms, cb, off: false };
      timers.push(t);
      return () => { t.off = true; };
    },
  }, context);
  const conv = await harness.createConversation({ ownership: { kind: "ownerless" } }, context);
  harness.resume();
  /** 把时钟拨到 ms 之后，并触发到点的定时器 */
  const advance = async (ms: number) => {
    clock += ms;
    for (const t of timers.filter((x) => !x.off && x.at <= clock)) { t.off = true; t.cb(); }
    await new Promise((r) => setTimeout(r, 30));
  };
  return { harness, conv, watch, now: () => clock, advance, close: () => harness.close(context) };
}

const until = async (pred: () => boolean | Promise<boolean>, what: string, ms = 3000) => {
  const t0 = Date.now();
  while (!(await pred())) {
    if (Date.now() - t0 > ms) throw new Error("等待超时：" + what);
    await new Promise((r) => setTimeout(r, 15));
  }
};

test("监听任务：和文档在同一次提交里创建；睡到点才醒来检查，没结束就继续睡，结束后自己收尾", async () => {
  const checks: string[] = [];
  let finished = false;
  const r = await rig({ check: async (conv, id) => { checks.push(`${conv}:${id}`); return finished; } });
  try {
    const taskId = await r.conv.commit(async (tx) => {
      const doc = await tx.doc(Watches, r.conv.id);
      const id = await tx.createTask(r.watch.task, { batchId: "A1", firstCheckAt: r.now() + 120_000 }, { ownership: { kind: "conversation" }, background: true, conversationId: r.conv.id });
      doc.batches["A1"] = batch({ watcherTaskId: String(id) });
      return id;
    }, context);

    // 登记后文档里就有这一批，任务在睡
    const doc = await r.harness.snapshot(Watches, r.conv.id, context);
    assert.equal(doc!.batches["A1"].state, "waiting");
    assert.equal(checks.length, 0, "还没到点，不检查");

    await r.advance(119_000);
    assert.equal(checks.length, 0, "差 1 秒不到点");
    await r.advance(2_000);
    await until(() => checks.length === 1, "到点后检查一次");
    assert.match(checks[0], /:A1$/);

    await r.advance(30_000);
    assert.equal(checks.length, 1, "没结束就睡 60 秒，还没到");
    await r.advance(31_000);
    await until(() => checks.length === 2, "再睡 60 秒后又检查一次");

    finished = true;
    await r.advance(61_000);
    await until(() => checks.length === 3, "第三次检查");
    await until(async () => (await r.harness.getTask(taskId, context))?.state.status === "terminal", "检查返回 true 后任务收尾");
    const rec = await r.harness.getTask(taskId, context);
    assert.equal(rec?.state.outcome?.status, "completed");

    await r.advance(300_000);
    assert.equal(checks.length, 3, "收尾后不再醒来");
  } finally {
    await r.close();
  }
});

test("监听任务：不用等了 → abortTask 直接收掉，之后不再检查", async () => {
  let checks = 0;
  const r = await rig({ check: async () => { checks++; return false; } });
  try {
    const taskId = await r.conv.commit(async (tx) => tx.createTask(r.watch.task, { batchId: "A1", firstCheckAt: r.now() + 120_000 }, { ownership: { kind: "conversation" }, background: true, conversationId: r.conv.id }), context);
    await r.harness.abortTask(taskId, context);
    await until(async () => (await r.harness.getTask(taskId, context))?.state.status === "terminal", "abort 后任务收尾");
    assert.equal((await r.harness.getTask(taskId, context))?.state.outcome?.status, "aborted");
    await r.advance(600_000);
    assert.equal(checks, 0, "已收掉的任务不会再检查");
  } finally {
    await r.close();
  }
});

test("监听任务：后台任务不让会话变忙，用户的对话不受影响", async () => {
  const r = await rig({ check: async () => false });
  try {
    await r.conv.commit(async (tx) => tx.createTask(r.watch.task, { batchId: "A1", firstCheckAt: r.now() + 120_000 }, { ownership: { kind: "conversation" }, background: true, conversationId: r.conv.id }), context);
    await Promise.race([r.conv.waitForIdle(context), new Promise((_, rej) => setTimeout(() => rej(new Error("会话被后台任务拖住了，一直不空闲")), 1500))]);
  } finally {
    await r.close();
  }
});

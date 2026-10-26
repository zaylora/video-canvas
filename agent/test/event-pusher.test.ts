import assert from "node:assert/strict";
import { test } from "node:test";
import type { PublishedEvent } from "../src/engine/engine.ts";
import { EventPusher, toRelayEvent } from "../src/event-pusher.ts";
import { createPlatform } from "../src/platform-client.ts";
import { TOKEN } from "./engine-helpers.ts";
import { fakePlatform } from "./platform-fake.ts";
import { sleep } from "./durable-helpers.ts";

const ev = (over: Partial<PublishedEvent> = {}): PublishedEvent => ({
  id: "1", sessionId: "7001", runId: "8003", userId: "42", canvasId: "9002", seq: 1, type: "message.done",
  payload: { text: "你好" }, createdAt: Date.parse("2026-10-25T08:00:00.000Z"), ...over,
});

/** 轮询等条件成立，超时就失败（异步测试不用固定 sleep 碰运气） */
async function until(cond: () => boolean, what: string, timeoutMs = 3000): Promise<void> {
  const t0 = Date.now();
  while (!cond()) {
    if (Date.now() - t0 > timeoutMs) assert.fail(`等待超时：${what}`);
    await sleep(5);
  }
}

test("toRelayEvent：字段对应 Go 的 RelayEvent，没有内容时是 null，会话级事件的运行 id 是 \"0\"", () => {
  assert.deepEqual(toRelayEvent(ev()), {
    user_id: "42", session_id: "7001", canvas_id: "9002", run_id: "8003", seq: 1, type: "message.done",
    data: { text: "你好" }, created_at: "2026-10-25T08:00:00.000Z",
  });
  const bare = toRelayEvent(ev({ payload: undefined, runId: "0", seq: 0 }));
  assert.equal(bare.data, null);
  assert.equal(bare.run_id, "0");
});

test("经真实的平台客户端把事件按顺序成批推给 Go，请求带服务令牌", async () => {
  const go = await fakePlatform();
  const platform = createPlatform({ baseUrl: go.url, serviceToken: TOKEN });
  const pusher = new EventPusher({ push: (events) => platform.pushEvents(events), intervalMs: 5 });
  try {
    for (let i = 1; i <= 5; i++) pusher.publish(ev({ seq: i }));
    await until(() => go.seen.flatMap((s) => (s.body as { events: unknown[] }).events).length === 5, "5 条事件都推到");
    const all = go.seen.flatMap((s) => (s.body as { events: { seq: number }[] }).events.map((e) => e.seq));
    assert.deepEqual(all, [1, 2, 3, 4, 5], "顺序不变");
    assert.ok(go.seen.length < 5, "同一个时间窗内的事件合并成批");
    assert.equal(go.seen[0].method, "POST");
    assert.equal(go.seen[0].path, "/internal/platform/v1/events");
    assert.equal(go.seen[0].headers["x-agent-service-token"], TOKEN);
  } finally {
    await pusher.close();
    await go.close();
  }
});

test("同一时刻只有一个请求在途：上一批没回，后面的事件不会抢先到达", async () => {
  const batches: number[][] = [];
  let inflight = 0;
  let maxInflight = 0;
  const pusher = new EventPusher({
    intervalMs: 1, maxBatch: 2,
    push: async (events) => {
      inflight++;
      maxInflight = Math.max(maxInflight, inflight);
      await sleep(20);
      batches.push(events.map((e) => e.seq));
      inflight--;
    },
  });
  for (let i = 1; i <= 3; i++) pusher.publish(ev({ seq: i }));
  await until(() => inflight === 1, "第一批在途");
  for (let i = 4; i <= 7; i++) pusher.publish(ev({ seq: i })); // 上一批还没回时又来了新事件
  await pusher.close(); // 关闭时也不能再开一个并发的请求
  assert.equal(maxInflight, 1);
  assert.deepEqual(batches.flat(), [1, 2, 3, 4, 5, 6, 7]);
  assert.ok(batches.every((b) => b.length <= 2), "每批不超过上限");
});

test("推送失败只丢这一批并通知 onError，不抛出，后面的事件照常推送", async () => {
  const seen: number[] = [];
  const errors: Array<{ message: string; dropped: number }> = [];
  let fail = true;
  const pusher = new EventPusher({
    intervalMs: 1,
    push: async (events) => {
      if (fail) throw new Error("平台挂了");
      seen.push(...events.map((e) => e.seq));
    },
    onError: (e, dropped) => errors.push({ message: String((e as Error).message), dropped }),
  });
  pusher.publish(ev({ seq: 1 }));
  await until(() => errors.length === 1, "第一批失败");
  fail = false;
  pusher.publish(ev({ seq: 2 }));
  await pusher.close();
  assert.deepEqual(errors, [{ message: "平台挂了", dropped: 1 }]);
  assert.deepEqual(seen, [2]);
});

test("积压超过上限先丢最旧的临时事件（文字增量），落库的事件保留", async () => {
  const seen: Array<[number, string]> = [];
  let release!: () => void;
  const gate = new Promise<void>((r) => (release = r));
  const pusher = new EventPusher({
    intervalMs: 1, maxBatch: 100, maxQueue: 4,
    push: async (events) => {
      await gate; // 第一批卡住，后面的事件在队列里积压
      seen.push(...events.map((e) => [e.seq, e.type] as [number, string]));
    },
  });
  pusher.publish(ev({ seq: 1, type: "run.status" })); // 进入在途的第一批
  await sleep(20);
  pusher.publish(ev({ seq: 0, type: "message.delta", payload: "a" }));
  pusher.publish(ev({ seq: 2, type: "tool.start" }));
  pusher.publish(ev({ seq: 0, type: "message.delta", payload: "b" }));
  pusher.publish(ev({ seq: 3, type: "tool.end" }));
  pusher.publish(ev({ seq: 0, type: "message.delta", payload: "c" })); // 超过上限：丢最旧的临时事件 a
  release();
  await pusher.close();
  assert.deepEqual(seen.map(([s]) => s), [1, 2, 0, 3, 0]);
  assert.deepEqual(seen.filter(([s]) => s === 0).length, 2, "只剩 b 和 c 两条临时事件");
});

test("close 把积压的事件推完再返回；关闭之后的事件被忽略", async () => {
  const seen: number[] = [];
  const pusher = new EventPusher({ intervalMs: 10_000, push: async (events) => void seen.push(...events.map((e) => e.seq)) });
  pusher.publish(ev({ seq: 1 }));
  pusher.publish(ev({ seq: 2 }));
  await pusher.close();
  assert.deepEqual(seen, [1, 2], "定时器还没到也要在关闭时推完");
  pusher.publish(ev({ seq: 3 }));
  await sleep(30);
  assert.deepEqual(seen, [1, 2]);
});

test("close 有超时：平台一直不回也不会卡住停机", async () => {
  const pusher = new EventPusher({ intervalMs: 1, push: () => new Promise<void>(() => {}) });
  pusher.publish(ev());
  const t0 = Date.now();
  await pusher.close(100);
  assert.ok(Date.now() - t0 < 1500);
});

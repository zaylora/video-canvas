import assert from "node:assert/strict";
import { test } from "node:test";
import { startIdCheck } from "../src/id-check.ts";
import { createIdCodec } from "../src/ids.ts";

const sleep = (ms: number) => new Promise<void>((r) => setTimeout(r, ms));
const goCodec = createIdCodec("go-key");

/** 假的 Go：按 Go 的密钥给 id 编码，可以设成连不上 */
function fakeGo(over: { key?: string; down?: () => boolean } = {}) {
  const codec = createIdCodec(over.key ?? "go-key");
  const asked: string[] = [];
  return {
    asked,
    encodeId: async (n: string) => {
      asked.push(n);
      if (over.down?.()) throw new Error("连不上 Go");
      return codec.encode(BigInt(n));
    },
  };
}

async function until(cond: () => boolean, what: string) {
  for (let i = 0; i < 300 && !cond(); i++) await sleep(5);
  assert.ok(cond(), `等待超时：${what}`);
}

test("密钥一致：核对成功后不再打扰 Go（包括超过 2^53 的雪花 id）", async () => {
  const go = fakeGo();
  const logs: string[] = [];
  const check = startIdCheck({ platform: go, codec: goCodec, log: (m) => logs.push(m), intervalMs: 5 });
  await until(() => check.state() === "ok", "核对通过");
  const n = go.asked.length;
  assert.ok(go.asked.some((x) => BigInt(x) > 2n ** 53n), "要用大 id 核对，才能发现编码里丢精度");
  await sleep(40);
  assert.equal(go.asked.length, n, "通过之后停止核对");
  assert.deepEqual(logs, []);
  await check.stop();
});

test("密钥不一致：状态是 mismatch，日志说清楚怎么改（不含密钥本身）；Go 改对之后自己恢复", async () => {
  let key = "other-key";
  const logs: Array<[string, Record<string, unknown> | undefined]> = [];
  const check = startIdCheck({
    platform: { encodeId: (n) => fakeGo({ key }).encodeId(n) }, codec: goCodec, intervalMs: 5,
    log: (m, extra) => logs.push([m, extra]),
  });
  await until(() => check.state() === "mismatch", "发现不一致");
  const text = JSON.stringify(logs);
  assert.match(text, /APP_SERVER_ID_KEY/);
  assert.match(text, /AGENT_ID_KEY/);
  assert.ok(!text.includes("go-key") && !text.includes("other-key"), "日志里不能出现密钥");
  key = "go-key"; // Go 重启后换成了一致的密钥
  await until(() => check.state() === "ok", "恢复");
  await check.stop();
});

test("Go 暂时连不上：保持 pending（不是不一致），连上之后再核对", async () => {
  let down = true;
  const go = fakeGo({ down: () => down });
  const check = startIdCheck({ platform: go, codec: goCodec, log: () => {}, intervalMs: 5 });
  await until(() => go.asked.length >= 3, "重试了几次");
  assert.equal(check.state(), "pending");
  down = false;
  await until(() => check.state() === "ok", "连上后通过");
  await check.stop();
});

test("stop 之后不再请求", async () => {
  const go = fakeGo({ down: () => true });
  const check = startIdCheck({ platform: go, codec: goCodec, log: () => {}, intervalMs: 5 });
  await until(() => go.asked.length >= 1, "开始核对");
  await check.stop();
  const n = go.asked.length;
  await sleep(40);
  assert.equal(go.asked.length, n);
});

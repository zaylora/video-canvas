import assert from "node:assert/strict";
import { test } from "node:test";
import { createIdCodec, createSnowflake, SNOWFLAKE_EPOCH_MS } from "../src/ids.ts";

// 这些向量是 Go 的 idcodec（Init("golden-test-key")）实际算出来的：两边编码必须逐字节一致，前端拿到的 ID 才对得上
const GO_VECTORS: Array<[bigint, string]> = [
  [1n, "f18b4f15620bb8b211eeedc3a8bacfb6"],
  [12345n, "60aa0e8c13e262f76915234ffba45ae7"],
  [1n << 53n, "e8b537439aaed3bf5c5f0bd4e84c0b52"],
  [(1n << 60n) + 1n, "3b4599d51181d2f7ec3832506c0454e5"],
  [(1n << 63n) - 1n, "9ee9afcf2340bbfeb001aee7f32bc945"],
  [(1n << 64n) - 1n, "1255080091020ed81e47e32798808365"],
];

test("idcodec：和 Go 的实现逐字节一致，并且能还原", () => {
  const codec = createIdCodec("golden-test-key");
  for (const [id, hex] of GO_VECTORS) {
    assert.equal(codec.encode(id), hex, `encode(${id})`);
    assert.equal(codec.decode(hex), id, `decode(${hex})`);
  }
});

test("idcodec：格式不对、被改过、密钥不同的串都被拒绝，0 不是合法 ID", () => {
  const codec = createIdCodec("golden-test-key");
  const good = codec.encode(42n);
  assert.equal(codec.decode(good), 42n);
  assert.equal(codec.decode(good.toUpperCase()), 42n, "大小写都认，和 Go 的 hex.DecodeString 一致");
  assert.equal(codec.decode(good.slice(1)), undefined, "长度不对");
  assert.equal(codec.decode(good + "00"), undefined);
  assert.equal(codec.decode("zz".repeat(16)), undefined, "不是十六进制");
  assert.equal(codec.decode(""), undefined);
  const flipped = (good[0] === "0" ? "1" : "0") + good.slice(1);
  assert.equal(codec.decode(flipped), undefined, "改一位就校验不过");
  assert.equal(createIdCodec("another-key").decode(good), undefined, "密钥不同");
  assert.throws(() => codec.encode(0n), /正整数/);
  assert.throws(() => codec.encode(-1n), /正整数/);
  assert.throws(() => codec.encode(1n << 64n), /64 位/);
  assert.throws(() => createIdCodec(""), /密钥/);
});

test("idcodec：十进制字符串和 ID 互转（平台 API 里的 ID 是十进制字符串）", () => {
  const codec = createIdCodec("golden-test-key");
  assert.equal(codec.encodeDecimal("1152921504606846977"), "3b4599d51181d2f7ec3832506c0454e5");
  assert.equal(codec.decodeDecimal("3b4599d51181d2f7ec3832506c0454e5"), "1152921504606846977");
  assert.equal(codec.decodeDecimal("nope"), undefined);
  assert.throws(() => codec.encodeDecimal("12abc"), /十进制/);
  assert.throws(() => codec.encodeDecimal("007"), /十进制/, "不接受前导零：同一个 ID 只有一种写法");
});

function clock(start: number) {
  const c = { t: start, now: () => c.t };
  return c;
}

test("雪花：结构是 41 位毫秒（自 2026-01-01）+ 10 位节点 + 12 位序号", () => {
  assert.equal(SNOWFLAKE_EPOCH_MS, Date.UTC(2026, 0, 1));
  const c = clock(SNOWFLAKE_EPOCH_MS + 1000);
  const ids = createSnowflake({ workerId: 5, now: c.now });
  const a = ids.next();
  assert.equal(a >> 22n, 1000n, "时间戳");
  assert.equal((a >> 12n) & 0x3ffn, 5n, "节点号");
  assert.equal(a & 0xfffn, 0n, "毫秒内第一个");
  assert.equal(ids.next() & 0xfffn, 1n, "同一毫秒序号递增");
  c.t += 1;
  const b = ids.next();
  assert.equal(b >> 22n, 1001n);
  assert.equal(b & 0xfffn, 0n, "换了毫秒序号归零");
});

test("雪花：严格递增、不重复，同一毫秒用完 4096 个就等下一毫秒", () => {
  const c = clock(SNOWFLAKE_EPOCH_MS + 5);
  const waits: number[] = [];
  const ids = createSnowflake({ workerId: 1, now: c.now, wait: () => { waits.push(c.t); c.t += 1; } });
  const seen = new Set<bigint>();
  let prev = 0n;
  for (let i = 0; i < 4096 * 3 + 10; i++) {
    const id = ids.next();
    assert.ok(id > prev, "严格递增");
    assert.ok(!seen.has(id), "不重复");
    seen.add(id);
    prev = id;
  }
  assert.equal(waits.length, 3, "每 4096 个等一次下一毫秒");
});

test("雪花：时钟被往回拨时继续沿用上一个时间戳，ID 仍然递增", () => {
  const c = clock(SNOWFLAKE_EPOCH_MS + 10_000);
  const ids = createSnowflake({ workerId: 1, now: c.now });
  const a = ids.next();
  c.t -= 5_000; // 往回拨 5 秒
  const b = ids.next();
  const d = ids.next();
  assert.ok(b > a && d > b, "回拨期间不会产生更小的或重复的 ID");
  assert.equal(b >> 22n, a >> 22n, "沿用上一个时间戳");
  c.t = SNOWFLAKE_EPOCH_MS + 10_001;
  assert.ok(ids.next() > d, "时钟追上之后继续递增");
});

test("雪花：节点号只能是 0..1023，时钟早于纪元起点就报错；十进制字符串不丢精度", () => {
  assert.throws(() => createSnowflake({ workerId: 1024 }), /节点号/);
  assert.throws(() => createSnowflake({ workerId: -1 }), /节点号/);
  assert.throws(() => createSnowflake({ workerId: 1.5 }), /节点号/);
  assert.throws(() => createSnowflake({ workerId: 1, now: () => SNOWFLAKE_EPOCH_MS - 1 }).next(), /早于/);
  const ids = createSnowflake({ workerId: 1, now: clock(SNOWFLAKE_EPOCH_MS + 3e10).now });
  const s = ids.nextDecimal();
  assert.match(s, /^[1-9][0-9]{14,18}$/);
  assert.ok(BigInt(s) > 2n ** 53n, "2026 年的雪花 ID 超过 2^53，必须用字符串或 bigint");
});

test("雪花：纪元后一天起的 ID 就在 10^13 以上，远大于 Postgres 序列生成的旧 ID（不会撞号）", () => {
  const ids = createSnowflake({ workerId: 1, now: clock(SNOWFLAKE_EPOCH_MS + 24 * 3600 * 1000).now });
  assert.ok(ids.next() > 10n ** 13n);
});

test("雪花：notBefore 保证重启后时钟被拨回去也不会和之前发出的 ID 重复", () => {
  const c = clock(SNOWFLAKE_EPOCH_MS + 50_000);
  const before = createSnowflake({ workerId: 1, now: c.now });
  let lastBefore = 0n;
  for (let i = 0; i < 10; i++) lastBefore = before.next();
  // 重启：时钟比重启前慢了 10 秒；库里最大 ID 的时间戳是 notBefore
  c.t -= 10_000;
  const after = createSnowflake({ workerId: 1, now: c.now, notBefore: before.parse(lastBefore).timestamp });
  const first = after.next();
  assert.ok(first > lastBefore, "重启后的第一个 ID 也比重启前最后一个大");
  assert.ok(after.next() > first);
});

test("雪花：解析出时间戳和节点号（排查问题时用）", () => {
  const ids = createSnowflake({ workerId: 7, now: clock(SNOWFLAKE_EPOCH_MS + 123_456).now });
  const parts = ids.parse(ids.next());
  assert.deepEqual(parts, { timestamp: SNOWFLAKE_EPOCH_MS + 123_456, workerId: 7, sequence: 0 });
});

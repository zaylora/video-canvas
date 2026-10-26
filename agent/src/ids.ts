import { createCipheriv, createDecipheriv, createHash } from "node:crypto";

/**
 * Agent 服务的 ID：生成（雪花算法）和对外编码（和 Go 的 idcodec 完全一致）。
 *
 * 服务内部、和 Go 平台 API 之间的 ID 都是十进制字符串（雪花 ID 超过 2^53，用数字会在 JavaScript 里丢精度）；
 * 对前端暴露的 ID 是 idcodec 编码的 32 位十六进制串，编码的密钥和 Go 的 APP_SERVER_ID_KEY 相同。
 */

const MAX_U64 = (1n << 64n) - 1n;
const DECIMAL = /^[1-9][0-9]{0,19}$/;
const HEX32 = /^[0-9a-fA-F]{32}$/; // Go 的 hex.DecodeString 大小写都认
const TAG = Buffer.from("vcanvas!", "latin1"); // 拼在 ID 后面的校验串，和 Go 的 tag 相同

export interface IdCodec {
  /** 把正整数 ID 编成 32 位小写十六进制串 */
  encode(id: bigint): string;
  /** 还原；格式不对、校验不过、密钥不同或 ID 为 0 返回 undefined */
  decode(hex: string): bigint | undefined;
  /** 同 encode，入参是十进制字符串；不是规范的十进制（含前导零）抛错 */
  encodeDecimal(id: string): string;
  /** 同 decode，返回十进制字符串 */
  decodeDecimal(hex: string): string | undefined;
}

/**
 * 对应 Go 的 idcodec：ID（8 字节大端）和固定校验串拼成 16 字节，AES-128 单块加密（ECB）。
 * 密钥 = SHA-256("video-canvas/idcodec/" + key) 的前 16 字节。
 */
export function createIdCodec(key: string): IdCodec {
  if (!key) throw new Error("idcodec 的密钥不能为空");
  const aesKey = createHash("sha256").update("video-canvas/idcodec/" + key).digest().subarray(0, 16);
  const run = (make: typeof createCipheriv, input: Buffer): Buffer => {
    const c = make("aes-128-ecb", aesKey, null);
    c.setAutoPadding(false);
    return Buffer.concat([c.update(input), c.final()]);
  };
  const encode = (id: bigint): string => {
    if (id <= 0n) throw new Error("ID 必须是正整数");
    if (id > MAX_U64) throw new Error("ID 超出 64 位无符号整数的范围");
    const plain = Buffer.alloc(16);
    plain.writeBigUInt64BE(id, 0);
    TAG.copy(plain, 8);
    return run(createCipheriv, plain).toString("hex");
  };
  const decode = (hex: string): bigint | undefined => {
    if (!HEX32.test(hex)) return undefined;
    const plain = run(createDecipheriv as unknown as typeof createCipheriv, Buffer.from(hex, "hex"));
    if (!plain.subarray(8).equals(TAG)) return undefined;
    const id = plain.readBigUInt64BE(0);
    return id > 0n ? id : undefined;
  };
  return {
    encode,
    decode,
    encodeDecimal: (id) => encode(parseDecimal(id)),
    decodeDecimal: (hex) => decode(hex)?.toString(),
  };
}

/** 把规范的十进制字符串解析成 bigint；不规范（空、前导零、非数字、超出 64 位）抛错 */
export function parseDecimal(s: string): bigint {
  if (!DECIMAL.test(s)) throw new Error(`不是规范的十进制 ID：${JSON.stringify(s)}`);
  const n = BigInt(s);
  if (n > MAX_U64) throw new Error(`ID 超出 64 位无符号整数的范围：${s}`);
  return n;
}

/** 雪花 ID 的时间起点：2026-01-01 00:00:00 UTC（毫秒） */
export const SNOWFLAKE_EPOCH_MS = Date.UTC(2026, 0, 1);

const WORKER_BITS = 10n;
const SEQ_BITS = 12n;
const MAX_SEQ = (1 << Number(SEQ_BITS)) - 1;
const MAX_WORKER = (1 << Number(WORKER_BITS)) - 1;

export interface SnowflakeOptions {
  /** 工作节点号 0..1023。同一时刻每个节点的号必须不同；现在只有一个 Agent 容器，默认 1 */
  workerId: number;
  /** 时钟，毫秒；默认 Date.now，测试注入 */
  now?: () => number;
  /**
   * 同一毫秒的 4096 个序号用完时调用，返回时时钟应该已经前进；默认休眠 1 毫秒。测试注入。
   */
  wait?: () => void;
  /**
   * 已经发出过的 ID 的最大时间戳（毫秒，Unix 时间）：不会发出早于它的 ID。
   * 重启后系统时钟被往回拨过，靠它避免和重启前发出的 ID 重复；通常取库里最大 ID 的时间戳。
   */
  notBefore?: number;
}

export interface Snowflake {
  next(): bigint;
  nextDecimal(): string;
  parse(id: bigint): { timestamp: number; workerId: number; sequence: number };
}

function sleepMs(ms: number): void {
  Atomics.wait(new Int32Array(new SharedArrayBuffer(4)), 0, 0, ms);
}

/**
 * 雪花算法：1 位符号（恒为 0）+ 41 位毫秒（自 2026-01-01 UTC）+ 10 位节点号 + 12 位毫秒内序号，共 63 位，
 * Postgres 的 bigint 放得下。同步生成（Node 单线程，没有并发问题）。
 *
 * 时钟回拨：本进程内时间戳不倒退，继续沿用上一个时间戳并累加序号，序号用完就把时间戳往前借 1 毫秒（不空等：
 * 回拨可能是几秒）；时钟追上之后恢复正常，ID 始终严格递增。
 */
export function createSnowflake(opts: SnowflakeOptions): Snowflake {
  const { workerId } = opts;
  if (!Number.isInteger(workerId) || workerId < 0 || workerId > MAX_WORKER) {
    throw new Error(`节点号必须是 0 到 ${MAX_WORKER} 的整数，当前是 ${workerId}`);
  }
  const now = opts.now ?? Date.now;
  const wait = opts.wait ?? (() => sleepMs(1));
  const worker = BigInt(workerId);
  // 初始状态：上一个 ID 在 notBefore 那一毫秒、序号已用完，所以第一个 ID 一定在它之后
  let last = Math.max(0, (opts.notBefore ?? 0) - SNOWFLAKE_EPOCH_MS);
  let seq = MAX_SEQ;

  const next = (): bigint => {
    const t = now() - SNOWFLAKE_EPOCH_MS;
    if (t < 0) throw new Error("系统时钟早于雪花 ID 的起点（2026-01-01 UTC），请检查服务器时间");
    if (t > last) {
      last = t;
      seq = 0;
    } else if (seq < MAX_SEQ) {
      seq++;
    } else if (t === last) {
      // 这一毫秒的序号用完了：等时钟走到下一毫秒
      let t2 = now() - SNOWFLAKE_EPOCH_MS;
      while (t2 <= last) {
        wait();
        t2 = now() - SNOWFLAKE_EPOCH_MS;
      }
      last = t2;
      seq = 0;
    } else {
      // 时钟在往回走（或重启后落后于 notBefore）：向前借一毫秒
      last++;
      seq = 0;
    }
    return (BigInt(last) << (WORKER_BITS + SEQ_BITS)) | (worker << SEQ_BITS) | BigInt(seq);
  };

  return {
    next,
    nextDecimal: () => next().toString(),
    parse: (id) => ({
      timestamp: Number(id >> (WORKER_BITS + SEQ_BITS)) + SNOWFLAKE_EPOCH_MS,
      workerId: Number((id >> SEQ_BITS) & BigInt(MAX_WORKER)),
      sequence: Number(id & BigInt(MAX_SEQ)),
    }),
  };
}

import type { AdaptFormat, Segment } from "@/api/drama/type";
import type { Capabilities } from "@/api/model/type";

/**
 * 片场的提示词格式：分镜原稿固定是 Seedance 写法；顶栏视频模型是别的写法时，
 * 要先把原稿转写成那种格式（存在片段的 adaptations 里）再提交。
 */
export type TargetFormat = "seedance" | AdaptFormat;

/** 转写稿相对当前原稿的状态：未转写 / 已过期（原稿在转写后改过）/ 最新 */
export type AdaptState = "none" | "stale" | "fresh";

/**
 * 顶栏模型要的格式：按后台配置的 prompt_format 判断，不看模型名；
 * 没配（空 / 未知）按通用；还没有模型时当作 Seedance，界面保持原样。
 */
export function targetFormat(caps: Capabilities | undefined): TargetFormat {
  if (!caps) return "seedance";
  if (caps.prompt_format === "seedance") return "seedance";
  if (caps.prompt_format === "h3") return "h3";
  return "generic";
}

const LABELS: Record<TargetFormat, string> = { seedance: "Seedance", h3: "H3", generic: "通用" };

/** 标签上的短名 */
export const formatLabel = (format: TargetFormat): string => LABELS[format];

const toHex = (bytes: Uint8Array): string =>
  Array.from(bytes, (byte) => byte.toString(16).padStart(2, "0")).join("");

/**
 * 原稿全文 UTF-8 的 SHA-256 小写 hex，与后端 source_hash 同算法。
 * crypto.subtle 只在安全上下文（https / localhost）里有；用 http + 局域网 IP 访问时退回纯 JS 实现。
 */
export async function sha256Hex(text: string): Promise<string> {
  if (!globalThis.crypto?.subtle) return sha256HexSync(text);
  const digest = await crypto.subtle.digest("SHA-256", new TextEncoder().encode(text));
  return toHex(new Uint8Array(digest));
}

/** SHA-256 的轮常量：前 64 个质数立方根小数部分的前 32 位 */
const K = Uint32Array.from([
  0x428a2f98, 0x71374491, 0xb5c0fbcf, 0xe9b5dba5, 0x3956c25b, 0x59f111f1, 0x923f82a4, 0xab1c5ed5,
  0xd807aa98, 0x12835b01, 0x243185be, 0x550c7dc3, 0x72be5d74, 0x80deb1fe, 0x9bdc06a7, 0xc19bf174,
  0xe49b69c1, 0xefbe4786, 0x0fc19dc6, 0x240ca1cc, 0x2de92c6f, 0x4a7484aa, 0x5cb0a9dc, 0x76f988da,
  0x983e5152, 0xa831c66d, 0xb00327c8, 0xbf597fc7, 0xc6e00bf3, 0xd5a79147, 0x06ca6351, 0x14292967,
  0x27b70a85, 0x2e1b2138, 0x4d2c6dfc, 0x53380d13, 0x650a7354, 0x766a0abb, 0x81c2c92e, 0x92722c85,
  0xa2bfe8a1, 0xa81a664b, 0xc24b8b70, 0xc76c51a3, 0xd192e819, 0xd6990624, 0xf40e3585, 0x106aa070,
  0x19a4c116, 0x1e376c08, 0x2748774c, 0x34b0bcb5, 0x391c0cb3, 0x4ed8aa4a, 0x5b9cca4f, 0x682e6ff3,
  0x748f82ee, 0x78a5636f, 0x84c87814, 0x8cc70208, 0x90befffa, 0xa4506ceb, 0xbef9a3f7, 0xc67178f2,
]);

/** 纯 JS 的 SHA-256（FIPS 180-4），只在没有 crypto.subtle 时用；原稿只有几百字，性能不是问题 */
export function sha256HexSync(text: string): string {
  const data = new TextEncoder().encode(text);
  // 补位：0x80 + 若干 0 + 64 位大端长度，总长补到 64 字节的整数倍
  const blocks = Math.ceil((data.length + 9) / 64);
  const buf = new Uint8Array(blocks * 64);
  buf.set(data);
  buf[data.length] = 0x80;
  const view = new DataView(buf.buffer);
  const bits = data.length * 8;
  view.setUint32(buf.length - 8, Math.floor(bits / 0x100000000));
  view.setUint32(buf.length - 4, bits >>> 0);

  const h = Uint32Array.from([
    0x6a09e667, 0xbb67ae85, 0x3c6ef372, 0xa54ff53a, 0x510e527f, 0x9b05688c, 0x1f83d9ab, 0x5be0cd19,
  ]);
  const w = new Uint32Array(64);
  const rotr = (x: number, n: number) => (x >>> n) | (x << (32 - n));
  for (let block = 0; block < blocks; block++) {
    for (let i = 0; i < 16; i++) w[i] = view.getUint32(block * 64 + i * 4);
    for (let i = 16; i < 64; i++) {
      const s0 = rotr(w[i - 15], 7) ^ rotr(w[i - 15], 18) ^ (w[i - 15] >>> 3);
      const s1 = rotr(w[i - 2], 17) ^ rotr(w[i - 2], 19) ^ (w[i - 2] >>> 10);
      w[i] = w[i - 16] + s0 + w[i - 7] + s1;
    }
    let [a, b, c, d, e, f, g, hh] = h;
    for (let i = 0; i < 64; i++) {
      const t1 = hh + (rotr(e, 6) ^ rotr(e, 11) ^ rotr(e, 25)) + ((e & f) ^ (~e & g)) + K[i] + w[i];
      const t2 = (rotr(a, 2) ^ rotr(a, 13) ^ rotr(a, 22)) + ((a & b) ^ (a & c) ^ (b & c));
      hh = g;
      g = f;
      f = e;
      e = (d + t1) >>> 0;
      d = c;
      c = b;
      b = a;
      a = (t1 + t2) >>> 0;
    }
    h[0] += a;
    h[1] += b;
    h[2] += c;
    h[3] += d;
    h[4] += e;
    h[5] += f;
    h[6] += g;
    h[7] += hh;
  }
  const out = new Uint8Array(32);
  const outView = new DataView(out.buffer);
  for (let i = 0; i < 8; i++) outView.setUint32(i * 4, h[i]);
  return toHex(out);
}

/** 某格式转写稿的状态；promptHash 是当前原稿的指纹 */
export function adaptState(segment: Segment, format: AdaptFormat, promptHash: string): AdaptState {
  const adaptation = segment.adaptations[format];
  if (!adaptation) return "none";
  return adaptation.sourceHash === promptHash ? "fresh" : "stale";
}

/** 这个格式下要提交的文字：Seedance 交原稿，其他交转写稿（没有为空串） */
export function submitText(segment: Segment, format: TargetFormat): string {
  if (format === "seedance") return segment.prompt;
  return segment.adaptations[format]?.prompt ?? "";
}

export type BatchSplit = {
  /** 转写稿是最新的，可以直接提交 */
  ready: Segment[];
  /** 还没有这个格式的转写稿 */
  none: Segment[];
  /** 原稿在转写后改过 */
  stale: Segment[];
  /** 过期稿里被手改过的个数（重新转写会覆盖） */
  edited: number;
};

/** 批量生成前把待生成片段按转写稿状态分组 */
export function splitBatch(
  targets: readonly Segment[],
  format: AdaptFormat,
  hashOf: (segment: Segment) => string,
): BatchSplit {
  const split: BatchSplit = { ready: [], none: [], stale: [], edited: 0 };
  for (const segment of targets) {
    const state = adaptState(segment, format, hashOf(segment));
    if (state === "fresh") split.ready.push(segment);
    else if (state === "none") split.none.push(segment);
    else {
      split.stale.push(segment);
      if (segment.adaptations[format]?.edited) split.edited += 1;
    }
  }
  return split;
}

/** 片段号：「01、03」 */
export const segmentNos = (list: readonly Pick<Segment, "seq">[]): string =>
  list.map((item) => String(item.seq + 1).padStart(2, "0")).join("、");

/**
 * ID 编码密钥的一致性自检。Agent 服务用密钥给前端的 ID 编码、解码路径里的 ID，必须和 Go 的一样，否则前端拿到的 ID 全都对不上，
 * 表现是「什么都打不开」。Compose 里 Go 的实际密钥来自配置文件的默认值或 APP_SERVER_ID_KEY，很容易配错，所以启动后问 Go：
 * 拿几个 id 让它编码，和自己编的比一比。
 *
 * - 一致：不再打扰 Go；
 * - 不一致：`/health` 变成不健康（让一键升级的就绪检查失败、自动回退），日志说清楚怎么改；之后继续核对，Go 改对重启后自己恢复；
 * - Go 暂时连不上：保持「待定」，不当成不一致（两个容器的启动顺序不确定），连上再核对。
 */
import type { IdCodec } from "./ids.ts";

/** 核对用的 id：包括超过 2^53 的（JavaScript 数字装不下，编码里丢精度的错误只有大 id 才暴露） */
const PROBE_IDS = ["1", "281474976710657123"];

export type IdCheckState = "pending" | "ok" | "mismatch";

export interface IdCheckOptions {
  platform: { encodeId(n: string): Promise<string> };
  codec: IdCodec;
  log: (msg: string, extra?: Record<string, unknown>) => void;
  /** 两次核对的间隔（毫秒），默认 3000；不一致时放慢到 10 倍 */
  intervalMs?: number;
}

export interface IdCheck {
  state(): IdCheckState;
  stop(): Promise<void>;
}

export function startIdCheck(o: IdCheckOptions): IdCheck {
  const interval = o.intervalMs ?? 3000;
  let state: IdCheckState = "pending";
  let stopped = false;
  let timer: ReturnType<typeof setTimeout> | undefined;
  let running: Promise<void> = Promise.resolve();

  const probe = async (): Promise<void> => {
    if (stopped) return;
    let next: IdCheckState = state;
    let delay = interval;
    try {
      let same = true;
      for (const n of PROBE_IDS) if ((await o.platform.encodeId(n)) !== o.codec.encode(BigInt(n))) same = false;
      next = same ? "ok" : "mismatch";
    } catch {
      next = state === "mismatch" ? "mismatch" : "pending"; // 连不上：不改判断
    }
    if (stopped) return;
    if (next === "mismatch") {
      if (state !== "mismatch") {
        o.log("ID 编码密钥和 Go 不一致：前端会拿到对不上的 ID。请让 AGENT_ID_KEY 和 Go 实际使用的密钥一致（Go 用的是 APP_SERVER_ID_KEY；没设时用配置文件里的 server.id_key，" +
          "镜像里的默认值是 id_key）", { level: "error" });
      }
      delay = interval * 10;
    }
    if (next === "ok" && state === "mismatch") o.log("ID 编码密钥已和 Go 一致");
    state = next;
    if (state !== "ok") timer = setTimeout(() => void (running = probe()), delay);
  };
  running = probe();

  return {
    state: () => state,
    stop: async () => {
      stopped = true;
      if (timer) clearTimeout(timer);
      await running;
    },
  };
}

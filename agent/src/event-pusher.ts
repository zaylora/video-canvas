/**
 * 把引擎产生的事件推给 Go，再由 Go 经 WebSocket 发给浏览器。
 *
 * 事件已经落在 Agent 自己的库里，推送只是"让前端更快看到"：丢了不要紧，前端按 seq 走 HTTP 回放对账。
 * 所以这里不做重试，只保证：顺序不乱（同一时刻只有一个请求在途）、不拖慢引擎（publish 不等网络）、
 * 积压有上限（Go 不通时先丢文字增量）、停机时尽量推完。
 */
import type { PublishedEvent } from "./engine/engine.ts";

/** Go 的 RelayEvent（POST /internal/platform/v1/events 的一条）；id 是十进制字符串，由 Go 编码成对外的 id */
export interface RelayEvent {
  user_id: string;
  session_id: string;
  canvas_id: string;
  /** 会话级事件为 "0" */
  run_id: string;
  /** 0 表示临时事件（文字增量），不参与回放 */
  seq: number;
  type: string;
  data: unknown;
  created_at: string;
}

export function toRelayEvent(e: PublishedEvent): RelayEvent {
  return {
    user_id: e.userId, session_id: e.sessionId, canvas_id: e.canvasId, run_id: e.runId, seq: e.seq, type: e.type,
    data: e.payload === undefined ? null : e.payload, created_at: new Date(e.createdAt).toISOString(),
  };
}

export interface EventPusherOptions {
  /** 把一批事件推给 Go；抛错表示这批丢了 */
  push: (events: RelayEvent[]) => Promise<void>;
  /** 攒批的时间窗（毫秒），默认 30：文字增量一个字一个事件，逐条发请求太碎 */
  intervalMs?: number;
  /** 每个请求最多带多少条，默认 100（Go 的上限是 200） */
  maxBatch?: number;
  /** 队列里最多积压多少条，默认 2000；超了先丢最旧的临时事件，没有临时事件再丢最旧的 */
  maxQueue?: number;
  /** 一批推送失败（这批已丢弃）或积压溢出时调用；dropped 是丢掉的条数 */
  onError?: (e: unknown, dropped: number) => void;
}

const DEFAULT_CLOSE_TIMEOUT_MS = 3000;

export class EventPusher {
  readonly #push: EventPusherOptions["push"];
  readonly #intervalMs: number;
  readonly #maxBatch: number;
  readonly #maxQueue: number;
  readonly #onError: NonNullable<EventPusherOptions["onError"]>;
  #queue: RelayEvent[] = [];
  #timer: NodeJS.Timeout | undefined;
  #running: Promise<void> | undefined;
  #closed = false;

  constructor(o: EventPusherOptions) {
    this.#push = o.push;
    this.#intervalMs = o.intervalMs ?? 30;
    this.#maxBatch = o.maxBatch ?? 100;
    this.#maxQueue = o.maxQueue ?? 2000;
    this.#onError = o.onError ?? (() => {});
  }

  /** 排队等推送，不等网络；关闭之后的事件忽略 */
  publish(e: PublishedEvent): void {
    if (this.#closed) return;
    this.#queue.push(toRelayEvent(e));
    if (this.#queue.length > this.#maxQueue) this.#shed();
    if (!this.#running && !this.#timer) {
      this.#timer = setTimeout(() => {
        this.#timer = undefined;
        this.#kick();
      }, this.#intervalMs);
      this.#timer.unref();
    }
  }

  /** 停止接收，把积压的推完再返回；超时（默认 3 秒）就放弃，不拖住停机 */
  async close(timeoutMs = DEFAULT_CLOSE_TIMEOUT_MS): Promise<void> {
    this.#closed = true;
    if (this.#timer) clearTimeout(this.#timer);
    this.#timer = undefined;
    let timer: NodeJS.Timeout | undefined;
    const timeout = new Promise<void>((r) => (timer = setTimeout(r, timeoutMs)));
    const drained = (async () => {
      while (this.#queue.length > 0 || this.#running) {
        this.#kick();
        await this.#running;
      }
    })();
    await Promise.race([drained, timeout]);
    clearTimeout(timer);
  }

  #shed(): void {
    const i = this.#queue.findIndex((e) => e.seq === 0);
    this.#queue.splice(i >= 0 ? i : 0, 1);
    this.#onError(new Error("事件积压超过上限，丢弃了最旧的一条"), 1);
  }

  #kick(): void {
    if (this.#running || this.#queue.length === 0) return;
    this.#running = this.#drain().finally(() => {
      this.#running = undefined;
    });
  }

  async #drain(): Promise<void> {
    while (this.#queue.length > 0) {
      const batch = this.#queue.splice(0, this.#maxBatch);
      try {
        await this.#push(batch);
      } catch (e) {
        try {
          this.#onError(e, batch.length);
        } catch {
          // 回调出错不能影响后面的推送
        }
      }
    }
  }
}

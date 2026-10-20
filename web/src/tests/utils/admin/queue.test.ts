import { describe, expect, test } from "bun:test";

import type {
  QueueChannel,
  QueueInstance,
  QueueOverview,
  ThroughputPoint,
} from "@/api/admin/queue/type";
import {
  appliedText,
  channelRow,
  countCells,
  failureRate,
  formatAgo,
  formatDuration,
  formatPercent,
  formatStartedAt,
  instanceLoadText,
  isDraftDirty,
  isInstanceLost,
  isQueueIdle,
  lostInstances,
  pendingTotal,
  PAUSE_CONFIRM_TEXT,
  queueWriteDenyReason,
  riverUiDenyReason,
  saveFailedText,
  nextDraft,
  shownConcurrency,
  summarizeThroughput,
  throughputBars,
  validateConcurrency,
} from "@/utils/admin/queue";

const counts = { waiting: 0, running: 0, retryable: 0, discarded_1h: 0 };

const instance = (patch: Partial<QueueInstance> = {}): QueueInstance => ({
  id: "rc_1",
  hostname: "vc-api-1",
  version: "v0.4.6",
  started_at: "2026-10-20T01:12:00Z",
  heartbeat_at: "2026-10-20T02:00:00Z",
  online: true,
  is_leader: true,
  stats: {},
  ...patch,
});

const point = (succeeded: number, failed: number, avg_wait_ms = 0): ThroughputPoint => ({
  minute: "2026-10-20T02:00:00Z",
  succeeded,
  failed,
  avg_wait_ms,
});

describe("并发范围校验", () => {
  test("1 到硬上限的整数合法", () => {
    expect(validateConcurrency(1, 64)).toBeNull();
    expect(validateConcurrency(64, 64)).toBeNull();
    expect(validateConcurrency(8, 16)).toBeNull();
  });
  test("越界与非整数不合法，原因带上限", () => {
    expect(validateConcurrency(0, 64)).toBe("并发范围 1 到 64");
    expect(validateConcurrency(17, 16)).toBe("并发范围 1 到 16");
    expect(validateConcurrency(2.5, 16)).toBe("并发必须是整数");
    expect(validateConcurrency(Number.NaN, 16)).toBe("并发必须是整数");
  });
});

describe("保存草稿与服务端值分开保存", () => {
  test("没有草稿或草稿等于服务端值都不算脏", () => {
    expect(isDraftDirty(undefined, 8)).toBe(false);
    expect(isDraftDirty(8, 8)).toBe(false);
    expect(isDraftDirty(9, 8)).toBe(true);
  });

  test("拖动中的草稿不会被刷新带来的新服务端值覆盖", () => {
    const draft = nextDraft(20, 8);
    expect(shownConcurrency(draft, 8)).toBe(20);
    /** 服务端值被别人改成 12：滑杆仍显示草稿 20，并且仍然是脏的 */
    expect(shownConcurrency(draft, 12)).toBe(20);
    expect(isDraftDirty(draft, 12)).toBe(true);
    /** 没有草稿时跟着服务端走 */
    expect(shownConcurrency(undefined, 16)).toBe(16);
  });

  test("拖回服务端值等于没有草稿", () => {
    expect(nextDraft(20, 8)).toBe(20);
    expect(nextDraft(8, 8)).toBeUndefined();
  });
});

describe("保存与暂停的文案", () => {
  test("已生效于 N/M 个实例", () => {
    expect(appliedText(2, 3)).toBe("已生效于 2/3 个实例");
  });
  test("保存失败带原因并说明已还原", () => {
    expect(saveFailedText("并发范围 1 到 64", 8)).toBe("保存失败：并发范围 1 到 64，已还原为 8");
  });
  test("暂停确认提示：任务保留、排队时间继续累计、超时过期并退款", () => {
    expect(PAUSE_CONFIRM_TEXT).toContain("已入队任务保留");
    expect(PAUSE_CONFIRM_TEXT).toContain("排队时间继续累计");
    expect(PAUSE_CONFIRM_TEXT).toContain("过期并退款");
  });
});

describe("权限原因", () => {
  test("super_admin 没有禁用原因，admin 有", () => {
    expect(queueWriteDenyReason(true)).toBeNull();
    expect(riverUiDenyReason(true)).toBeNull();
    expect(queueWriteDenyReason(false)).toContain("super_admin");
    expect(riverUiDenyReason(false)).toContain("super_admin");
  });
});

describe("计数汇总与空闲判断", () => {
  test("未完成任务数 = 排队 + 运行 + 重试，丢弃不算", () => {
    expect(pendingTotal({ waiting: 23, running: 8, retryable: 2, discarded_1h: 9 })).toBe(33);
  });

  test("计数格固定四项：排队中、运行中、重试中、近 1 小时丢弃", () => {
    expect(
      countCells({ waiting: 5, running: 3, retryable: 2, discarded_1h: 1 }).map((cell) => [
        cell.label,
        cell.value,
        cell.warn,
      ]),
    ).toEqual([
      ["排队中", 5, false],
      ["运行中", 3, false],
      ["重试中", 2, true],
      ["近 1 小时丢弃", 1, true],
    ]);
  });

  const overview = (patch: Partial<QueueOverview> = {}): QueueOverview => ({
    queue: { paused: false, concurrency: 8, hard_cap: 64, counts },
    channels: [],
    instances: [],
    ...patch,
  });

  test("没有任务、没有渠道占用时空闲", () => {
    expect(isQueueIdle(overview())).toBe(true);
  });
  test("队列里有任务，或渠道有占用 / 等待，都不空闲", () => {
    const busyQueue = overview({
      queue: { paused: false, concurrency: 8, hard_cap: 64, counts: { ...counts, waiting: 1 } },
    });
    expect(isQueueIdle(busyQueue)).toBe(false);
    const channel = {
      key: "a",
      name: "A",
      occupied: 0,
      max_running: 5,
      waiting: 0,
      oldest_wait_sec: 0,
    };
    expect(isQueueIdle(overview({ channels: [{ ...channel, occupied: 1 }] }))).toBe(false);
    expect(isQueueIdle(overview({ channels: [{ ...channel, waiting: 2 }] }))).toBe(false);
  });
});

describe("实例是否失联", () => {
  test("以后端的 online 为准", () => {
    expect(isInstanceLost(instance())).toBe(false);
    expect(isInstanceLost(instance({ online: false }))).toBe(true);
  });
  test("挑出失联的实例", () => {
    const list = [instance({ id: "a" }), instance({ id: "b", online: false })];
    expect(lostInstances(list).map((item) => item.id)).toEqual(["b"]);
    expect(lostInstances([instance()])).toEqual([]);
  });
  test("处理中 / 并发：失联实例的处理中显示“–”，stats 为空对象显示“–”", () => {
    expect(instanceLoadText({ inflight: 3, limit: 8 }, true)).toBe("3 / 8");
    expect(instanceLoadText({ inflight: 3, limit: 8 }, false)).toBe("– / 8");
    expect(instanceLoadText({}, true)).toBe("–");
  });
});

describe("失败率与吞吐摘要", () => {
  test("失败率按失败 /（完成 + 失败）算，没有任务时为 0", () => {
    expect(failureRate(97, 3)).toBeCloseTo(3);
    expect(failureRate(0, 0)).toBe(0);
    expect(failureRate(0, 5)).toBe(100);
    expect(formatPercent(failureRate(97, 3))).toBe("3.0%");
    expect(formatPercent(0)).toBe("0.0%");
  });

  test("汇总完成数、失败数、失败率；平均排队按每分钟任务数加权，空分钟不拉低", () => {
    const summary = summarizeThroughput([
      point(9, 1, 60_000),
      point(0, 0, 0),
      point(30, 0, 120_000),
    ]);
    expect(summary.succeeded).toBe(39);
    expect(summary.failed).toBe(1);
    expect(summary.failureRate).toBeCloseTo(2.5);
    expect(summary.avgWaitMs).toBeCloseTo((10 * 60_000 + 30 * 120_000) / 40);
    expect(summary.empty).toBe(false);
  });

  test("一个任务都没有：空闲，平均排队为 0", () => {
    const summary = summarizeThroughput([point(0, 0), point(0, 0)]);
    expect(summary).toMatchObject({
      succeeded: 0,
      failed: 0,
      failureRate: 0,
      avgWaitMs: 0,
      empty: true,
    });
    expect(summarizeThroughput([]).empty).toBe(true);
  });

  test("柱子高度以单分钟总数最高的为 1；全 0 时都是 0", () => {
    const bars = throughputBars([point(6, 2), point(2, 0), point(0, 0)]);
    expect(bars[0]).toMatchObject({ succeededRatio: 0.75, failedRatio: 0.25 });
    expect(bars[1]).toMatchObject({ succeededRatio: 0.25, failedRatio: 0 });
    expect(bars[2]).toMatchObject({ succeededRatio: 0, failedRatio: 0 });
    expect(throughputBars([point(0, 0)])[0]).toMatchObject({ succeededRatio: 0, failedRatio: 0 });
  });
});

describe("渠道占用行", () => {
  const channel = (patch: Partial<QueueChannel> = {}): QueueChannel => ({
    key: "kling",
    name: "可灵 · 官方",
    occupied: 5,
    max_running: 5,
    waiting: 14,
    oldest_wait_sec: 372,
    ...patch,
  });

  test("占用达到上限显示满载，带等待数与最早等待时长", () => {
    expect(channelRow(channel())).toEqual({
      key: "kling",
      name: "可灵 · 官方",
      occupancy: "5 / 5",
      waiting: 14,
      oldestWait: "6 分 12 秒",
      full: true,
    });
  });
  test("还有空位", () => {
    expect(
      channelRow(channel({ occupied: 2, max_running: 6, waiting: 0, oldest_wait_sec: 0 })),
    ).toMatchObject({
      occupancy: "2 / 6",
      full: false,
      oldestWait: null,
    });
  });
  test("max_running 为 0 显示“不限”，永远不满载", () => {
    expect(
      channelRow(channel({ occupied: 30, max_running: 0, waiting: 0, oldest_wait_sec: 0 })),
    ).toMatchObject({
      occupancy: "30 / 不限",
      full: false,
    });
  });
});

describe("时间显示", () => {
  test("时长：秒、分秒、小时", () => {
    expect(formatDuration(0)).toBe("0 秒");
    expect(formatDuration(47)).toBe("47 秒");
    expect(formatDuration(108)).toBe("1 分 48 秒");
    expect(formatDuration(120)).toBe("2 分");
    expect(formatDuration(3600)).toBe("1 小时");
    expect(formatDuration(5400)).toBe("1 小时 30 分");
    expect(formatDuration(-3)).toBe("0 秒");
  });

  test("心跳相对时间，时钟偏差不出现负数，坏数据为空串", () => {
    const now = Date.parse("2026-10-20T02:00:47Z");
    expect(formatAgo("2026-10-20T02:00:00Z", now)).toBe("47 秒前");
    expect(formatAgo("2026-10-20T02:00:59Z", now)).toBe("0 秒前");
    expect(formatAgo("not-a-date", now)).toBe("");
  });

  test("启动时间：今天只显示时刻，其他天带日期", () => {
    const started = new Date(2026, 9, 20, 9, 12).toISOString();
    expect(formatStartedAt(started, new Date(2026, 9, 20, 18, 0).getTime())).toBe("今天 09:12");
    expect(formatStartedAt(started, new Date(2026, 9, 21, 8, 0).getTime())).toBe("10-20 09:12");
    expect(formatStartedAt("x", Date.now())).toBe("");
  });
});

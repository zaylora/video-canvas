import { describe, expect, test } from "bun:test";

import type {
  AdminStatsDay,
  ChannelLoad,
  ChannelView,
  PluginMeta,
  PluginView,
} from "@/api/admin/ai/type";
import {
  dayTotal,
  formatMonthDay,
  formatTooltipDate,
  loadRows,
  niceTicks,
  successRate,
  thinTicks,
  todayAndYesterday,
  topN,
} from "@/utils/admin/overview-stats";

const day = (date: string, succeeded = 0, failed = 0, other = 0): AdminStatsDay => ({
  date,
  succeeded,
  failed,
  other,
});

describe("dayTotal", () => {
  test("三个桶之和就是当天任务数", () => {
    expect(dayTotal(day("2026-10-10", 5, 1, 2))).toBe(8);
    expect(dayTotal(day("2026-10-10"))).toBe(0);
  });
});

describe("todayAndYesterday", () => {
  test("最后一项是今天，倒数第二项是昨天", () => {
    const daily = [day("2026-10-08", 1), day("2026-10-09", 2, 1), day("2026-10-10", 3, 0, 2)];
    expect(todayAndYesterday(daily)).toEqual({ today: 5, yesterday: 3 });
  });
  test("数据不足时按 0 处理", () => {
    expect(todayAndYesterday([])).toEqual({ today: 0, yesterday: 0 });
    expect(todayAndYesterday([day("2026-10-10", 4)])).toEqual({ today: 4, yesterday: 0 });
  });
});

describe("successRate", () => {
  test("成功 ÷ (成功 + 失败)，其他不进分母", () => {
    const daily = [day("a", 90, 10, 500), day("b", 0, 0, 7)];
    expect(successRate(daily)).toBe(90);
  });
  test("没有已结束的任务返回 null，页面显示“—”而不是 0%", () => {
    expect(successRate([])).toBeNull();
    expect(successRate([day("a", 0, 0, 9)])).toBeNull();
  });
  test("全部失败是 0 而不是 null", () => {
    expect(successRate([day("a", 0, 4)])).toBe(0);
  });
});

describe("topN", () => {
  const item = (label: string, count: number) => ({ key: label, label, count });
  test("超过 n+1 项时取前 n 名，其余合并成“其他”", () => {
    const got = topN([item("a", 10), item("b", 8), item("c", 5), item("d", 3), item("e", 2)], 3);
    expect(got.map((x) => [x.label, x.count, x.other ?? false])).toEqual([
      ["a", 10, false],
      ["b", 8, false],
      ["c", 5, false],
      ["其他", 5, true],
    ]);
  });
  test("只多出一项时不合并：一个“其他”里只有一项没有意义", () => {
    const got = topN([item("a", 3), item("b", 2), item("c", 1)], 2);
    expect(got.map((x) => x.label)).toEqual(["a", "b", "c"]);
  });
  test("不修改传入的数组；空数组返回空数组", () => {
    const input = [item("a", 1)];
    expect(topN(input, 5)).toEqual(input);
    expect(topN([], 5)).toEqual([]);
  });
});

describe("niceTicks", () => {
  test("刻度取 1 / 2 / 5 × 10ⁿ，上限不小于最大值", () => {
    expect(niceTicks(87)).toEqual({ max: 100, ticks: [0, 25, 50, 75, 100] });
    expect(niceTicks(187)).toEqual({ max: 200, ticks: [0, 50, 100, 150, 200] });
    expect(niceTicks(4)).toEqual({ max: 4, ticks: [0, 1, 2, 3, 4] });
    expect(niceTicks(1200).max).toBeGreaterThanOrEqual(1200);
  });
  test("没有数据时给 0–4 的坐标轴，图表仍然有网格", () => {
    expect(niceTicks(0)).toEqual({ max: 4, ticks: [0, 1, 2, 3, 4] });
  });
});

describe("thinTicks", () => {
  const dates = (n: number) => Array.from({ length: n }, (_, i) => `d${i}`);
  test("7 天每天一个标签", () => {
    expect(thinTicks(dates(7))).toEqual(dates(7));
  });
  test("30 天从今天往前每 5 天一个，保证最后一天（今天）一定有标签", () => {
    const got = thinTicks(dates(30));
    expect(got).toEqual(["d4", "d9", "d14", "d19", "d24", "d29"]);
    expect(got.at(-1)).toBe("d29");
  });
});

describe("日期格式", () => {
  test("formatMonthDay：月/日不补零", () => {
    expect(formatMonthDay("2026-10-04")).toBe("10/4");
    expect(formatMonthDay("2026-01-31")).toBe("1/31");
  });
  test("formatTooltipDate：带星期，不受运行环境时区影响", () => {
    expect(formatTooltipDate("2026-10-04")).toBe("10 月 4 日 · 周日");
    expect(formatTooltipDate("2026-10-10")).toBe("10 月 10 日 · 周六");
  });
});

describe("loadRows", () => {
  const bearer: PluginMeta = { auth: { type: "bearer" } };
  const plugin = (extra: Partial<PluginView> = {}): PluginView => ({
    key: "p",
    name: "插件P",
    source: "uploaded",
    enabled: true,
    updated_at: "",
    versions: [
      {
        id: 1,
        plugin_key: "p",
        version: "1.0.0",
        sha256: "",
        created_at: "",
        created_by: 0,
        channel_count: 0,
        meta: bearer,
      },
    ],
    ...extra,
  });
  const channel = (key: string, extra: Partial<ChannelView> = {}): ChannelView => ({
    key,
    name: key.toUpperCase(),
    plugin_key: "p",
    plugin_version_id: 1,
    plugin_version: "1.0.0",
    base_url: "https://x.test",
    trusted_internal: false,
    allow_credentials: false,
    settings: null,
    rate_limit: null,
    enabled: true,
    secret_set: true,
    updated_by: 0,
    updated_at: "",
    created_at: "",
    ...extra,
  });
  const load = (key: string, running: number, waiting: number): ChannelLoad => ({
    channel: key,
    running,
    waiting,
  });

  test("没有出现在负载里的渠道按空闲处理", () => {
    const [row] = loadRows([channel("a")], [plugin()], []);
    expect(row).toMatchObject({ key: "a", running: 0, waiting: 0, usable: true, idle: true });
    expect(row.runningRatio).toBe(0);
    expect(row.queuedRatio).toBe(0);
  });

  test("有上限时分母是上限：生成中 2 / 4 占一半，排队叠在后面", () => {
    const ch = channel("a", { rate_limit: { max_running: 4 } });
    const [row] = loadRows([ch], [plugin()], [load("a", 2, 1)]);
    expect(row.max).toBe(4);
    expect(row.runningRatio).toBe(0.5);
    expect(row.queuedRatio).toBe(0.75);
    expect(row.full).toBe(false);
  });

  test("满载：生成中达到上限，排队超出上限时分母取两者之和，条不溢出", () => {
    const ch = channel("a", { rate_limit: { max_running: 4 } });
    const [row] = loadRows([ch], [plugin()], [load("a", 4, 4)]);
    expect(row.full).toBe(true);
    expect(row.runningRatio).toBe(0.5);
    expect(row.queuedRatio).toBe(1);
  });

  test("不限（max_running 为 0 或没配）：分母取所有可用渠道里最大的占用，永远不算满载", () => {
    const rows = loadRows(
      [channel("a"), channel("b", { rate_limit: { rps: 5 } })],
      [plugin()],
      [load("a", 3, 1), load("b", 1, 0)],
    );
    const a = rows.find((r) => r.key === "a")!;
    const b = rows.find((r) => r.key === "b")!;
    expect(a.max).toBe(0);
    expect(a.full).toBe(false);
    expect(a.runningRatio).toBe(0.75);
    expect(a.queuedRatio).toBe(1);
    expect(b.runningRatio).toBe(0.25);
  });

  test("不可用的渠道（缺 Key / 停用）不画负载，带原因标签，排在最后", () => {
    const rows = loadRows(
      [
        channel("nokey", { secret_set: false }),
        channel("off", { enabled: false }),
        channel("busy"),
        channel("idle"),
      ],
      [plugin()],
      [load("busy", 2, 0), load("nokey", 3, 0)],
    );
    expect(rows.map((r) => r.key)).toEqual(["busy", "idle", "nokey", "off"]);
    const nokey = rows.find((r) => r.key === "nokey")!;
    expect(nokey).toMatchObject({
      usable: false,
      label: "未设 Key",
      runningRatio: 0,
      queuedRatio: 0,
    });
    expect(rows.find((r) => r.key === "off")).toMatchObject({ usable: false, label: "已停用" });
  });

  test("可用渠道按“生成中 + 排队”降序，同样大按 key 升序", () => {
    const rows = loadRows(
      [channel("c"), channel("a"), channel("b")],
      [plugin()],
      [load("a", 1, 0), load("b", 1, 0), load("c", 0, 5)],
    );
    expect(rows.map((r) => r.key)).toEqual(["c", "a", "b"]);
  });

  test("没有渠道返回空数组", () => {
    expect(loadRows([], [plugin()], [])).toEqual([]);
  });
});

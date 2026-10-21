import { describe, expect, test } from "bun:test";

import { adminAgentRunEndpoints as ep } from "@/api/admin/agent-run/endpoints";
import type { AgentRunDigest, AgentStatsGroup } from "@/api/admin/agent-run/type.d";
import {
  DEFAULT_RUN_FILTERS,
  FLAT_THRESHOLD_PP,
  MIN_SAMPLE,
  buildVersionCards,
  compareVersions,
  feedbackReasonLabel,
  formatRate,
  harnessLabel,
  harnessOptions,
  hasActiveRunFilters,
  modelOptions,
  normalizeUserIdInput,
  runListQuery,
  runRowBadges,
  runStatusView,
  statsSince,
  clampPage,
  pageCount,
  toggleHarnessFilter,
  type RunFilters,
} from "@/utils/admin/agent-run";

const NOW = Date.parse("2026-10-22T10:30:00Z");
const filters = (patch: Partial<RunFilters> = {}): RunFilters => ({
  ...DEFAULT_RUN_FILTERS,
  ...patch,
});

const group = (patch: Partial<AgentStatsGroup> = {}): AgentStatsGroup => ({
  key: "a1b2c3d4e5f6",
  n: 50,
  feedback_rate: 0.2,
  down_rate: 0.3,
  undo_rate: 0.1,
  fail_end_rate: 0.05,
  median_duration_ms: 42_000,
  median_steps: 8,
  avg_credits: 3.5,
  prompt_version: 10,
  build_sha: "abc1234",
  last_at: "2026-10-22T09:00:00Z",
  ...patch,
});

const digest = (patch: Partial<AgentRunDigest> = {}): AgentRunDigest => ({
  run_id: "0123456789abcdef0123456789abcdef",
  session_id: "s",
  canvas_id: "c",
  user_id: 7,
  mode: "auto",
  model_key: "m",
  prompt_version: 10,
  tools_hash: "t",
  build_sha: "b",
  harness_key: "a1b2c3d4e5f6",
  skills: {},
  status: "succeeded",
  error_code: "",
  request_head: "把第三个镜头改成夜景",
  steps: 5,
  tool_calls: 4,
  tool_errors: 0,
  model_calls: 5,
  input_tokens: 100,
  output_tokens: 50,
  spent_credits: 2,
  duration_ms: 1000,
  approvals_total: 0,
  approvals_rejected: 0,
  steer_count: 0,
  mutation_count: 0,
  undone: false,
  feedback: 0,
  ended_at: null,
  created_at: "2026-10-22T09:00:00Z",
  updated_at: "2026-10-22T09:00:00Z",
  ...patch,
});

describe("接口路径", () => {
  test("列表、统计固定；详情和导出带编码后的 rid", () => {
    expect(ep.list()).toBe("/admin/agent/runs");
    expect(ep.stats()).toBe("/admin/agent/stats");
    expect(ep.detail("ab12")).toBe("/admin/agent/runs/ab12");
    expect(ep.export("ab12")).toBe("/admin/agent/runs/ab12/export");
    expect(ep.detail("a/b")).toBe("/admin/agent/runs/a%2Fb");
  });
});

describe("筛选 → 查询参数", () => {
  test("默认筛选不带任何条件", () => {
    expect(runListQuery(DEFAULT_RUN_FILTERS, NOW)).toEqual({});
  });

  test("时间预设：近 7 天、近 30 天按当前时间往回推；全部不带 since", () => {
    expect(runListQuery(filters({ range: "7d" }), NOW).since).toBe("2026-10-15T10:30:00.000Z");
    expect(runListQuery(filters({ range: "30d" }), NOW).since).toBe("2026-09-22T10:30:00.000Z");
    expect(runListQuery(filters({ range: "all" }), NOW).since).toBeUndefined();
  });

  test("时间预设：今天是本地时区的当天 0 点", () => {
    const since = runListQuery(filters({ range: "today" }), NOW).since as string;
    const d = new Date(since);
    expect([d.getHours(), d.getMinutes(), d.getSeconds(), d.getMilliseconds()]).toEqual([
      0, 0, 0, 0,
    ]);
    const now = new Date(NOW);
    expect(d.getDate()).toBe(now.getDate());
    expect(d.getTime()).toBeLessThanOrEqual(NOW);
  });

  test("状态：失败类展开成逗号分隔的多个状态，等待中同理", () => {
    expect(runListQuery(filters({ status: "failed" }), NOW).status).toBe(
      "failed,timeout,interrupted,step_limit,budget_exhausted",
    );
    expect(runListQuery(filters({ status: "waiting" }), NOW).status).toBe(
      "waiting_approval,waiting_input",
    );
    expect(runListQuery(filters({ status: "succeeded" }), NOW).status).toBe("succeeded");
    expect(runListQuery(filters({ status: "canceled" }), NOW).status).toBe("canceled");
    expect(runListQuery(filters({ status: "all" }), NOW).status).toBeUndefined();
  });

  test("反馈：点赞 / 点踩 / 未评价对应 up / down / none；全部不带", () => {
    expect(runListQuery(filters({ feedback: "up" }), NOW).feedback).toBe("up");
    expect(runListQuery(filters({ feedback: "down" }), NOW).feedback).toBe("down");
    expect(runListQuery(filters({ feedback: "none" }), NOW).feedback).toBe("none");
    expect(runListQuery(filters({ feedback: "all" }), NOW).feedback).toBeUndefined();
  });

  test("仅看被撤销：开了才带 undone=true，关了不带", () => {
    expect(runListQuery(filters({ undone: true }), NOW).undone).toBe(true);
    expect("undone" in runListQuery(filters({ undone: false }), NOW)).toBe(false);
  });

  test("Harness、模型、用户 ID 原样带上；用户 ID 转数字，非法的忽略", () => {
    const q = runListQuery(filters({ harnessKey: "a1b2", modelKey: "gpt", userId: "42" }), NOW);
    expect(q).toMatchObject({ harness_key: "a1b2", model_key: "gpt", user_id: 42 });
    expect("user_id" in runListQuery(filters({ userId: "abc" }), NOW)).toBe(false);
    expect("user_id" in runListQuery(filters({ userId: "0" }), NOW)).toBe(false);
    expect("user_id" in runListQuery(filters({ userId: "" }), NOW)).toBe(false);
  });

  test("查询参数里永远没有 cursor：任一筛选变化都回到第一页，翻页游标由分页 hook 追加", () => {
    const q = runListQuery(filters({ status: "failed", undone: true, userId: "3" }), NOW);
    expect("cursor" in q).toBe(false);
  });
});

describe("用户 ID 输入", () => {
  test("接受纯数字和 #前缀，去掉空格和前导 0", () => {
    expect(normalizeUserIdInput(" 42 ")).toBe("42");
    expect(normalizeUserIdInput("#42")).toBe("42");
    expect(normalizeUserIdInput("007")).toBe("7");
  });
  test("非数字、0、负数、小数一律视为空", () => {
    for (const text of ["", "abc", "0", "-3", "1.5", "4 2"])
      expect(normalizeUserIdInput(text)).toBe("");
  });
});

describe("筛选状态", () => {
  test("hasActiveRunFilters：默认没有；任何一项改动都算", () => {
    expect(hasActiveRunFilters(DEFAULT_RUN_FILTERS)).toBe(false);
    for (const patch of [
      { range: "7d" },
      { status: "failed" },
      { feedback: "down" },
      { undone: true },
      { harnessKey: "x" },
      { modelKey: "x" },
      { userId: "1" },
    ] as Partial<RunFilters>[])
      expect(hasActiveRunFilters(filters(patch))).toBe(true);
  });

  test("点版本卡片：按该 harness_key 筛选，再点同一张取消", () => {
    const on = toggleHarnessFilter(DEFAULT_RUN_FILTERS, "a1b2");
    expect(on.harnessKey).toBe("a1b2");
    expect(toggleHarnessFilter(on, "a1b2").harnessKey).toBe("");
    expect(toggleHarnessFilter(on, "zz").harnessKey).toBe("zz");
  });
});

describe("状态", () => {
  test("成功绿、失败类红、预算与步数用尽黄、停止中性、等待蓝", () => {
    expect(runStatusView("succeeded")).toEqual({ label: "成功", tone: "success" });
    for (const s of ["failed", "timeout", "interrupted"])
      expect(runStatusView(s).tone).toBe("danger");
    for (const s of ["step_limit", "budget_exhausted"])
      expect(runStatusView(s).tone).toBe("warning");
    expect(runStatusView("canceled")).toEqual({ label: "已停止", tone: "neutral" });
    for (const s of ["waiting_approval", "waiting_input"])
      expect(runStatusView(s).tone).toBe("info");
  });
  test("每个已知状态都有中文名；未知状态原样显示、中性色", () => {
    expect(runStatusView("failed").label).toBe("失败");
    expect(runStatusView("timeout").label).toBe("超时");
    expect(runStatusView("waiting_approval").label).toBe("等待审批");
    expect(runStatusView("brand_new")).toEqual({ label: "brand_new", tone: "neutral" });
  });
});

describe("反馈原因", () => {
  test("五个标签有中文", () => {
    expect(feedbackReasonLabel("misread")).toBe("没理解我的需求");
    expect(feedbackReasonLabel("wrong_edit")).toBe("画布改错了");
    expect(feedbackReasonLabel("slow")).toBe("步骤太多或太慢");
    expect(feedbackReasonLabel("bad_output")).toBe("生成结果不好");
    expect(feedbackReasonLabel("other")).toBe("其他");
  });
  test("不认识的标签原样返回", () => {
    expect(feedbackReasonLabel("whatever")).toBe("whatever");
  });
});

describe("Harness / 模型下拉选项", () => {
  test("harness 名：v{版本} · key 前 6 位", () => {
    expect(harnessLabel(10, "a1b2c3d4e5f6")).toBe("v10 · a1b2c3");
  });
  test("选项首项是“全部”，之后按 stats 的顺序", () => {
    const opts = harnessOptions([group(), group({ key: "ffeeddccbbaa", prompt_version: 9 })]);
    expect(opts).toEqual([
      ["", "全部"],
      ["a1b2c3d4e5f6", "v10 · a1b2c3"],
      ["ffeeddccbbaa", "v9 · ffeedd"],
    ]);
    expect(modelOptions([group({ key: "gpt-x" })])).toEqual([
      ["", "全部"],
      ["gpt-x", "gpt-x"],
    ]);
  });
  test("当前选中的值不在 stats 里（比如时间窗外的老版本）也要保留，下拉不会显示成空", () => {
    expect(modelOptions([], "old-model")).toEqual([
      ["", "全部"],
      ["old-model", "old-model"],
    ]);
    expect(harnessOptions([], "a1b2c3d4e5f6")).toEqual([
      ["", "全部"],
      ["a1b2c3d4e5f6", "a1b2c3"],
    ]);
  });
});

describe("统计时间窗", () => {
  test("近 30 天", () => {
    expect(statsSince(NOW)).toBe("2026-09-22T10:30:00.000Z");
  });
});

describe("比率显示", () => {
  test("0–1 小数显示成一位小数的百分比", () => {
    expect(formatRate(0.1234)).toBe("12.3%");
    expect(formatRate(0)).toBe("0.0%");
    expect(formatRate(1)).toBe("100.0%");
  });
});

describe("版本对比：样本不足与涨跌", () => {
  test("样本阈值是 30，持平阈值是 2 个百分点", () => {
    expect(MIN_SAMPLE).toBe(30);
    expect(FLAT_THRESHOLD_PP).toBe(2);
  });

  test("n<30 标样本不足，且不给任何涨跌", () => {
    const cards = buildVersionCards([group({ n: 29, key: "new" }), group({ n: 80, key: "old" })]);
    expect(cards[0]!.insufficient).toBe(true);
    expect(cards[0]!.deltas).toBeNull();
    expect(cards[1]!.insufficient).toBe(false);
  });

  test("n=30 刚好够", () => {
    const cards = buildVersionCards([group({ n: 30 })]);
    expect(cards[0]!.insufficient).toBe(false);
  });

  test("较旧一张样本不足：较新这张也不给涨跌", () => {
    const cards = buildVersionCards([group({ n: 80, key: "new" }), group({ n: 10, key: "old" })]);
    expect(cards[0]!.insufficient).toBe(false);
    expect(cards[0]!.deltas).toBeNull();
  });

  test("只取前 3 组；最旧的一张没有对比对象，没有涨跌", () => {
    const cards = buildVersionCards([
      group({ key: "k1" }),
      group({ key: "k2" }),
      group({ key: "k3" }),
      group({ key: "k4" }),
    ]);
    expect(cards.map((c) => c.group.key)).toEqual(["k1", "k2", "k3"]);
    expect(cards[2]!.deltas).toBeNull();
    expect(cards[0]!.deltas).not.toBeNull();
  });

  test("没有数据：没有卡片", () => {
    expect(buildVersionCards([])).toEqual([]);
  });

  test("相邻两张都够样本：较新的相对较旧的给出点踩率、撤销率、失败结束率三项变化", () => {
    const cards = buildVersionCards([
      group({ key: "new", down_rate: 0.2, undo_rate: 0.25, fail_end_rate: 0.05 }),
      group({ key: "old", down_rate: 0.38, undo_rate: 0.1, fail_end_rate: 0.05 }),
    ]);
    const deltas = cards[0]!.deltas!;
    expect(deltas.map((d) => d.metric)).toEqual(["down_rate", "undo_rate", "fail_end_rate"]);
    expect(deltas[0]).toMatchObject({ diffPp: -18, verdict: "better" });
    expect(deltas[1]).toMatchObject({ diffPp: 15, verdict: "worse" });
    expect(deltas[2]).toMatchObject({ diffPp: 0, verdict: "flat" });
  });

  test("持平边界：差距不足 2 个百分点算持平，刚好 2 个百分点就算变化", () => {
    const flat = compareVersions(
      group({ down_rate: 0.319, undo_rate: 0.101, fail_end_rate: 0.1 }),
      group({ down_rate: 0.3, undo_rate: 0.12, fail_end_rate: 0.08 }),
    )!;
    expect(flat[0]!.verdict).toBe("flat"); // +1.9
    expect(flat[1]!.verdict).toBe("flat"); // -1.9
    expect(flat[2]).toMatchObject({ diffPp: 2, verdict: "worse" });
    const better = compareVersions(group({ down_rate: 0.1 }), group({ down_rate: 0.12 }))!;
    expect(better[0]).toMatchObject({ diffPp: -2, verdict: "better" });
  });

  test("浮点误差不影响边界：0.12 − 0.10 按 2 个百分点算", () => {
    const d = compareVersions(group({ down_rate: 0.12 }), group({ down_rate: 0.1 }))!;
    expect(d[0]).toMatchObject({ diffPp: 2, verdict: "worse" });
  });

  test("任一边样本不足，compareVersions 返回 null", () => {
    expect(compareVersions(group({ n: 5 }), group())).toBeNull();
    expect(compareVersions(group(), group({ n: 5 }))).toBeNull();
  });
});

describe("列表行的小徽标", () => {
  test("没有信号：没有徽标", () => {
    expect(runRowBadges(digest())).toEqual([]);
  });
  test("工具报错、插话、审批被拒大于 0 才出现，title 说明数量", () => {
    const badges = runRowBadges(digest({ tool_errors: 2, steer_count: 1, approvals_rejected: 3 }));
    expect(badges.map((b) => b.key)).toEqual(["tool_errors", "steer", "rejected"]);
    expect(badges[0]!.title).toContain("2");
    expect(badges[1]!.title).toContain("1");
    expect(badges[2]!.title).toContain("3");
  });
});

describe("页码分页", () => {
  test("总页数：向上取整，没有数据也至少 1 页", () => {
    expect(pageCount(0, 20)).toBe(1);
    expect(pageCount(20, 20)).toBe(1);
    expect(pageCount(21, 20)).toBe(2);
    expect(pageCount(100, 30)).toBe(4);
  });
  test("页码修正：落在 1 到总页数之间（筛选或数据变少后当前页可能越界）", () => {
    expect(clampPage(0, 50, 20)).toBe(1);
    expect(clampPage(2, 50, 20)).toBe(2);
    expect(clampPage(9, 50, 20)).toBe(3);
    expect(clampPage(5, 0, 20)).toBe(1);
  });
  test("列表查询带上页码和每页条数", () => {
    expect(runListQuery(DEFAULT_RUN_FILTERS, NOW, { page: 3, pageSize: 30 })).toMatchObject({
      page: 3,
      limit: 30,
    });
    expect(runListQuery(DEFAULT_RUN_FILTERS, NOW)).toEqual({});
  });
});

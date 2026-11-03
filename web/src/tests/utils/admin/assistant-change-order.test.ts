import { describe, expect, test } from "bun:test";

import type {
  AdminChangeItem,
  AdminChangePayload,
  AdminChangeResult,
  AgentApprovalDto,
} from "@/api/agent/type";
import {
  adminChangePayload,
  adminChangeResult,
  adminChangeTitle,
  basisRows,
  buildApproveDecision,
  buildRejectDecision,
  canRollback,
  clock,
  defaultSelection,
  formatDiffValue,
  isOrderActionable,
  isPickable,
  itemOutcome,
  orderPhase,
  resultCounts,
  resultSummary,
  rollbackMessage,
  selectedCount,
  toggleSelection,
  resolveSelection,
} from "@/utils/admin/assistant/change-order";

const item = (over: Partial<AdminChangeItem> = {}): AdminChangeItem => ({
  target: "model",
  key: "kling-v2-1",
  label: "可灵 2.1",
  live: true,
  status: "ok",
  issues: [],
  warnings: [],
  required_role: "admin",
  diff: [{ path: "pricing.per_second", before: 8, after: 10 }],
  basis: [],
  ...over,
});

const payload = (items: AdminChangeItem[], over: Partial<AdminChangePayload> = {}) => ({
  order_no: "CO-251102-7F3A",
  title: "视频模型按成本加价 30%",
  reason: "用户要求",
  items,
  ...over,
});

const approval = (
  over: Partial<AgentApprovalDto> = {},
  items: AdminChangeItem[] = [item()],
): AgentApprovalDto => ({
  id: "ap1",
  run_id: "r1",
  kind: "admin_change",
  status: "pending",
  payload: payload(items),
  quote_credits: 0,
  decision: null,
  result: null,
  expires_at: "2026-11-03T00:00:00Z",
  ...over,
});

const result = (items: AdminChangeResult["items"]): AdminChangeResult => ({
  items,
  applied_by: "zhang",
  applied_at: "2026-11-02T06:00:00Z",
  audit_ids: [11, 12],
});

describe("二期变更单：依赖勾选联动", () => {
  const items = [
    item({ key: "plugin", target: "plugin", required_role: "super_admin" }),
    item({ key: "channel", target: "channel", depends_on: [0] }),
    item({ key: "model", target: "model_create", depends_on: [1] }),
  ];

  test("角色不足会连带取消全部下游条目", () => {
    expect([...defaultSelection(items, "admin")]).toEqual([]);
    expect(selectedCount(items, "admin", new Set([0, 1, 2]))).toBe(0);
    expect(
      buildApproveDecision(items, "admin", new Set([0, 1, 2])).items?.map((x) => x.approve),
    ).toEqual([false, false, false]);
  });

  test("取消依赖项递归清除下游；重新选依赖不会替用户勾选下游", () => {
    const initial = defaultSelection(items, "super_admin");
    const next = toggleSelection(initial, 0, items, "super_admin");
    expect([...next]).toEqual([]);
    expect([...initial]).toEqual([0, 1, 2]);
    expect([...toggleSelection(next, 2, items, "super_admin")]).toEqual([]);
    expect([...toggleSelection(next, 0, items, "super_admin")]).toEqual([0]);
  });

  test("不依赖条目顺序；缺失的依赖不可批准", () => {
    const reversed = [
      item({ depends_on: [1] }),
      item({ depends_on: [2] }),
      item({ status: "invalid" }),
    ];
    expect([...resolveSelection(reversed, "super_admin", new Set([0, 1, 2]))]).toEqual([]);
    expect([...defaultSelection([item({ depends_on: [5] })], "super_admin")]).toEqual([]);
  });

  test("审批解析保留新 target、depends_on 和 detail", () => {
    const detail = { kind: "video", channel: "c", upstream_model: "v1", plugin: "p@latest" };
    const parsed = adminChangePayload(
      approval({}, [item({ target: "model_create", depends_on: [0], detail })]),
    );
    expect(parsed?.items[0]).toMatchObject({ target: "model_create", depends_on: [0], detail });
  });
});

describe("变更单：条目能不能勾选", () => {
  test("status 是 ok 且角色够才能勾选", () => {
    expect(isPickable(item(), "admin")).toBe(true);
    expect(isPickable(item({ status: "invalid" }), "admin")).toBe(false);
    expect(isPickable(item({ status: "stale" }), "super_admin")).toBe(false);
    expect(isPickable(item({ status: "mismatch" }), "super_admin")).toBe(false);
  });

  test("角色不够的条目不能勾选：admin 不能确认 super_admin 的条目，super_admin 都可以", () => {
    const sup = item({ required_role: "super_admin" });
    expect(isPickable(sup, "admin")).toBe(false);
    expect(isPickable(sup, "super_admin")).toBe(true);
    expect(isPickable(item(), null)).toBe(false);
  });
});

describe("变更单：勾选与计数", () => {
  const items = [
    item({ key: "a" }),
    item({ key: "b", status: "invalid", issues: ["缺少渠道"] }),
    item({ key: "c" }),
    item({ key: "d", diff: [{ path: "pricing.unit", before: 2, after: 2 }] }),
    item({ key: "e", required_role: "super_admin" }),
  ];

  test("默认勾选所有能勾选的条目；所有字段改前改后都相同的（如取整后未变化）默认不勾选", () => {
    expect([...defaultSelection(items, "admin")].sort()).toEqual([0, 2]);
    expect([...defaultSelection(items, "super_admin")].sort()).toEqual([0, 2, 4]);
  });

  test("默认勾选只看 diff，不看提醒文案；对象值按内容比较", () => {
    const list = [
      item({ key: "w", warnings: ["取整后未变化"] }),
      item({
        key: "o",
        diff: [
          {
            path: "capabilities.params.duration",
            before: { options: [5, 10] },
            after: { options: [5, 10] },
          },
        ],
      }),
      item({
        key: "p",
        diff: [
          {
            path: "capabilities.params.duration",
            before: { options: [5] },
            after: { options: [5, 10] },
          },
        ],
      }),
    ];
    expect([...defaultSelection(list, "admin")].sort()).toEqual([0, 2]);
  });

  test("已选计数只算能勾选的条目", () => {
    expect(selectedCount(items, "admin", new Set([0, 1, 2, 4]))).toBe(2);
    expect(selectedCount(items, "admin", new Set())).toBe(0);
  });

  test("切换勾选返回新集合，不改原集合", () => {
    const before = new Set([0]);
    const added = toggleSelection(before, 2);
    expect([...added].sort()).toEqual([0, 2]);
    expect([...before]).toEqual([0]);
    expect([...toggleSelection(added, 0)]).toEqual([2]);
  });
});

describe("变更单：决定请求体", () => {
  const items = [item({ key: "a" }), item({ key: "b", status: "invalid" }), item({ key: "c" })];

  test("逐条勾选：每一项都带 index 和 approve，没勾的、不能勾的 approve 为 false", () => {
    expect(buildApproveDecision(items, "admin", new Set([0]))).toEqual({
      decision: "approve",
      items: [
        { index: 0, approve: true },
        { index: 1, approve: false },
        { index: 2, approve: false },
      ],
    });
  });

  test("勾选集合里混进了不能勾选的条目，也不会批准它", () => {
    const req = buildApproveDecision(items, "admin", new Set([0, 1, 2]));
    expect(req.items?.map((x) => x.approve)).toEqual([true, false, true]);
  });

  test("拒绝带理由；理由去首尾空格，空的不带 answer", () => {
    expect(buildRejectDecision("  价格偏低 ")).toEqual({ decision: "reject", answer: "价格偏低" });
    expect(buildRejectDecision("   ")).toEqual({ decision: "reject" });
  });

  test("回滚发一条普通消息", () => {
    expect(rollbackMessage("CO-251102-7F3A")).toBe("回滚变更单 CO-251102-7F3A");
  });
});

describe("变更单：读审批", () => {
  test("读出 admin_change 的内容；其他种类、没有条目的返回 null", () => {
    expect(adminChangePayload(approval())?.order_no).toBe("CO-251102-7F3A");
    expect(adminChangePayload(approval({ kind: "edit" }))).toBeNull();
    expect(adminChangePayload(approval({ payload: { order_no: "x" } as never }))).toBeNull();
  });

  test("条目里缺的数组字段补成空数组，不让坏数据把消息流弄崩", () => {
    const broken = {
      ...item(),
      issues: undefined,
      warnings: undefined,
      basis: undefined,
      diff: undefined,
    };
    const got = adminChangePayload(approval({ payload: payload([broken as never]) as never }));
    expect(got?.items[0]).toMatchObject({ issues: [], warnings: [], basis: [], diff: [] });
  });

  test("读执行结果：执行前是 null，形状不对也是 null", () => {
    expect(adminChangeResult(approval())).toBeNull();
    expect(adminChangeResult(approval({ result: {} }))).toBeNull();
    const ok = result([{ index: 0, status: "applied" }]);
    expect(adminChangeResult(approval({ result: ok }))?.applied_by).toBe("zhang");
  });

  test("条目的执行结果按下标找", () => {
    const r = result([
      { index: 0, status: "applied" },
      { index: 2, status: "failed", message: "审核期间配置已被他人修改，未覆盖" },
    ]);
    expect(itemOutcome(r, 0)?.status).toBe("applied");
    expect(itemOutcome(r, 2)?.message).toContain("他人修改");
    expect(itemOutcome(r, 1)).toBeNull();
    expect(itemOutcome(null, 0)).toBeNull();
  });

  test("统计已应用 / 失败 / 跳过的条数", () => {
    const r = result([
      { index: 0, status: "applied" },
      { index: 1, status: "applied" },
      { index: 2, status: "failed" },
      { index: 3, status: "skipped" },
      { index: 4, status: "rejected" },
    ]);
    expect(resultCounts(r)).toEqual({ applied: 2, failed: 1, skipped: 1 });
  });
});

describe("变更单：整单状态与回滚", () => {
  test("管理操作不显示自动回滚入口，保留动作供审批展示", () => {
    for (const target of [
      "user",
      "register_settings",
      "smtp_settings",
      "storage",
      "image_processor",
    ] as const) {
      const managed = approval(
        { status: "executed", result: result([{ index: 0, status: "applied" }]) },
        [item({ target, action: "delete" })],
      );
      expect(canRollback(managed)).toBe(false);
      expect(adminChangePayload(managed)?.items[0]?.action).toBe("delete");
    }
  });
  test("按审批状态给出整单状态", () => {
    expect(orderPhase(approval())).toBe("pending");
    expect(orderPhase(approval({ status: "executed" }))).toBe("applied");
    expect(orderPhase(approval({ status: "partially_approved" }))).toBe("partial");
    expect(orderPhase(approval({ status: "failed" }))).toBe("failed");
    expect(orderPhase(approval({ status: "rejected" }))).toBe("rejected");
    expect(orderPhase(approval({ status: "expired" }))).toBe("expired");
    expect(orderPhase(approval({ status: "approved" }))).toBe("applying");
  });

  test("有已应用的条目才能回滚；回滚单本身不再提供回滚", () => {
    const applied = result([{ index: 0, status: "applied" }]);
    expect(canRollback(approval({ status: "executed", result: applied }))).toBe(true);
    expect(
      canRollback(approval({ status: "failed", result: result([{ index: 0, status: "failed" }]) })),
    ).toBe(false);
    expect(canRollback(approval({ status: "pending" }))).toBe(false);
    const revert = approval({
      status: "executed",
      result: applied,
      payload: payload([item()], { revert_of: "CO-1" }) as never,
    });
    expect(canRollback(revert)).toBe(false);
  });
});

describe("变更单：值的显示", () => {
  test("没有的值显示破折号，字符串原样，数字原样，数组和对象紧凑 JSON", () => {
    expect(formatDiffValue(null)).toBe("—");
    expect(formatDiffValue(undefined)).toBe("—");
    expect(formatDiffValue(10)).toBe("10");
    expect(formatDiffValue("秒")).toBe("秒");
    expect(formatDiffValue(true)).toBe("true");
    expect(formatDiffValue([5, 10])).toBe("[5,10]");
    expect(formatDiffValue({ a: 1 })).toBe('{"a":1}');
  });
});

describe("变更单：计算依据", () => {
  test("成本加价：上游报价、来源链接、汇率、换算、成本、加价率、售价", () => {
    const rows = basisRows({
      path: "pricing.per_second",
      mode: "cost",
      upstream: {
        amount: 0.14,
        currency: "USD",
        unit: "秒",
        source_url: "https://klingai.com/pricing",
      },
      fx: { rate: 6.6529, date: "2026-10-30", source: "live", stale: true },
      credits_per_yuan: 7.5,
      yuan: 0.9314,
      cost: 7,
      markup_pct: 30,
      price: 10,
    });
    expect(rows.map((r) => [r.label, r.value])).toEqual([
      ["上游报价", "0.14 USD / 秒"],
      ["来源", "https://klingai.com/pricing"],
      ["汇率", "USD/CNY 6.6529 · 2026-10-30 · 实时 · 汇率可能过期"],
      ["积分换算", "1 元 = 7.5 积分"],
      ["人民币", "¥0.9314"],
      ["成本", "7 积分"],
      ["加价率", "30%"],
      ["售价", "10 积分"],
    ]);
    expect(rows[1].href).toBe("https://klingai.com/pricing");
  });

  test("来源不是 http(s) 链接时不做成链接", () => {
    const rows = basisRows({
      path: "p",
      mode: "cost",
      upstream: { amount: 1, currency: "CNY", unit: "次", source_url: "javascript:alert(1)" },
      price: 2,
    });
    expect(rows.find((r) => r.label === "来源")?.href).toBeUndefined();
  });

  test("相对调价：原价、调整、售价；负数不加号", () => {
    expect(basisRows({ path: "p", mode: "relative", from: 2, pct: 10, price: 3 })).toEqual([
      { label: "原价", value: "2 积分" },
      { label: "调整", value: "+10%" },
      { label: "售价", value: "3 积分" },
    ]);
    expect(basisRows({ path: "p", mode: "relative", from: 20, pct: -10, price: 18 })[1].value).toBe(
      "-10%",
    );
  });
});

describe("变更单：作废的单只读", () => {
  test("待决定且运行还在等它才能决定；运行已结束的待决定单是作废的", () => {
    expect(isOrderActionable(approval(), "waiting_approval")).toBe(true);
    expect(isOrderActionable(approval(), undefined)).toBe(true);
    expect(isOrderActionable(approval(), "canceled")).toBe(false);
    expect(isOrderActionable(approval(), "succeeded")).toBe(false);
    expect(isOrderActionable(approval({ status: "executed" }), "waiting_approval")).toBe(false);
  });
});

describe("变更单：记录行的标题与结果摘要", () => {
  test("标题优先，没有标题用单号，读不出来叫「变更单」", () => {
    expect(adminChangeTitle(approval())).toBe("视频模型按成本加价 30%");
    expect(adminChangeTitle(approval({ payload: payload([item()], { title: "" }) as never }))).toBe(
      "变更单 CO-251102-7F3A",
    );
    expect(adminChangeTitle(approval({ payload: {} as never }))).toBe("变更单");
  });

  test("摘要：已应用总写；失败、跳过为 0 不写；带操作人和时间", () => {
    const text = resultSummary(
      approval({
        status: "partially_approved",
        result: result([
          { index: 0, status: "applied" },
          { index: 1, status: "failed" },
          { index: 2, status: "skipped" },
          { index: 3, status: "applied" },
        ]),
      }),
    );
    expect(text).toBe(
      `已应用 2 项 · 失败 1 项 · 跳过 1 项 · zhang · ${clock(Date.parse("2026-11-02T06:00:00Z"))}`,
    );
    const clean = resultSummary(
      approval({ status: "executed", result: result([{ index: 0, status: "applied" }]) }),
    );
    expect(clean).toBe(`已应用 1 项 · zhang · ${clock(Date.parse("2026-11-02T06:00:00Z"))}`);
  });

  test("还没有执行结果：没有摘要", () => {
    expect(resultSummary(approval())).toBeNull();
  });

  test("clock 是两位的 HH:mm", () => {
    expect(clock(new Date(2026, 10, 2, 7, 5).getTime())).toBe("07:05");
  });
});

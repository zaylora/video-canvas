import { describe, expect, test } from "bun:test";

import { mapLedgerPage } from "@/api/me";
import type { LedgerItemDto } from "@/api/me/type";
import {
  formatLedgerTime,
  LEDGER_PAGE_SIZES,
  lastPageOf,
  ledgerAmount,
  ledgerSubtitle,
  LEDGER_TYPE_LABEL,
  pageList,
  parsePageParam,
  parsePageSize,
} from "@/utils/profile/ledger";

/** 造一条流水 */
const item = (part: Partial<LedgerItemDto>): LedgerItemDto => ({
  id: "1",
  type: "settle",
  amount: 4,
  taskId: null,
  agentCallId: null,
  note: "",
  createdAt: "2026-10-09T06:20:00Z",
  ...part,
});

describe("页码列表", () => {
  test("不超过 7 页全部显示", () => {
    expect(pageList(1, 1)).toEqual([1]);
    expect(pageList(3, 7)).toEqual([1, 2, 3, 4, 5, 6, 7]);
  });
  test("中间页：首末常显，两侧用省略号折叠", () => {
    expect(pageList(6, 12)).toEqual([1, "…", 5, 6, 7, "…", 12]);
  });
  test("靠近首页或末页时只在一侧折叠", () => {
    expect(pageList(1, 12)).toEqual([1, 2, 3, 4, 5, "…", 12]);
    expect(pageList(12, 12)).toEqual([1, "…", 8, 9, 10, 11, 12]);
  });
  test("省略号只省一页时，直接显示那一页", () => {
    expect(pageList(4, 12)).toEqual([1, 2, 3, 4, 5, "…", 12]);
    expect(pageList(9, 12)).toEqual([1, "…", 8, 9, 10, 11, 12]);
  });
  test("页码最多 7 个位置（含省略号）", () => {
    for (let cur = 1; cur <= 30; cur += 1) expect(pageList(cur, 30).length).toBeLessThanOrEqual(7);
  });
  test("总页数：至少 1 页", () => {
    expect(lastPageOf(0, 20)).toBe(1);
    expect(lastPageOf(20, 20)).toBe(1);
    expect(lastPageOf(21, 20)).toBe(2);
  });
});

describe("URL 参数", () => {
  test("page 只接受正整数，其余回到 1", () => {
    expect(parsePageParam("3")).toBe(3);
    expect(parsePageParam(null)).toBe(1);
    expect(parsePageParam("0")).toBe(1);
    expect(parsePageParam("-2")).toBe(1);
    expect(parsePageParam("2.5")).toBe(1);
    expect(parsePageParam("abc")).toBe(1);
  });
  test("每页条数只有 10 / 20 / 50，默认 20", () => {
    expect(LEDGER_PAGE_SIZES).toEqual([10, 20, 50]);
    expect(parsePageSize("50")).toBe(50);
    expect(parsePageSize("30")).toBe(20);
    expect(parsePageSize(null)).toBe(20);
  });
});

describe("金额按余额变化展示", () => {
  test("结算是真实扣减：−n，正文色", () => {
    expect(ledgerAmount(item({ type: "settle", amount: 30 }))).toEqual({
      text: "−30",
      tone: "default",
    });
  });
  test("冻结、解冻不改变余额：灰色、不带正负号", () => {
    expect(ledgerAmount(item({ type: "freeze", amount: 30 }))).toEqual({
      text: "冻结 30",
      tone: "muted",
    });
    expect(ledgerAmount(item({ type: "refund", amount: 4 }))).toEqual({
      text: "解冻 4",
      tone: "muted",
    });
  });
  test("管理员调整：加为绿色 +n，减为正文色 −n", () => {
    expect(ledgerAmount(item({ type: "admin_adjust", amount: 200 }))).toEqual({
      text: "+200",
      tone: "positive",
    });
    expect(ledgerAmount(item({ type: "admin_adjust", amount: -30 }))).toEqual({
      text: "−30",
      tone: "default",
    });
  });
  test("初始积分：+n 绿色", () => {
    expect(ledgerAmount(item({ type: "initial", amount: 50 }))).toEqual({
      text: "+50",
      tone: "positive",
    });
  });
});

describe("流水副标题", () => {
  test("关联任务显示 #id，Agent 调用显示「Agent 对话」", () => {
    expect(ledgerSubtitle(item({ taskId: "3120" }))).toEqual({ text: "任务 #3120", note: false });
    expect(ledgerSubtitle(item({ agentCallId: "9" }))).toEqual({ text: "Agent 对话", note: false });
  });
  test("管理员调整显示备注原文，标记为备注（一行截断、悬停看全文）", () => {
    expect(
      ledgerSubtitle(item({ type: "admin_adjust", amount: 200, note: "<b>国庆活动补发</b>" })),
    ).toEqual({ text: "<b>国庆活动补发</b>", note: true });
  });
  test("初始积分显示注册赠送", () => {
    expect(ledgerSubtitle(item({ type: "initial", amount: 50 }))).toEqual({
      text: "注册赠送",
      note: false,
    });
  });
  test("类型名称", () => {
    expect(LEDGER_TYPE_LABEL.admin_adjust).toBe("管理员调整");
    expect(LEDGER_TYPE_LABEL.settle).toBe("生成结算");
  });
});

describe("流水接口映射", () => {
  test("items 为 null 兜底成空数组；0 表示没有关联；不带操作人", () => {
    const page = mapLedgerPage({ items: null, total: 41, page: 9, page_size: 20 });
    expect(page).toEqual({ items: [], total: 41, page: 9, pageSize: 20 });
    const one = mapLedgerPage({
      items: [
        {
          id: 7,
          type: "admin_adjust",
          amount: -30,
          task_id: 0,
          agent_call_id: null,
          note: "收回",
          created_at: "2026-10-09T06:20:00Z",
        },
      ],
      total: 1,
      page: 1,
      page_size: 20,
    }).items[0];
    expect(one).toEqual({
      id: "7",
      type: "admin_adjust",
      amount: -30,
      taskId: null,
      agentCallId: null,
      note: "收回",
      createdAt: "2026-10-09T06:20:00Z",
    });
  });
});

describe("流水时间", () => {
  test("按浏览器时区格式化成 YYYY-MM-DD HH:mm，解析失败原样返回", () => {
    expect(formatLedgerTime("2026-10-09T06:20:00Z")).toMatch(/^2026-10-(09|10) \d{2}:20$/);
    expect(formatLedgerTime("bad")).toBe("bad");
  });
});

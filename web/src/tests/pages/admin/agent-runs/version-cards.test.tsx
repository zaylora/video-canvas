import { describe, expect, test } from "bun:test";
import { renderToStaticMarkup } from "react-dom/server";

import type { AgentStatsGroup } from "@/api/admin/agent-run/type.d";
import { VersionCards } from "@/pages/admin/agent-runs/version-cards";
import type { StatsStatus } from "@/pages/admin/agent-runs/use-agent-stats";

import { group } from "./fixtures";

const NOW = Date.parse("2026-10-22T10:00:00Z");
const noop = () => undefined;
const cards = (status: StatsStatus, groups: AgentStatsGroup[] = [], selectedKey = "") =>
  renderToStaticMarkup(
    <VersionCards
      status={status}
      groups={groups}
      selectedKey={selectedKey}
      now={NOW}
      onSelect={noop}
      onRetry={noop}
    />,
  );

describe("版本对比卡片：各状态", () => {
  test("加载中：三张骨架", () => {
    const out = cards("loading");
    expect(out.match(/data-slot="skeleton"/g)).toHaveLength(3);
    expect(out).toContain('aria-busy="true"');
  });

  test("加载失败：只在卡片区给提示和重试", () => {
    const out = cards("error");
    expect(out).toContain("版本对比加载失败");
    expect(out).toContain("重试");
    expect(out).not.toContain('data-slot="version-card"');
  });

  test("没有数据：空状态", () => {
    expect(cards("ready", [])).toContain("最近 30 天还没有运行记录");
  });
});

describe("版本对比卡片：内容", () => {
  test("版本名、运行数、最近运行时间、六项指标", () => {
    const out = cards("ready", [
      group({
        n: 50,
        down_rate: 0.125,
        undo_rate: 0.1,
        fail_end_rate: 0.04,
        median_duration_ms: 42_000,
        avg_credits: 3.46,
        feedback_rate: 0.2,
        last_at: "2026-10-22T08:00:00Z",
      }),
    ]);
    expect(out).toContain("v10 · a1b2c3");
    expect(out).toContain("50 次运行");
    expect(out).toContain("2 小时前");
    for (const label of ["点踩率", "撤销率", "失败结束率", "中位耗时", "平均积分", "反馈率"])
      expect(out).toContain(label);
    expect(out).toContain("12.5%");
    expect(out).toContain("42s");
    expect(out).toContain("3.5");
    expect(out).toContain("20.0%");
  });

  test("只显示最近 3 张", () => {
    const out = cards(
      "ready",
      ["k1", "k2", "k3", "k4"].map((key) => group({ key: key + "aaaaaaaa" })),
    );
    expect(out.match(/data-slot="version-card"/g)).toHaveLength(3);
  });

  test("样本不足（n<30）：标「样本不足」，只有数值，没有涨跌", () => {
    const out = cards("ready", [group({ n: 12 }), group({ key: "old", n: 80 })]);
    expect(out).toContain("样本不足");
    expect(out).not.toContain('data-slot="delta-chip"');
  });

  test("相邻两张都够样本：较新的一张显示三项涨跌，降低为绿、升高为红、不足 2 个百分点为持平", () => {
    const out = cards("ready", [
      group({ key: "new", down_rate: 0.2, undo_rate: 0.25, fail_end_rate: 0.05 }),
      group({ key: "old", down_rate: 0.38, undo_rate: 0.1, fail_end_rate: 0.06 }),
    ]);
    const chips = out.match(/data-slot="delta-chip" data-verdict="(\w+)"/g);
    // 较新一张三项；较旧一张没有更旧的可比
    expect(chips).toEqual([
      'data-slot="delta-chip" data-verdict="better"',
      'data-slot="delta-chip" data-verdict="worse"',
      'data-slot="delta-chip" data-verdict="flat"',
    ]);
    expect(out).toContain("text-status-success");
    expect(out).toContain("text-destructive");
    expect(out).toContain("18 个百分点");
    expect(out).toContain("持平");
    expect(out).not.toContain("样本不足");
  });

  test("点选：选中的卡片 aria-pressed 为真，再点同一张取消（由上层切换）", () => {
    const groups = [group({ key: "k1aaaaaaaa" }), group({ key: "k2aaaaaaaa" })];
    const out = cards("ready", groups, "k2aaaaaaaa");
    expect(out.match(/aria-pressed="true"/g)).toHaveLength(1);
    expect(out.match(/aria-pressed="false"/g)).toHaveLength(1);
    expect(out).toContain("再点一次取消筛选");
  });
});

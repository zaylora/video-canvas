import { describe, expect, test } from "bun:test";
import { renderToStaticMarkup } from "react-dom/server";
import { MemoryRouter } from "react-router";

import type { ActivityDto } from "@/api/me/type";
import { ActivityHeatmap } from "@/components/profile/activity-heatmap";

const DATA: ActivityDto = {
  tz: "Asia/Shanghai",
  start: "2025-10-05",
  end: "2026-10-09",
  years: [2025, 2026],
  total: 14,
  days: [
    { date: "2026-10-08", count: 12, image: 8, video: 3, audio: 1, text: 0 },
    { date: "2026-10-09", count: 2, image: 2, video: 0, audio: 0, text: 0 },
  ],
};

const html = (data: ActivityDto, year: number | null = null) =>
  renderToStaticMarkup(
    <MemoryRouter>
      <ActivityHeatmap data={data} year={year} />
    </MemoryRouter>,
  );

describe("热力图组件", () => {
  test("是 ARIA grid，7 行，每格 aria-label 与 tooltip 文案一致", () => {
    const out = html(DATA);
    expect(out).toContain('role="grid"');
    expect(out.match(/role="row"/g)?.length).toBe(7);
    expect(out).toContain(
      'aria-label="2026年10月8日 星期四 · 12 次生成，图片 8 · 视频 3 · 音频 1"',
    );
    expect(out).toContain('aria-label="2026年10月7日 · 无生成"');
  });

  test("roving tabindex：只有最近一天可 Tab 聚焦", () => {
    const out = html(DATA);
    expect(out.match(/tabindex="0"/g)?.length).toBe(1);
    expect(out).toMatch(/<rect[^>]*tabindex="0"[^>]*data-date="2026-10-09"/);
  });

  test("未来日期不渲染", () => {
    expect(html(DATA)).not.toContain('data-date="2026-10-10"');
  });

  test("汇总行：最近一年显示当前连续；选了年份不显示", () => {
    const recent = html(DATA);
    expect(recent).toContain("过去一年共");
    expect(recent).toContain("活跃 2 天");
    expect(recent).toContain("当前连续 2 天");
    const year = html({ ...DATA, start: "2026-01-01", end: "2026-10-09" }, 2026);
    expect(year).toContain("2026 年共");
    expect(year).not.toContain("当前连续");
  });

  test("SVG 右侧留白：最后一列的月份标签不会被裁掉", () => {
    // 2026-10-09 是周五，最近一年的最后一列从 10-04（周日）开始，正好是 10 月的第一个周日，标签 "10月" 画在最后一列
    const out = html(DATA);
    const width = Number(out.match(/<svg[^>]*width="(\d+)"/)?.[1]);
    // 一年里 "10月" 会出现两次（第一列和最后一列），要取最后一个
    const labels = [...out.matchAll(/<text[^>]*x="(\d+)"[^>]*>10月<\/text>/g)];
    expect(labels.length).toBe(2);
    const labelX = Number(labels[labels.length - 1]![1]);
    // "10月" 在 10px 字号下约 20px 宽，右侧至少留出这么多，不然超出 SVG 的部分被裁掉
    expect(width - labelX).toBeGreaterThanOrEqual(20);
  });

  test("没有任何生成时提示去画布里试试", () => {
    const out = html({ ...DATA, total: 0, days: [] }).replace(/<[^>]+>/g, "");
    expect(out).toContain("还没有生成记录，去画布里试试");
  });
});

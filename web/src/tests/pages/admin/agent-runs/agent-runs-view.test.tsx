import { describe, expect, test } from "bun:test";
import { renderToStaticMarkup } from "react-dom/server";

import { AgentRunsView, type AgentRunsViewProps } from "@/pages/admin/agent-runs/agent-runs-view";
import { DEFAULT_RUN_FILTERS } from "@/utils/admin/agent-run";

import { digest, group } from "./fixtures";

const noop = () => undefined;
const view = (props: Partial<AgentRunsViewProps> = {}) =>
  renderToStaticMarkup(
    <AgentRunsView
      statsStatus="ready"
      harness={[group()]}
      models={[group({ key: "gpt-test" })]}
      filters={DEFAULT_RUN_FILTERS}
      filtersActive={false}
      listState="ready"
      items={[digest()]}
      page={1}
      pageSize={20}
      total={1}
      refreshing={false}
      activeRid={null}
      now={Date.parse("2026-10-22T10:00:00Z")}
      onSelectHarness={noop}
      onRetryStats={noop}
      onFilterChange={noop}
      onReset={noop}
      onOpen={noop}
      onPageChange={noop}
      onPageSizeChange={noop}
      onRetryList={noop}
      {...props}
    />,
  );

describe("运行记录页", () => {
  test("标题、版本对比、筛选栏、列表同屏", () => {
    const out = view();
    expect(out).toContain("运行记录");
    expect(out).toContain('data-slot="version-cards"');
    expect(out).toContain('data-slot="run-filter-bar"');
    expect(out).toContain('data-slot="run-table"');
  });

  test("版本对比加载失败：只在卡片区提示，列表照常显示", () => {
    const out = view({ statsStatus: "error", harness: [] });
    expect(out).toContain("版本对比加载失败");
    expect(out).toContain('data-slot="run-table"');
    expect(out).not.toContain("运行记录加载失败");
  });

  test("列表加载失败：只在列表区提示，版本对比照常显示", () => {
    const out = view({ listState: "error", items: [] });
    expect(out).toContain("运行记录加载失败");
    expect(out).toContain('data-slot="version-card"');
    expect(out).not.toContain("版本对比加载失败");
  });

  test("点卡片筛选的 harness 在卡片上高亮，并且在筛选栏的下拉里显示版本名", () => {
    const out = view({ filters: { ...DEFAULT_RUN_FILTERS, harnessKey: "a1b2c3d4e5f6" } });
    expect(out).toContain('aria-pressed="true"');
    expect(out).toContain(">v10 · a1b2c3</span>");
  });

  test("底部是页码分页：共 N 条、第 x / y 页，没有「加载更多」", () => {
    const out = view({ total: 45, page: 2, items: [digest()] });
    expect(out).toContain('data-slot="data-panel-footer"');
    expect(out).toContain("共 45 条");
    expect(out).toContain("第 2 / 3 页");
    expect(out).not.toContain("加载更多");
  });

  test("没有数据或加载失败时不显示分页条", () => {
    expect(view({ total: 0, items: [] })).not.toContain('data-slot="data-panel-footer"');
    expect(view({ listState: "error", total: 0, items: [] })).not.toContain(
      'data-slot="data-panel-footer"',
    );
  });

  test("切页时保留旧数据并半透明（refreshing）", () => {
    expect(view({ refreshing: true })).toContain("opacity-60");
    expect(view({ refreshing: false })).not.toContain("opacity-60");
  });
});

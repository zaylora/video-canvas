import { describe, expect, test } from "bun:test";
import { renderToStaticMarkup } from "react-dom/server";

import { RunFilterBar } from "@/pages/admin/agent-runs/run-filter-bar";
import { DEFAULT_RUN_FILTERS, type RunFilters } from "@/utils/admin/agent-run";

import { group } from "./fixtures";

const noop = () => undefined;
const bar = (filters: Partial<RunFilters> = {}, active = false) =>
  renderToStaticMarkup(
    <RunFilterBar
      filters={{ ...DEFAULT_RUN_FILTERS, ...filters }}
      active={active}
      harness={[group()]}
      models={[group({ key: "gpt-test" })]}
      onChange={noop}
      onReset={noop}
    />,
  );

describe("筛选栏", () => {
  test("七个筛选控件和重置按钮", () => {
    const out = bar();
    for (const label of [
      "时间范围",
      "状态",
      "反馈",
      "Harness 版本",
      "模型",
      "用户 ID",
      "仅看被撤销",
    ])
      expect(out).toContain(`aria-label="${label}"`);
    expect(out).toContain("重置筛选");
  });

  test("没有筛选在生效：重置按钮置灰；有则可点", () => {
    const resetButton = (out: string) =>
      /<button[^>]*>(?:<svg.*?<\/svg>)?重置筛选/.exec(out)?.[0] ?? "";
    expect(resetButton(bar({}, false))).toContain(' disabled=""');
    expect(resetButton(bar({ status: "failed" }, true))).not.toContain(' disabled=""');
  });

  test("用户 ID 输入框显示已生效的值", () => {
    expect(bar({ userId: "42" })).toMatch(/<input[^>]*value="42"/);
  });

  test("仅看被撤销：开关的选中状态跟随筛选", () => {
    expect(bar({ undone: true })).toMatch(/role="switch"[^>]*aria-checked="true"/);
    expect(bar({ undone: false })).toMatch(/role="switch"[^>]*aria-checked="false"/);
  });
});

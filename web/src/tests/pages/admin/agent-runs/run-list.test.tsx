import { describe, expect, test } from "bun:test";
import { renderToStaticMarkup } from "react-dom/server";

import { RunList, type RunListState } from "@/pages/admin/agent-runs/run-list";

import { digest } from "./fixtures";

const noop = () => undefined;
const list = (
  state: RunListState,
  items = [digest()],
  props: { filtersActive?: boolean; activeRid?: string | null } = {},
) =>
  renderToStaticMarkup(
    <RunList
      state={state}
      items={items}
      activeRid={props.activeRid ?? null}
      filtersActive={props.filtersActive ?? false}
      onOpen={noop}
      onRetry={noop}
      onReset={noop}
    />,
  );

describe("运行列表：各状态", () => {
  test("加载中：表头照常，表体是骨架行，没有真实数据", () => {
    const out = list("loading", []);
    expect(out).toContain("请求首句");
    expect(out).toContain('data-slot="table-skeleton-row"');
    expect(out).not.toContain("没有符合条件的运行");
  });

  test("加载失败：Notice 加重试，不显示表格", () => {
    const out = list("error", []);
    expect(out).toContain('data-slot="notice"');
    expect(out).toContain("运行记录加载失败");
    expect(out).toContain("重试");
    expect(out).not.toContain('data-slot="run-table"');
  });

  test("空：「没有符合条件的运行」；没有筛选时不给重置，有筛选时给「重置筛选」", () => {
    const plain = list("ready", []);
    expect(plain).toContain("没有符合条件的运行");
    expect(plain).not.toContain("重置筛选");
    const filtered = list("ready", [], { filtersActive: true });
    expect(filtered).toContain("没有符合条件的运行");
    expect(filtered).toContain("重置筛选");
  });
});

describe("运行列表：有数据", () => {
  test("九列表头", () => {
    const out = list("ready");
    for (const head of ["时间", "用户", "请求首句", "状态", "步数", "积分", "反馈", "撤销", "版本"])
      expect(out).toContain(`>${head}</th>`);
  });

  test("一行的内容：时间、#用户 ID、请求首句（title 是完整文字）、状态 Tag、步数、积分、版本", () => {
    const out = list("ready", [
      digest({ request_head: "把第三个镜头改成夜景", steps: 7, spent_credits: 12 }),
    ]);
    expect(out).toContain("10-22");
    expect(out).toContain("#42");
    expect(out).toContain('title="把第三个镜头改成夜景"');
    expect(out).toContain('data-tone="success"');
    expect(out).toContain("成功");
    expect(out).toContain(">7<");
    expect(out).toContain(">12<");
    expect(out).toContain("v10");
    expect(out).toContain('title="v10 · a1b2c3"');
  });

  test("状态 Tag 的色调：失败类 danger、预算用尽 warning、已停止 neutral、等待 info", () => {
    expect(list("ready", [digest({ status: "failed" })])).toContain('data-tone="danger"');
    expect(list("ready", [digest({ status: "timeout" })])).toContain("超时");
    expect(list("ready", [digest({ status: "budget_exhausted" })])).toContain(
      'data-tone="warning"',
    );
    expect(list("ready", [digest({ status: "canceled" })])).toContain("已停止");
    expect(list("ready", [digest({ status: "waiting_approval" })])).toContain('data-tone="info"');
  });

  test("请求首句被截成单行：有 truncate 类", () => {
    expect(list("ready")).toMatch(/class="[^"]*truncate[^"]*"[^>]*title="把第三个镜头改成夜景"/);
  });

  test("反馈：👍 / 👎 图标带读屏文字，没评价是「—」", () => {
    expect(list("ready", [digest({ feedback: 1 })])).toContain("有帮助");
    expect(list("ready", [digest({ feedback: -1 })])).toContain("没帮上忙");
    const none = list("ready", [digest({ feedback: 0 })]);
    expect(none).not.toContain("有帮助");
    expect(none).not.toContain("没帮上忙");
  });

  test("撤销：被撤销是「是」，否则是「—」", () => {
    expect(list("ready", [digest({ undone: true })])).toContain(">是<");
    expect(list("ready", [digest({ undone: false })])).not.toContain(">是<");
  });

  test("小徽标：工具报错、插话、审批被拒大于 0 才出现，悬浮说明数量", () => {
    const none = list("ready");
    expect(none).not.toContain("报错");
    expect(none).not.toContain("插话");
    expect(none).not.toContain("拒审");
    const out = list("ready", [digest({ tool_errors: 2, steer_count: 1, approvals_rejected: 3 })]);
    expect(out).toContain('title="工具报错 2 次"');
    expect(out).toContain('title="用户中途插话 1 次"');
    expect(out).toContain('title="用户拒绝了 3 次审批"');
  });

  test("当前在抽屉里看的行高亮，行可聚焦", () => {
    const run = digest();
    const out = list("ready", [run], { activeRid: run.run_id });
    expect(out).toContain('aria-current="true"');
    expect(out).toContain('tabindex="0"');
    expect(out).toContain(`data-run-id="${run.run_id}"`);
    expect(list("ready", [run])).not.toContain("aria-current");
  });

  test("没有请求首句显示「—」", () => {
    expect(list("ready", [digest({ request_head: "" })])).toContain(">—<");
  });
});

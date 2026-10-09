import { describe, expect, test } from "bun:test";
import { renderToStaticMarkup } from "react-dom/server";

import { TaskIdTag } from "@/components/task-id-tag";

const html = (id: string | null) => renderToStaticMarkup(<TaskIdTag id={id} />);

/** 取出包含某段文字的 span 的 class 列表 */
const classOf = (out: string, text: string) => {
  const match = out.match(new RegExp(`<span[^>]*class="([^"]*)"[^>]*>${text}</span>`));
  return match?.[1] ?? "";
};

describe("任务 ID 标签", () => {
  test("没有任务 ID 时什么都不渲染", () => {
    expect(html(null)).toBe("");
    expect(html("")).toBe("");
  });

  test("默认展示任务 ID，「已复制」提示默认隐藏", () => {
    const out = html("fd36");
    expect(classOf(out, "任务 ID：fd36")).not.toContain("invisible");
    expect(classOf(out, "已复制任务 ID")).toContain("invisible");
  });

  test("两段文字叠在同一个网格格子里，复制后切换不会改变按钮高度导致抖动", () => {
    const out = html("fd36");
    // 同一格（col-start-1 row-start-1）：按钮尺寸由两段里较大的那段决定，切换时只改可见性，不改布局
    expect(classOf(out, "任务 ID：fd36")).toContain("col-start-1 row-start-1");
    expect(classOf(out, "已复制任务 ID")).toContain("col-start-1 row-start-1");
  });
});

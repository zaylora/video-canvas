import { describe, expect, test } from "bun:test";
import { renderToStaticMarkup } from "react-dom/server";

import { Markdown } from "@/pages/canvas/agent/markdown";

const html = (text: string) => renderToStaticMarkup(<Markdown text={text} />);

describe("Agent 消息的 Markdown 渲染", () => {
  test("加粗、列表、标题被解析成对应元素", () => {
    const out = html("**已经齐了的**\n\n- 场景资产 6 张\n- 道具资产 6 张\n\n## 小标题");
    expect(out).toContain("<strong");
    expect(out).toContain("<li");
    expect(out).toContain("<h3");
    expect(out).not.toContain("**");
  });

  test("行内加粗在句子中间也能解析", () => {
    expect(html("前面 **1-1 样片的骨架已经搭好**（7 个镜头组）后面")).toContain("<strong");
  });

  test("支持 GFM 表格和删除线", () => {
    const out = html("| a | b |\n|---|---|\n| 1 | 2 |\n\n~~旧~~");
    expect(out).toContain("<table");
    expect(out).toContain("<del");
  });

  test("原始 HTML 不会被渲染", () => {
    const out = html("<script>alert(1)</script><img src=x onerror=alert(1)>");
    expect(out).not.toContain("<script");
    expect(out).not.toContain("<img");
  });

  test("链接在新标签页打开并带 noreferrer", () => {
    const out = html("[文档](https://example.com)");
    expect(out).toContain('target="_blank"');
    expect(out).toContain("noreferrer");
  });

  test("javascript: 链接不会生成 href", () => {
    expect(html("[x](javascript:alert(1))")).not.toContain("javascript:");
  });
});

import { describe, expect, test } from "bun:test";
import { renderToStaticMarkup } from "react-dom/server";

import { Assistant } from "@/pages/canvas/agent/message-list";
import type { TimelineItem } from "@/utils/agent/timeline";

type AssistantItem = Extract<TimelineItem, { type: "assistant" }>;

const item = (over: Partial<AssistantItem> = {}): AssistantItem => ({
  type: "assistant",
  key: "a:r1:0",
  text: "镜头都改好了",
  thinking: "先看看画布",
  streaming: false,
  ...over,
});
const html = (it: AssistantItem, props: { showThinking?: boolean; copied?: boolean } = {}) =>
  renderToStaticMarkup(
    <Assistant
      item={it}
      showThinking={props.showThinking ?? true}
      copied={props.copied ?? false}
    />,
  );

describe("助手消息：结束时不插入新元素、不位移", () => {
  test("流式中已有思考：正文上方就是「思考中」，结束后同一位置变成「已思考」", () => {
    const streaming = html(item({ streaming: true }));
    const done = html(item());
    expect(streaming).toContain("思考中");
    expect(streaming).not.toContain("已思考");
    expect(done).toContain("已思考");
    expect(done).not.toContain("思考中");
    // 两种状态里，思考标签都在正文之前
    expect(streaming.indexOf("思考中")).toBeLessThan(streaming.indexOf("镜头都改好了"));
    expect(done.indexOf("已思考")).toBeLessThan(done.indexOf("镜头都改好了"));
  });

  test("流式中的「思考中」不是按钮（不可展开）；结束后的「已思考」是可展开的按钮", () => {
    expect(html(item({ streaming: true }))).not.toContain("<button");
    expect(html(item())).toContain('aria-expanded="false"');
  });

  test("没有思考内容、或者不显示思考：什么标签都没有", () => {
    expect(html(item({ thinking: "" }))).not.toMatch(/思考/);
    expect(html(item(), { showThinking: false })).not.toMatch(/思考/);
  });

  test("流式输出时没有闪烁的光标", () => {
    expect(html(item({ streaming: true }))).not.toContain("animate-pulse");
  });

  test("复制按钮浮在消息上，不占一行高度；流式中不出现", () => {
    const done = html(item());
    const copy = done.match(/<button[^>]*aria-label="复制"[^>]*>/)?.[0] ?? "";
    expect(copy).toContain("absolute");
    expect(done).not.toContain("h-6");
    expect(html(item({ streaming: true }))).not.toContain('aria-label="复制"');
  });

  test("这一轮的底栏已经有复制按钮时，消息上不再重复", () => {
    expect(html(item(), { copied: true })).not.toContain('aria-label="复制"');
  });
});

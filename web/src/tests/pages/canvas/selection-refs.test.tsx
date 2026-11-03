import { describe, expect, test } from "bun:test";
import { renderToStaticMarkup } from "react-dom/server";

import { SelectionRefs } from "@/pages/canvas/agent/selection-refs";

/**
 * 用户消息上方的节点引用：后台助手、短剧助手等没有画布的入口也渲染消息列表，
 * 没有 ReactFlowProvider。没有引用时不能去取画布的 hook，否则整页报错。
 */
describe("SelectionRefs", () => {
  test("没有引用时不渲染，也不依赖 ReactFlowProvider", () => {
    expect(() => renderToStaticMarkup(<SelectionRefs ids={[]} nodes={[]} />)).not.toThrow();
    expect(renderToStaticMarkup(<SelectionRefs ids={[]} nodes={[]} />)).toBe("");
  });
});

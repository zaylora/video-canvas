import { describe, expect, test } from "bun:test";
import { renderToStaticMarkup } from "react-dom/server";

import { ContextLimitsHintView } from "@/pages/admin/ai/models/context-limits-assist";

const render = (props: Parameters<typeof ContextLimitsHintView>[0]) =>
  renderToStaticMarkup(<ContextLimitsHintView {...props} />);

describe("上下文规格的识别提示", () => {
  test("原型里的识别建议包含输入能力，OpenAI 下提示看视频不会被应用", () => {
    const html = render({
      state: {
        kind: "ready",
        limits: {
          found: true,
          context_window: 128000,
          max_output: 16384,
          vision: true,
          video: true,
        },
      },
      current: { window: 128000, output: 16384 },
      media: { vision: false, video: false },
      gemini: false,
      onApply: () => {},
    });
    expect(html).toContain("能看图");
    expect(html).toContain("能看视频");
    expect(html).toContain("应用识别值");
    expect(html).toContain("不会被应用");
  });
  test("还没选上游模型：提示先选好渠道和上游模型", () => {
    const html = render({ state: { kind: "idle" }, current: undefined, onApply: () => {} });
    expect(html).toContain("上游模型");
    expect(html).not.toContain("应用");
  });

  test("识别中", () => {
    expect(render({ state: { kind: "loading" }, current: undefined, onApply: () => {} })).toContain(
      "正在识别",
    );
  });

  test("没认出来：请手动填写", () => {
    const html = render({
      state: { kind: "ready", limits: { found: false, context_window: 0, max_output: 0 } },
      current: undefined,
      onApply: () => {},
    });
    expect(html).toContain("手动填写");
  });

  test("识别失败也只提示手动填写，不是错误", () => {
    expect(render({ state: { kind: "error" }, current: undefined, onApply: () => {} })).toContain(
      "手动填写",
    );
  });

  test("认出来了、当前值一样：说明来源和数值，没有应用按钮", () => {
    const html = render({
      state: {
        kind: "ready",
        limits: { found: true, context_window: 128000, max_output: 16384, source: "builtin" },
      },
      current: { window: 128000, output: 16384 },
      onApply: () => {},
    });
    expect(html).toContain("内置模型库");
    expect(html).toContain("128,000");
    expect(html).toContain("16,384");
    expect(html).not.toContain("应用识别值");
  });

  test("认出来了、当前值不同：给出「应用识别值」按钮，不会自动覆盖", () => {
    const html = render({
      state: {
        kind: "ready",
        limits: { found: true, context_window: 300000, max_output: 80000, source: "upstream" },
      },
      current: { window: 128000, output: 16384 },
      onApply: () => {},
    });
    expect(html).toContain("渠道上游返回");
    expect(html).toContain("应用识别值");
  });
});

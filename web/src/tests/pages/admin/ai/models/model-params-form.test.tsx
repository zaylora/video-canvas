import { describe, expect, test } from "bun:test";
import { renderToStaticMarkup } from "react-dom/server";

import { ModelParamsForm } from "@/pages/admin/ai/models/model-params-form";
import { defaultCapabilities } from "@/utils/admin/model-template";

const render = (kind: string) =>
  renderToStaticMarkup(
    <ModelParamsForm
      body={{ kind, capabilities: defaultCapabilities(kind) }}
      issues={[]}
      warnings={[]}
      onChange={() => {}}
    />,
  );

describe("ModelParamsForm", () => {
  test.each(["video", "image", "audio", "text", "agent"])(
    "%s 模型的能力与参数页完全由面板控制，没有固定参数和 JSON 编辑入口",
    (kind) => {
      const html = render(kind);
      expect(html).not.toContain("固定参数");
      expect(html).not.toContain("JSON");
      expect(html).not.toContain("model-params");
      expect(html).not.toContain('model-capabilities"');
    },
  );

  test("视频模型仍然渲染生成参数面板", () => {
    expect(render("video")).toContain("生成参数");
  });
});

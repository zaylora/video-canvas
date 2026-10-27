import { describe, expect, test } from "bun:test";
import { renderToStaticMarkup } from "react-dom/server";

import type { ConfigIssue } from "@/api/admin/ai/type";
import { ModelBasicForm } from "@/pages/admin/ai/models/model-basic-form";

const render = (kind: string, format?: string, issues: ConfigIssue[] = []) =>
  renderToStaticMarkup(
    <ModelBasicForm
      body={{
        kind,
        label: "测试模型",
        channels: [{ channel: "c", upstream_model: "m", api_format: format }],
      }}
      isNew
      catalog={{ channels: [], plugins: [], pluginsStatus: "ready" }}
      issues={issues}
      warnings={[]}
      onChange={() => {}}
    />,
  );

describe("ModelBasicForm 接口格式", () => {
  test("旧 Agent 配置回显 OpenAI 兼容与请求说明", () => {
    const html = render("agent");
    expect(html).toContain('for="model-api-format"');
    expect(html).toContain("OpenAI 兼容");
    expect(html).toContain("{base}/v1/chat/completions");
    expect(html).toContain("Authorization: Bearer");
  });

  test("Gemini 配置回显原生格式与渠道根路径说明", () => {
    const html = render("agent", "gemini");
    expect(html).toContain("Gemini 原生");
    expect(html).toContain("{base}/v1beta/models/{上游模型}:streamGenerateContent?alt=sse");
    expect(html).toContain("x-goog-api-key 与 Authorization: Bearer");
    expect(html).toContain("渠道地址要指向 Gemini 原生接口的根路径（可能带专用前缀）");
  });

  test.each(["text", "image", "video", "audio", ""])("%s 能力不显示接口格式", (kind) => {
    expect(render(kind)).not.toContain("model-api-format");
  });

  test("后端接口格式错误显示在对应字段下", () => {
    const html = render("agent", "gemini", [
      { path: "channels[0].api_format", message: "接口格式只能是 openai 或 gemini" },
    ]);
    expect(html).toContain("接口格式只能是 openai 或 gemini");
    expect(html).toContain('aria-invalid="true"');
  });
});

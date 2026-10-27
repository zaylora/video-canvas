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
  test("按原型提供看视频开关；OpenAI 禁用，Gemini 可用，其他种类不显示", () => {
    const view = (format: string) =>
      renderToStaticMarkup(
        <ModelParamsForm
          body={{
            kind: "agent",
            channels: [{ api_format: format }],
            capabilities: defaultCapabilities("agent"),
          }}
          issues={[]}
          warnings={[]}
          onChange={() => {}}
        />,
      );
    expect(view("openai")).toContain("能看视频");
    expect(view("openai")).toContain("只有 Gemini");
    expect(view("openai").match(/<button[^>]*id="model-video"[^>]*>/)?.[0]).toContain(
      'disabled=""',
    );
    expect(view("gemini").match(/<button[^>]*id="model-video"[^>]*>/)?.[0]).not.toContain(
      'disabled=""',
    );
    for (const kind of ["image", "video", "text", "audio"])
      expect(render(kind)).not.toContain("能看视频");
  });
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

  test("视频模型仍然渲染生成参数面板，但不能再手动添加新参数", () => {
    const html = render("video");
    expect(html).toContain("生成参数");
    expect(html).not.toContain("添加参数");
    expect(html).not.toContain("新参数名");
  });

  test("音频模型能在「参考素材」里开图片参考，但没有视频和音频参考", () => {
    const html = render("audio");
    expect(html).toContain("参考素材");
    expect(html).toContain("接收参考图片");
    expect(html).not.toContain("接收参考音频");
    expect(html).not.toContain("接收参考视频");
  });

  test("视频模型的参考素材仍有图片、音频、视频三项；文本和 Agent 模型没有参考素材", () => {
    const video = render("video");
    for (const label of ["接收参考图片", "接收参考音频", "接收参考视频"])
      expect(video).toContain(label);
    for (const kind of ["text", "agent"]) expect(render(kind)).not.toContain("参考素材");
  });

  test("Agent 模型有「上游响应超时」，视频等其他模型没有", () => {
    expect(render("agent")).toContain("上游响应超时");
    for (const kind of ["video", "image", "audio", "text"])
      expect(render(kind)).not.toContain("上游响应超时");
  });

  test("Agent 模型没有「任务超时」：那是异步生成任务的截止时间，和对话调用无关", () => {
    expect(render("agent")).not.toContain("任务超时");
    expect(render("video")).toContain("任务超时");
  });

  test("已配置的上游响应超时回显在自定义输入框里", () => {
    const html = renderToStaticMarkup(
      <ModelParamsForm
        body={{ kind: "agent", idle_timeout: "150s", capabilities: defaultCapabilities("agent") }}
        issues={[]}
        warnings={[]}
        onChange={() => {}}
      />,
    );
    expect(html).toContain('value="150s"');
  });

  test("后端返回的 idle_timeout 校验问题显示在这个字段下", () => {
    const html = renderToStaticMarkup(
      <ModelParamsForm
        body={{ kind: "agent", idle_timeout: "5s", capabilities: defaultCapabilities("agent") }}
        issues={[{ path: "idle_timeout", message: "必须在 10s – 10m 之间" }]}
        warnings={[]}
        onChange={() => {}}
      />,
    );
    expect(html).toContain("必须在 10s – 10m 之间");
  });
});

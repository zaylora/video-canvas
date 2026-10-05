import { describe, expect, test } from "bun:test";

import { PLUGIN_AUTHORING_PROMPT } from "@/pages/admin/ai/plugins/plugin-prompt";

describe("协议插件编写提示词", () => {
  test("要求工作流导入草稿自动携带识别出的节点映射", () => {
    expect(PLUGIN_AUTHORING_PROMPT).toContain("返回草稿的 params.nodes");
    expect(PLUGIN_AUTHORING_PROMPT).toContain("meta.import");
  });

  test("要求提交前拦截空节点映射", () => {
    expect(PLUGIN_AUTHORING_PROMPT).toContain("不得提交空节点列表");
    expect(PLUGIN_AUTHORING_PROMPT).toContain("提示词输入必须绑定到工作流节点");
  });

  test("资料不完整时要求逐项追问，补齐后才生成插件", () => {
    expect(PLUGIN_AUTHORING_PROMPT).toContain("信息不完整时先向用户提问");
    expect(PLUGIN_AUTHORING_PROMPT).toContain("收到回答后重新检查");
    expect(PLUGIN_AUTHORING_PROMPT).toContain("不得输出插件代码");
  });

  test("默认要求连通性检查和完整的模型导入闭环", () => {
    expect(PLUGIN_AUTHORING_PROMPT).toContain(
      "buildCheckRequest(ctx)               模型/工作流插件必须实现",
    );
    expect(PLUGIN_AUTHORING_PROMPT).toContain(
      "meta.import、buildImportRequest / parseImportResponse 成对钩子、buildCheckRequest",
    );
    expect(PLUGIN_AUTHORING_PROMPT).toContain("不要提交生成任务");
  });

  test("文件输入优先使用直接引用且不假定 prepared 一定存在", () => {
    expect(PLUGIN_AUTHORING_PROMPT).toContain("最终提交请求的嵌套 JSON 字段里直接放");
    expect(PLUGIN_AUTHORING_PROMPT).toContain("不得因为实现了准备钩子就无条件解引用 ctx.prepared");
    expect(PLUGIN_AUTHORING_PROMPT).not.toContain("RunningHub");
    expect(PLUGIN_AUTHORING_PROMPT).not.toContain("ComfyUI");
  });
});

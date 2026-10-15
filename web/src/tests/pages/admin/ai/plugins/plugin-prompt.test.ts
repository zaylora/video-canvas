import { describe, expect, test } from "bun:test";

import { PLUGIN_AUTHORING_PROMPT } from "@/pages/admin/ai/plugins/plugin-prompt";

describe("协议插件编写提示词", () => {
  test("模型没有固定参数：ctx.model 只有 key / kind / upstreamModel，输入全部来自面板配置的 ctx.input", () => {
    expect(PLUGIN_AUTHORING_PROMPT).toContain("model:   { key, kind, upstreamModel }");
    expect(PLUGIN_AUTHORING_PROMPT).not.toContain("ctx.model.params");
    expect(PLUGIN_AUTHORING_PROMPT).not.toContain("固定参数");
    expect(PLUGIN_AUTHORING_PROMPT).not.toContain("params.nodes");
    expect(PLUGIN_AUTHORING_PROMPT).not.toContain("params.fixedNodes");
    expect(PLUGIN_AUTHORING_PROMPT).not.toContain("params 示例");
  });

  test("工作流的节点映射、档位对照表等固定内容写在插件代码里，不再靠运营填写", () => {
    expect(PLUGIN_AUTHORING_PROMPT).toContain("写在插件代码里");
    expect(PLUGIN_AUTHORING_PROMPT).toContain("不要要求运营另外填写");
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

  test("连通性检查与模型导入已弃用：不再要求实现，并明确告诉 AI 不要写", () => {
    expect(PLUGIN_AUTHORING_PROMPT).toContain(
      "buildCheckRequest / buildImportRequest / parseImportResponse 与 meta.import 已弃用",
    );
    expect(PLUGIN_AUTHORING_PROMPT).toContain("宿主不再调用，不要写");
    // 旧的“必须实现检查与导入”的要求都不能残留
    expect(PLUGIN_AUTHORING_PROMPT).not.toContain("模型/工作流插件必须实现");
    expect(PLUGIN_AUTHORING_PROMPT).not.toContain("成对实现并配合 meta.import");
    expect(PLUGIN_AUTHORING_PROMPT).not.toContain("meta.import 已声明");
    expect(PLUGIN_AUTHORING_PROMPT).not.toContain("导入阶段绝不能调用生成接口");
    expect(PLUGIN_AUTHORING_PROMPT).not.toContain("import: { args: {} }");
    expect(PLUGIN_AUTHORING_PROMPT).not.toContain("buildCheckRequest: (ctx)");
    expect(PLUGIN_AUTHORING_PROMPT).not.toContain("parseImportResponse: (ctx");
  });

  test("meta 建议声明 vendor 与 endpoints 的 method / path，只用于展示", () => {
    expect(PLUGIN_AUTHORING_PROMPT).toContain('vendor: "my-platform"');
    expect(PLUGIN_AUTHORING_PROMPT).toContain('method: "POST", path: "/v1/images/generations"');
    expect(PLUGIN_AUTHORING_PROMPT).toContain("只用于管理端展示");
  });

  test("channelSettings 改称协议设置项：在模型里填，插件仍读 ctx.channel.settings", () => {
    expect(PLUGIN_AUTHORING_PROMPT).toContain("协议设置项");
    expect(PLUGIN_AUTHORING_PROMPT).toContain("在模型的“协议设置”里填");
    expect(PLUGIN_AUTHORING_PROMPT).toContain("ctx.channel.settings");
    expect(PLUGIN_AUTHORING_PROMPT).not.toContain("渠道上的非敏感设置");
    expect(PLUGIN_AUTHORING_PROMPT).not.toContain("渠道设置项");
  });

  test("auth 为 custom 时宿主直接提供 ctx.credentials（含 secretKey），不再有“允许读取 Key”开关", () => {
    expect(PLUGIN_AUTHORING_PROMPT).toContain("ctx.credentials");
    expect(PLUGIN_AUTHORING_PROMPT).toContain("secretKey");
    expect(PLUGIN_AUTHORING_PROMPT).not.toContain("允许插件读取 Key");
    expect(PLUGIN_AUTHORING_PROMPT).not.toContain("allow_credentials");
  });

  test("渠道自定义请求头由宿主自动加，插件不用处理", () => {
    expect(PLUGIN_AUTHORING_PROMPT).toContain("渠道的自定义请求头由宿主自动加");
  });

  test("文件输入优先使用直接引用且不假定 prepared 一定存在", () => {
    expect(PLUGIN_AUTHORING_PROMPT).toContain("最终提交请求的嵌套 JSON 字段里直接放");
    expect(PLUGIN_AUTHORING_PROMPT).toContain("不得因为实现了准备钩子就无条件解引用 ctx.prepared");
    expect(PLUGIN_AUTHORING_PROMPT).not.toContain("RunningHub");
    expect(PLUGIN_AUTHORING_PROMPT).not.toContain("ComfyUI");
  });

  test("不再要求声明结果下载域名：提示词里没有 allowedHosts", () => {
    expect(PLUGIN_AUTHORING_PROMPT).not.toContain("allowedHosts");
  });

  test("请求描述的绝对地址可以是任意公网地址，只有和渠道 baseUrl 同源才注入 Key", () => {
    expect(PLUGIN_AUTHORING_PROMPT).toContain("任意公网 http/https 地址");
    expect(PLUGIN_AUTHORING_PROMPT).toContain(
      "只有和渠道 baseUrl 的协议、域名、端口都一致时，宿主才会注入 Key",
    );
    expect(PLUGIN_AUTHORING_PROMPT).toContain("其他地址不会带 Key");
  });

  test("统一结果的 url 产物不需要声明域名", () => {
    expect(PLUGIN_AUTHORING_PROMPT).toContain("文件可以放在任意公网域名，不需要声明域名");
  });

  test("放开现代 JS 语法，只保留真正不能用的东西", () => {
    expect(PLUGIN_AUTHORING_PROMPT).toContain("ES2020 的常见语法都能用");
    expect(PLUGIN_AUTHORING_PROMPT).toContain("钩子不能是 async 函数");
    // 旧的“只写 ES5”限制不能残留
    expect(PLUGIN_AUTHORING_PROMPT).not.toContain("只保证 ES5.1");
    expect(PLUGIN_AUTHORING_PROMPT).not.toContain("不要用可选链");
    expect(PLUGIN_AUTHORING_PROMPT).not.toContain("请用 var / function");
  });

  test("版本号可以不改：不再要求改代码就升 meta.version", () => {
    expect(PLUGIN_AUTHORING_PROMPT).toContain("版本号可以不改，上传时自动处理");
    expect(PLUGIN_AUTHORING_PROMPT).not.toContain("改了代码就升 meta.version");
    expect(PLUGIN_AUTHORING_PROMPT).not.toContain("提醒使用者把渠道切到新版本");
  });

  test("统一结果先教简写，再说明写错仍会报错", () => {
    expect(PLUGIN_AUTHORING_PROMPT).toContain('outputs: ["https://结果文件地址"]');
    expect(PLUGIN_AUTHORING_PROMPT).toContain('error: "给用户看的失败原因"');
    expect(PLUGIN_AUTHORING_PROMPT).toContain("写了就要写对");
    expect(PLUGIN_AUTHORING_PROMPT).toContain("不要写 SUCCESS、completed");
  });

  test("限流 / 繁忙必须归为 retryable，不能归为 terminal", () => {
    expect(PLUGIN_AUTHORING_PROMPT).toContain("一定要用 retryable");
  });
});

describe("协议插件编写提示词：核心 + 进阶附录", () => {
  const ADDENDUM = "# 附录：进阶写法";
  const DOC_SLOT = "# 目标平台 API 文档";
  // 简化前（c6bde58）的提示词长度，单位是字符（String.length）
  const BASELINE_LENGTH = 16780;

  const addendumAt = PLUGIN_AUTHORING_PROMPT.indexOf(ADDENDUM);
  const slotAt = PLUGIN_AUTHORING_PROMPT.indexOf(DOC_SLOT);
  const core = PLUGIN_AUTHORING_PROMPT.slice(0, addendumAt);
  const addendum = PLUGIN_AUTHORING_PROMPT.slice(addendumAt, slotAt);

  test("顺序是：核心 → 附录 → 文末粘贴 API 文档的位置", () => {
    expect(addendumAt).toBeGreaterThan(0);
    expect(slotAt).toBeGreaterThan(addendumAt);
    expect(PLUGIN_AUTHORING_PROMPT.trimEnd().endsWith("【在此粘贴文档】")).toBe(true);
  });

  test("核心不超过简化前提示词的 55%（设计目标是 50%，留一点措辞调整的余量）", () => {
    expect(core.length).toBeLessThanOrEqual(BASELINE_LENGTH * 0.55);
  });

  test("“先提问再出码”和“检查与导入已弃用”的说明留在核心里，不会被挪进附录", () => {
    for (const phrase of [
      "先检查信息是否完整",
      "信息不完整时先向用户提问",
      "检查通过前不得输出插件代码",
      "buildCheckRequest",
      "meta.import 已弃用",
    ]) {
      expect(core).toContain(phrase);
    }
  });

  test("常用写法在核心，少见写法在附录", () => {
    for (const phrase of ["__fileRef", "providerTaskId", "buildQueryRequest", "immediate"]) {
      expect(core).toContain(phrase);
    }
    for (const phrase of [
      "buildPrepareRequests",
      "ctx.prepared",
      "nodeInfoList",
      "responseType 为 binary",
      "classifyError",
      "utils.hmacSHA256",
    ]) {
      expect(addendum).toContain(phrase);
      expect(core).not.toContain(phrase);
    }
    // 绝对地址 url：核心只留一句“见附录”的指引，详细说明在附录
    expect(core).toContain("绝对地址 url 见附录");
    expect(addendum).toContain("## A3 绝对地址 url");
  });

  test("附录里说明绝对地址只对同源请求注入 Key", () => {
    expect(addendum).toContain("其他地址不会带 Key");
  });

  test("文本里没有反引号和 ${，不会破坏模板字符串", () => {
    expect(PLUGIN_AUTHORING_PROMPT).not.toContain("`");
    expect(PLUGIN_AUTHORING_PROMPT).not.toContain("${");
  });
});

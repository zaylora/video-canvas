import { describe, expect, test } from "bun:test";

import type { TraceStep } from "@/api/admin/ai/type";
import {
  buildChannelCheckRepairReport,
  buildRunRepairReport,
  buildUploadRepairReport,
  pickFailedSteps,
  REPAIR_BLOCK_MAX_BYTES,
  REPAIR_INTRO,
  REPAIR_TOTAL_MAX_BYTES,
  TRUNCATED_MARK,
  truncateUtf8,
} from "@/utils/admin/repair-report";
import { toTraceView } from "@/utils/admin/trace";

const bytes = (text: string) => new TextEncoder().encode(text).length;

/** 不含任何孤立代理项的合法字符串：编码再解码得到原文，且没有替换字符 */
function expectWellFormed(text: string) {
  expect(text).not.toContain("\uFFFD");
  expect(new TextDecoder("utf-8", { fatal: true }).decode(new TextEncoder().encode(text))).toBe(
    text,
  );
}

const hookStep = (hookName: string, over: Partial<TraceStep> = {}): TraceStep => ({
  name: "query",
  kind: "hook",
  hook: { name: hookName, input: { state: "SUCCESS" }, output: { status: "SUCCESS" }, logs: [] },
  duration_ms: 5,
  ...over,
});

const httpStep = (status: number, over: Partial<TraceStep> = {}): TraceStep => ({
  name: "submit",
  kind: "http",
  request: {
    method: "POST",
    url: "https://api.test/v1/images",
    headers: { Authorization: "***", "X-Trace": "abc" },
    body: '{"prompt":"a cat"}',
  },
  response: { status, body: '{"error":"missing api key"}', truncated: false },
  duration_ms: 20,
  ...over,
});

/** 原始追踪 → 展示模型，和界面用同一个转换，顺便守住标签字面量不漂移 */
const viewOf = (steps: TraceStep[]) => toTraceView(steps).steps;

describe("truncateUtf8", () => {
  test("没超限原样返回，正好等于上限也不截断", () => {
    expect(truncateUtf8("hello", 100)).toBe("hello");
    expect(truncateUtf8("a".repeat(50), 50)).toBe("a".repeat(50));
    expect(truncateUtf8("字".repeat(10), 30)).toBe("字".repeat(10));
  });

  test("按 UTF-8 字节数而不是字符数截断，结果（含标记）不超过上限", () => {
    const out = truncateUtf8("a".repeat(500), 100);
    expect(bytes(out)).toBeLessThanOrEqual(100);
    expect(out.endsWith(TRUNCATED_MARK)).toBe(true);
    // 英文 1 字节 1 个字符：上限 100 字节，扣掉标记后尽量多留
    expect(out).toBe("a".repeat(100 - bytes(TRUNCATED_MARK)) + TRUNCATED_MARK);

    // 中文 3 字节 1 个字符：2048 字节只能放下约 2/3 的字符数
    const zh = truncateUtf8("字".repeat(1000), REPAIR_BLOCK_MAX_BYTES);
    expect(bytes(zh)).toBeLessThanOrEqual(REPAIR_BLOCK_MAX_BYTES);
    const kept = zh.slice(0, -TRUNCATED_MARK.length);
    expect(kept).toBe("字".repeat(kept.length));
    expect(kept.length).toBeGreaterThan(600);
    expect(kept.length).toBeLessThan(700);
  });

  test("上限落在多字节字符中间时整字符舍弃，不切成两半", () => {
    // 10 个 ASCII + 10 个汉字共 40 字节；上限 30，扣掉标记后预算 12：
    // 10 个 ASCII 占 10 字节，再放一个汉字要 13 字节放不下，所以只保留 ASCII
    const text = "x".repeat(10) + "字".repeat(10);
    const out = truncateUtf8(text, bytes(TRUNCATED_MARK) + 12);
    expect(out).toBe("x".repeat(10) + TRUNCATED_MARK);
    expectWellFormed(out);
  });

  test("四字节字符（emoji）不被切开，也不留下孤立代理项", () => {
    for (const limit of [30, 31, 32, 33, 34, 35, 100, 101, 102, 103]) {
      const out = truncateUtf8("😀".repeat(100), limit);
      expect(bytes(out)).toBeLessThanOrEqual(limit);
      expectWellFormed(out);
      const kept = out.slice(0, -TRUNCATED_MARK.length);
      expect([...kept].every((char) => char === "😀")).toBe(true);
    }
  });

  test("混合内容在各种上限下都合法且不超限", () => {
    const text = "插件 plugin 🙂 日志：" + "错a😀".repeat(300);
    for (let limit = 20; limit < 400; limit += 7) {
      const out = truncateUtf8(text, limit);
      expect(bytes(out)).toBeLessThanOrEqual(limit);
      expectWellFormed(out);
      expect(out.endsWith(TRUNCATED_MARK)).toBe(true);
    }
  });

  test("标记只追加在被截断的地方，且只出现一次", () => {
    const out = truncateUtf8("b".repeat(3000), 200);
    expect(out.split(TRUNCATED_MARK).length - 1).toBe(1);
    expect(out.endsWith(TRUNCATED_MARK)).toBe(true);
    expect(truncateUtf8("short", 200)).not.toContain(TRUNCATED_MARK);
  });

  test("上限常量是 2KB 和 16KB", () => {
    expect(REPAIR_BLOCK_MAX_BYTES).toBe(2048);
    expect(REPAIR_TOTAL_MAX_BYTES).toBe(16384);
    expect(TRUNCATED_MARK).toBe("…（已截断）");
  });
});

describe("pickFailedSteps：失败步骤的选择规则", () => {
  test("有标记为失败的步骤：只取它们，保持原顺序和原序号", () => {
    const steps = viewOf([
      hookStep("buildSubmitRequest"),
      httpStep(500),
      hookStep("parseSubmitResponse", { error: "boom" }),
      hookStep("buildQueryRequest"),
    ]);
    const picked = pickFailedSteps(steps);
    expect(picked.map((step) => step.index)).toEqual([2, 3]);
    expect(picked.every((step) => step.failed)).toBe(true);
  });

  test("一个都没有失败（钩子成功但宿主不认返回值）：取追踪里的最后一个步骤", () => {
    const steps = viewOf([
      hookStep("buildSubmitRequest"),
      httpStep(200),
      hookStep("parseQueryResponse"),
    ]);
    expect(steps.some((step) => step.failed)).toBe(false);
    const picked = pickFailedSteps(steps);
    expect(picked).toHaveLength(1);
    expect(picked[0].index).toBe(3);
    expect(picked[0].title).toBe("parseQueryResponse");
  });

  test("追踪为空：没有可选的步骤", () => {
    expect(pickFailedSteps([])).toEqual([]);
  });
});

describe("buildUploadRepairReport：上传预检", () => {
  const issues = [
    {
      path: "exports.buildQueryRequest",
      message: "endpoints.image.mode 为 async，必须实现 buildQueryRequest 与 parseQueryResponse",
    },
    { path: "meta.auth.name", message: "auth.type 为 header 时必须写 auth.name" },
  ];

  test("拿不到插件 key 和版本号：不写“插件：”，也不编造", () => {
    expect(buildUploadRepairReport({ issues })).toBe(
      [
        REPAIR_INTRO,
        "",
        "阶段：上传预检",
        "",
        "问题：",
        "- exports.buildQueryRequest：endpoints.image.mode 为 async，必须实现 buildQueryRequest 与 parseQueryResponse",
        "- meta.auth.name：auth.type 为 header 时必须写 auth.name",
      ].join("\n"),
    );
  });

  test("开头说明就是约定的那句话", () => {
    expect(REPAIR_INTRO).toBe(
      "我的 video-canvas 协议插件出错了，请按插件契约修复，输出完整的 .js 文件（版本号不用改，上传时会自动递增）。",
    );
  });

  test("有插件 key 和代码里声明的版本号：写进“插件：”这一行", () => {
    const text = buildUploadRepairReport({
      issues,
      pluginKey: "yswg-image",
      declaredVersion: "1.3.1",
    });
    expect(text).toContain("插件：yswg-image（代码里的版本 1.3.1）\n阶段：上传预检");
  });

  test("只有 key：不带括号；只有版本号没有 key：单独一行", () => {
    expect(buildUploadRepairReport({ issues, pluginKey: "yswg-image" })).toContain(
      "插件：yswg-image\n阶段：上传预检",
    );
    const onlyVersion = buildUploadRepairReport({ issues, declaredVersion: "2.0.0" });
    expect(onlyVersion).toContain("代码里的版本：2.0.0\n阶段：上传预检");
    expect(onlyVersion).not.toContain("插件：");
  });

  test("空路径表示整个文件；空白的 key 和版本当作没有", () => {
    const text = buildUploadRepairReport({
      issues: [{ path: "", message: "语法错误：第 3 行" }],
      pluginKey: "  ",
      declaredVersion: "",
    });
    expect(text).toContain("- （整个文件）：语法错误：第 3 行");
    expect(text).not.toContain("插件：");
    expect(text).not.toContain("代码里的版本");
  });

  test("问题说明有多行时，续行缩进 2 格归在同一条下面", () => {
    const text = buildUploadRepairReport({
      issues: [{ path: "", message: "语法错误\nline 3: unexpected token" }],
    });
    expect(text).toContain("- （整个文件）：语法错误\n  line 3: unexpected token");
  });

  test("没有问题就不写“问题：”这一节", () => {
    expect(buildUploadRepairReport({ issues: [] })).toBe(`${REPAIR_INTRO}\n\n阶段：上传预检`);
  });

  test("单条问题说明超过 2KB 会截断；问题特别多时整体不超过 16KB", () => {
    const long = buildUploadRepairReport({
      issues: [{ path: "meta", message: "长".repeat(3000) }],
    });
    const line = long.split("\n").find((item) => item.startsWith("- meta："))!;
    expect(bytes(line.slice("- meta：".length))).toBeLessThanOrEqual(REPAIR_BLOCK_MAX_BYTES);
    expect(line.endsWith(TRUNCATED_MARK)).toBe(true);

    const many = buildUploadRepairReport({
      issues: Array.from({ length: 2000 }, (_, index) => ({
        path: `meta.field${index}`,
        message: "字段不合法，需要修改",
      })),
    });
    expect(bytes(many)).toBeLessThanOrEqual(REPAIR_TOTAL_MAX_BYTES);
    expect(many.endsWith(TRUNCATED_MARK)).toBe(true);
    expect(many.startsWith(REPAIR_INTRO)).toBe(true);
    expectWellFormed(many);
  });
});

describe("buildRunRepairReport：试跑失败", () => {
  const base = {
    plugin: { key: "yswg-image", version: "1.3.1" },
    modelKey: "yswg-gpt-image",
    kind: "image",
    errorCode: "plugin_error",
    errorMessage: "统一结果的 status 不合法",
  };

  test("钩子执行成功但宿主不认返回值（没有失败步骤）：完整格式，取最后一个步骤", () => {
    const steps = viewOf([
      hookStep("buildSubmitRequest"),
      httpStep(200),
      hookStep("parseQueryResponse"),
    ]);
    expect(buildRunRepairReport({ ...base, steps })).toBe(
      [
        REPAIR_INTRO,
        "",
        "插件：yswg-image v1.3.1",
        "阶段：试跑（图片模型 yswg-gpt-image）",
        "错误：plugin_error · 统一结果的 status 不合法",
        "",
        "失败步骤：",
        "[3] parseQueryResponse",
        "  输入：",
        "    {",
        '      "state": "SUCCESS"',
        "    }",
        "  输出：",
        "    {",
        '      "status": "SUCCESS"',
        "    }",
      ].join("\n"),
    );
  });

  test("单行的块直接写在标签后面", () => {
    const steps = viewOf([
      hookStep("parseQueryResponse", {
        hook: { name: "parseQueryResponse", input: "raw text", output: "ok" },
      }),
    ]);
    const text = buildRunRepairReport({ ...base, steps });
    expect(text).toContain("[1] parseQueryResponse\n  输入：raw text\n  输出：ok");
  });

  test("钩子步骤写出错信息、输入、输出和插件日志（utils.log）", () => {
    const steps = viewOf([
      hookStep("buildQueryRequest", {
        error: "ReferenceError: foo is not defined",
        hook: {
          name: "buildQueryRequest",
          input: "in",
          output: undefined,
          logs: ["开始查询", "task=8812"],
        },
      }),
    ]);
    const text = buildRunRepairReport({ ...base, steps });
    expect(text).toContain(
      [
        "[1] buildQueryRequest",
        "  错误：ReferenceError: foo is not defined",
        "  输入：in",
        "  插件日志：",
        "    开始查询",
        "    task=8812",
      ].join("\n"),
    );
    // 没有输出就不写“输出”
    expect(text).not.toContain("输出：");
  });

  test("http 步骤写状态码、请求体、响应体，不写请求头", () => {
    const steps = viewOf([hookStep("buildSubmitRequest"), httpStep(401)]);
    const text = buildRunRepairReport({ ...base, steps });
    expect(text).toContain(
      [
        "[2] POST /v1/images",
        "  状态码：401",
        "  请求体：",
        "    {",
        '      "prompt": "a cat"',
        "    }",
        "  响应体：",
        "    {",
        '      "error": "missing api key"',
        "    }",
      ].join("\n"),
    );
    expect(text).not.toContain("请求头");
    expect(text).not.toContain("Authorization");
    expect(text).not.toContain("X-Trace");
  });

  test("请求失败没有响应：写出错信息，不写状态码", () => {
    const steps = viewOf([httpStep(0, { error: "dial tcp: i/o timeout", response: undefined })]);
    const text = buildRunRepairReport({ ...base, steps });
    expect(text).toContain("[1] POST /v1/images\n  错误：dial tcp: i/o timeout");
    expect(text).not.toContain("状态码");
  });

  test("有失败步骤时只写失败的，多个失败步骤之间空一行", () => {
    const steps = viewOf([
      hookStep("buildSubmitRequest"),
      httpStep(502),
      hookStep("buildQueryRequest", { error: "boom" }),
      hookStep("parseQueryResponse"),
    ]);
    const text = buildRunRepairReport({ ...base, steps });
    expect(text).toContain("[2] POST /v1/images");
    expect(text).toContain("[3] buildQueryRequest");
    expect(text).not.toContain("[1] ");
    expect(text).not.toContain("[4] ");
    expect(text).toMatch(/响应体：[\s\S]*\n\n\[3\] buildQueryRequest/);
  });

  test("追踪为空：不写“失败步骤”这一节", () => {
    const text = buildRunRepairReport({ ...base, steps: [] });
    expect(text).not.toContain("失败步骤");
    expect(text).toBe(
      [
        REPAIR_INTRO,
        "",
        "插件：yswg-image v1.3.1",
        "阶段：试跑（图片模型 yswg-gpt-image）",
        "错误：plugin_error · 统一结果的 status 不合法",
      ].join("\n"),
    );
  });

  test("什么都拿不到：只剩阶段，不出现 undefined 或 null", () => {
    const text = buildRunRepairReport({ steps: [] });
    expect(text).toBe(`${REPAIR_INTRO}\n\n阶段：试跑`);
    const partial = buildRunRepairReport({
      plugin: { key: "p", version: null },
      modelKey: null,
      kind: undefined,
      errorCode: null,
      errorMessage: null,
      steps: [],
    });
    expect(partial).not.toContain("undefined");
    expect(partial).not.toContain("null");
    expect(partial).toContain("插件：p\n阶段：试跑");
  });

  test("模型 key 与种类能拿到哪个写哪个", () => {
    expect(buildRunRepairReport({ modelKey: "m1", steps: [] })).toContain("阶段：试跑（模型 m1）");
    expect(buildRunRepairReport({ kind: "video", steps: [] })).toContain("阶段：试跑（视频模型）");
    expect(buildRunRepairReport({ kind: "text", modelKey: "m1", steps: [] })).toContain(
      "阶段：试跑（文本模型 m1）",
    );
  });

  test("错误码和错误文案能拿到哪个写哪个", () => {
    expect(buildRunRepairReport({ errorCode: "http_401", steps: [] })).toContain("错误：http_401");
    expect(buildRunRepairReport({ errorMessage: "上游返回 401", steps: [] })).toContain(
      "错误：上游返回 401",
    );
  });

  test("单个块超过 2KB 按字节截断，标记追加在截断处；没超限的块不受影响", () => {
    const steps = viewOf([
      hookStep("parseQueryResponse", {
        hook: { name: "parseQueryResponse", input: "x".repeat(5000), output: "短输出" },
      }),
    ]);
    const text = buildRunRepairReport({ ...base, steps });
    const line = text.split("\n").find((item) => item.startsWith("  输入"))!;
    expect(line.startsWith("  输入（截断到 2KB）：")).toBe(true);
    expect(line.endsWith(TRUNCATED_MARK)).toBe(true);
    expect(bytes(line.slice(line.indexOf("：") + 1))).toBeLessThanOrEqual(REPAIR_BLOCK_MAX_BYTES);
    expect(text).toContain("  输出：短输出");
    expect(text.split(TRUNCATED_MARK).length - 1).toBe(1);
  });

  test("多行的块被截断时标记写在最后一行；多字节内容不被切坏", () => {
    const lines = Array.from({ length: 400 }, (_, index) => `第${index}行：中文内容😀`);
    const steps = viewOf([
      httpStep(500, {
        response: { status: 500, body: lines.join("\n"), truncated: false },
      }),
    ]);
    const text = buildRunRepairReport({ ...base, steps });
    expect(text).toContain("  响应体（截断到 2KB）：\n");
    expect(text.trimEnd().endsWith(TRUNCATED_MARK)).toBe(true);
    expectWellFormed(text);
    const block = text.slice(
      text.indexOf("  响应体（截断到 2KB）：\n") + "  响应体（截断到 2KB）：\n".length,
    );
    // 每行多了 4 个缩进空格；去掉缩进后的块不超过 2KB
    const body = block
      .split("\n")
      .map((item) => item.replace(/^ {4}/, ""))
      .join("\n");
    expect(bytes(body)).toBeLessThanOrEqual(REPAIR_BLOCK_MAX_BYTES);
  });

  test("全文超过 16KB 截断：开头的说明、插件、阶段、错误都保留，结尾追加标记", () => {
    const steps = viewOf(
      Array.from({ length: 12 }, (_, index) =>
        hookStep(`hook${index}`, {
          error: `第 ${index} 步失败`,
          hook: {
            name: `hook${index}`,
            input: "i".repeat(5000),
            output: "o".repeat(5000),
            logs: Array.from({ length: 200 }, (_, line) => `日志第 ${line} 行，说明文字`),
          },
        }),
      ),
    );
    const text = buildRunRepairReport({ ...base, steps });
    expect(bytes(text)).toBeLessThanOrEqual(REPAIR_TOTAL_MAX_BYTES);
    expect(text.endsWith(TRUNCATED_MARK)).toBe(true);
    expect(text.startsWith(`${REPAIR_INTRO}\n\n插件：yswg-image v1.3.1\n阶段：试跑`)).toBe(true);
    expect(text).toContain("错误：plugin_error · 统一结果的 status 不合法");
    expect(text).toContain("[1] hook0");
    expect(text).not.toContain("[12] hook11");
    expectWellFormed(text);
  });

  test("全文截断同样不切坏多字节字符", () => {
    const steps = viewOf(
      Array.from({ length: 20 }, (_, index) =>
        hookStep(`h${index}`, {
          error: "错".repeat(2000),
          hook: { name: `h${index}`, input: "字😀".repeat(1500), output: "中".repeat(1500) },
        }),
      ),
    );
    const text = buildRunRepairReport({ ...base, steps });
    expect(bytes(text)).toBeLessThanOrEqual(REPAIR_TOTAL_MAX_BYTES);
    expectWellFormed(text);
  });

  test("错误文案过长同样按 2KB 截断；多行的错误文案缩进对齐", () => {
    const text = buildRunRepairReport({ ...base, errorMessage: "错".repeat(3000), steps: [] });
    const line = text.split("\n").find((item) => item.startsWith("错误"))!;
    expect(line.startsWith("错误（截断到 2KB）：plugin_error · ")).toBe(true);
    expect(bytes(line.slice(line.indexOf("：") + 1))).toBeLessThanOrEqual(REPAIR_BLOCK_MAX_BYTES);
    expect(line.endsWith(TRUNCATED_MARK)).toBe(true);

    const multi = buildRunRepairReport({
      ...base,
      errorMessage: "钩子抛出异常\n    at parse (plugin.js:12)",
      steps: [],
    });
    expect(multi).toContain("错误：\n  plugin_error · 钩子抛出异常\n      at parse (plugin.js:12)");
  });
});

describe("buildChannelCheckRepairReport：渠道连通性检查失败", () => {
  test("插件与版本、阶段、检查返回的说明", () => {
    expect(
      buildChannelCheckRepairReport({
        plugin: { key: "yswg-image", version: "1.3.1" },
        message: "HTTP 401",
      }),
    ).toBe(
      [REPAIR_INTRO, "", "插件：yswg-image v1.3.1", "阶段：渠道连通性检查", "错误：HTTP 401"].join(
        "\n",
      ),
    );
  });

  test("拿不到插件就不写“插件：”；说明为空就不写“错误：”", () => {
    expect(buildChannelCheckRepairReport({ message: "dial tcp: i/o timeout" })).toBe(
      `${REPAIR_INTRO}\n\n阶段：渠道连通性检查\n错误：dial tcp: i/o timeout`,
    );
    expect(buildChannelCheckRepairReport({ plugin: { key: "p", version: "" }, message: " " })).toBe(
      `${REPAIR_INTRO}\n\n插件：p\n阶段：渠道连通性检查`,
    );
  });

  test("说明有多行时，“错误：”单独一行，内容缩进 2 格", () => {
    const multi = buildChannelCheckRepairReport({
      message: 'HTTP 400\n{\n  "error": "x"\n}',
    });
    expect(multi).toContain('错误：\n  HTTP 400\n  {\n    "error": "x"\n  }');
  });

  test("说明过长按 2KB 截断，标签标明截断，内容末尾追加标记", () => {
    const long = buildChannelCheckRepairReport({ message: "长".repeat(3000) });
    const line = long.split("\n").find((item) => item.startsWith("错误"))!;
    expect(line.startsWith("错误（截断到 2KB）：")).toBe(true);
    expect(bytes(line.slice(line.indexOf("：") + 1))).toBeLessThanOrEqual(REPAIR_BLOCK_MAX_BYTES);
    expect(line.endsWith(TRUNCATED_MARK)).toBe(true);
  });
});

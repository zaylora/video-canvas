import { describe, expect, test } from "bun:test";

import type { TraceStep } from "@/api/admin-ai/type";
import {
  durationPercent,
  firstFailedStep,
  foldText,
  formatDuration,
  maxStepDuration,
  prettyValue,
  toTraceView,
  urlPath,
} from "@/utils/admin/trace";

const hookStep = (over: Partial<TraceStep> = {}): TraceStep => ({
  name: "submit",
  kind: "hook",
  hook: { name: "buildSubmitRequest", input: { a: 1 }, output: { b: 2 }, logs: ["x", "y"] },
  duration_ms: 12,
  ...over,
});

const httpStep = (status: number, over: Partial<TraceStep> = {}): TraceStep => ({
  name: "submit",
  kind: "http",
  request: { method: "POST", url: "https://x.test/v1", headers: { Authorization: "***" }, body: '{"a":1}' },
  response: { status, body: '{"ok":true}', truncated: false },
  duration_ms: 1800,
  ...over,
});

describe("prettyValue", () => {
  test("对象缩进；JSON 字符串展开；普通字符串原样；空值为空串", () => {
    expect(prettyValue({ a: 1 })).toBe('{\n  "a": 1\n}');
    expect(prettyValue('{"a":1}')).toBe('{\n  "a": 1\n}');
    expect(prettyValue("hello")).toBe("hello");
    expect(prettyValue(null)).toBe("");
    expect(prettyValue(undefined)).toBe("");
  });

  test("被截断的 JSON 字符串不崩，原样返回", () => {
    expect(prettyValue('{"a":"xx')).toBe('{"a":"xx');
  });
});

describe("toTraceView", () => {
  test("钩子步骤：标题取钩子名，带输入输出与 utils.log", () => {
    const view = toTraceView([hookStep()]);
    const step = view.steps[0];
    expect(step.kind).toBe("hook");
    expect(step.title).toBe("buildSubmitRequest");
    expect(step.index).toBe(1);
    expect(step.blocks.map((block) => block.label)).toEqual(["输入", "输出"]);
    expect(step.logs).toEqual(["x", "y"]);
    expect(step.failed).toBe(false);
  });

  test("HTTP 步骤：标题是方法加路径（不带域名），完整 URL 单独保留，带状态、请求头、响应体，截断标记透传", () => {
    const step = toTraceView([httpStep(200, { response: { status: 200, body: "{}", truncated: true } })]).steps[0];
    expect(step.title).toBe("POST /v1");
    expect(step.url).toBe("https://x.test/v1");
    expect(step.status).toBe(200);
    expect(step.truncated).toBe(true);
    expect(step.blocks.map((block) => block.label)).toEqual(["请求头", "请求体", "响应体"]);
  });

  test("有 error 或 HTTP 状态 >= 400 都算失败", () => {
    const view = toTraceView([hookStep({ error: "boom" }), httpStep(500), httpStep(404), httpStep(200)]);
    expect(view.steps.map((step) => step.failed)).toEqual([true, true, true, false]);
    expect(view.failedCount).toBe(3);
  });

  test("总耗时是各步骤之和；耗时缺失按 0 计且显示为 null", () => {
    const view = toTraceView([hookStep({ duration_ms: 10 }), httpStep(200, { duration_ms: 90 }), { name: "x", kind: "hook" }]);
    expect(view.totalMs).toBe(100);
    expect(view.steps[2].durationMs).toBeNull();
  });

  test("null、非数组、坏条目都不崩；未知 kind 归为 other", () => {
    expect(toTraceView(null).steps).toEqual([]);
    expect(toTraceView([null, 1, "x"]).steps).toEqual([]);
    expect(toTraceView([{ name: "z", kind: "weird" }]).steps[0].kind).toBe("other");
  });
});

describe("firstFailedStep / maxStepDuration / durationPercent", () => {
  const view = toTraceView([hookStep(), httpStep(200), hookStep({ error: "e1", duration_ms: 2 }), hookStep({ error: "e2" })]);

  test("第一个失败步骤是被默认展开并滚动到的那个", () => {
    expect(firstFailedStep(view)?.index).toBe(3);
    expect(firstFailedStep(toTraceView([hookStep()]))).toBeUndefined();
  });

  test("耗时条按最长步骤归一", () => {
    expect(maxStepDuration(view)).toBe(1800);
    expect(durationPercent(1800, 1800)).toBe(100);
    expect(durationPercent(900, 1800)).toBe(50);
  });

  test("很短的步骤至少 2% 可见；未知耗时或最长为 0 时为 0", () => {
    expect(durationPercent(1, 1800)).toBe(2);
    expect(durationPercent(null, 1800)).toBe(0);
    expect(durationPercent(10, 0)).toBe(0);
    expect(durationPercent(0, 100)).toBe(0);
  });
});

describe("foldText / formatDuration", () => {
  test("短文本不折叠；行数或字数超限折叠并给出预览", () => {
    expect(foldText("a\nb").folded).toBe(false);
    const long = foldText(Array.from({ length: 30 }, (_, index) => `line${index}`).join("\n"));
    expect(long.folded).toBe(true);
    expect(long.preview.split("\n")).toHaveLength(12);
    const wide = foldText("x".repeat(3000));
    expect(wide.folded).toBe(true);
    expect(wide.preview).toHaveLength(1500);
  });

  test("毫秒与秒的格式化", () => {
    expect(formatDuration(12)).toBe("12ms");
    expect(formatDuration(1500)).toBe("1.50s");
    expect(formatDuration(12_500)).toBe("12.5s");
    expect(formatDuration(null)).toBe("-");
  });
});

describe("urlPath", () => {
  test("完整 URL 取路径与查询串；相对路径与残缺 URL 原样返回", () => {
    expect(urlPath("https://api.example.com/v1/video/generations/abc?x=1")).toBe("/v1/video/generations/abc?x=1");
    expect(urlPath("https://api.example.com")).toBe("/");
    expect(urlPath("/v1/x")).toBe("/v1/x");
    expect(urlPath("")).toBe("");
  });
});

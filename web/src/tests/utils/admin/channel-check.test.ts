import { describe, expect, test } from "bun:test";

import {
  CHECK_UNSUPPORTED_MESSAGE,
  classifyCheck,
  classifyCheckError,
} from "@/utils/admin/channel-check";

describe("classifyCheck（接口正常返回）", () => {
  test("成功：绿色，带说明与耗时", () => {
    const outcome = classifyCheck({ ok: true, message: "HTTP 200", duration_ms: 120 });
    expect(outcome).toMatchObject({ kind: "ok", tone: "success" });
    expect(outcome.detail).toContain("HTTP 200");
    expect(outcome.detail).toContain("120ms");
  });

  test("插件不支持连通性检查：中性提示，不是错误", () => {
    const outcome = classifyCheck({
      ok: false,
      message: CHECK_UNSUPPORTED_MESSAGE,
      duration_ms: 0,
    });
    expect(outcome).toMatchObject({ kind: "unsupported", tone: "neutral" });
  });

  test("其他失败：红色，带后端说明", () => {
    const outcome = classifyCheck({
      ok: false,
      message: "dial tcp: i/o timeout",
      duration_ms: 5000,
    });
    expect(outcome).toMatchObject({ kind: "failed", tone: "danger" });
    expect(outcome.detail).toContain("timeout");
  });

  test("失败时保留后端原始说明（不含耗时），复制修复信息用它", () => {
    const outcome = classifyCheck({
      ok: false,
      message: "HTTP 401",
      duration_ms: 320,
    });
    expect(outcome.message).toBe("HTTP 401");
    expect(outcome.detail).toBe("HTTP 401 · 耗时 320ms");
  });

  test("连通和不支持检查没有原始说明字段", () => {
    expect(
      classifyCheck({ ok: true, message: "HTTP 200", duration_ms: 1 }).message,
    ).toBeUndefined();
    expect(
      classifyCheck({ ok: false, message: CHECK_UNSUPPORTED_MESSAGE, duration_ms: 0 }).message,
    ).toBeUndefined();
  });
});

describe("classifyCheckError（接口抛错）", () => {
  test("Key 未设置：引导去设 Key", () => {
    const outcome = classifyCheckError({ status: 409, code: 50015 });
    expect(outcome).toMatchObject({ kind: "no-key", tone: "warning" });
    expect(outcome.detail).toContain("设置 Key");
  });

  test("runner 不可用：提示不是渠道的问题", () => {
    const outcome = classifyCheckError({ status: 503, code: 50021 });
    expect(outcome).toMatchObject({ kind: "runner-down", tone: "warning" });
  });

  test("其他错误：笼统失败，详情在全局提示里；没有可复制的说明", () => {
    expect(classifyCheckError({ status: 500 }).kind).toBe("error");
    expect(classifyCheckError(new Error("x")).kind).toBe("error");
    expect(classifyCheckError({ status: 500 }).message).toBeUndefined();
  });

  test("插件调用失败（50022，502）：插件在检查钩子里抛异常或返回非法请求，原因在后端文案里，要能带走", () => {
    const outcome = classifyCheckError({
      status: 502,
      code: 50022,
      message:
        "插件钩子 buildCheckRequest 执行失败（js_error）：Cannot read properties of undefined",
    });
    expect(outcome).toMatchObject({ kind: "plugin-fault", tone: "danger", title: "插件本身出错" });
    expect(outcome.message).toContain("buildCheckRequest");
    expect(outcome.detail).toContain("buildCheckRequest");
  });

  test("插件调用失败但没有文案：仍认出是插件出错，只是没有可复制的说明", () => {
    const outcome = classifyCheckError({ status: 502, code: 50022 });
    expect(outcome.kind).toBe("plugin-fault");
    expect(outcome.message).toBeUndefined();
  });
});

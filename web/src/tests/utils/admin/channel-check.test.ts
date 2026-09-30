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
    const outcome = classifyCheck({ ok: false, message: CHECK_UNSUPPORTED_MESSAGE, duration_ms: 0 });
    expect(outcome).toMatchObject({ kind: "unsupported", tone: "neutral" });
  });

  test("其他失败：红色，带后端说明", () => {
    const outcome = classifyCheck({ ok: false, message: "dial tcp: i/o timeout", duration_ms: 5000 });
    expect(outcome).toMatchObject({ kind: "failed", tone: "danger" });
    expect(outcome.detail).toContain("timeout");
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

  test("其他错误：笼统失败，详情在全局提示里", () => {
    expect(classifyCheckError({ status: 500 }).kind).toBe("error");
    expect(classifyCheckError(new Error("x")).kind).toBe("error");
  });
});

import { describe, expect, test } from "bun:test";

import {
  clearRequestLog,
  getRequestLog,
  recordRequest,
  redactRequestBody,
  shouldLogRequest,
} from "@/utils/requests/request-log";

describe("shouldLogRequest", () => {
  test("只记录后台接口", () => {
    expect(shouldLogRequest("/admin/ai/models")).toBe(true);
    expect(shouldLogRequest("/canvases/1")).toBe(false);
    expect(shouldLogRequest(undefined)).toBe(false);
  });
});

describe("redactRequestBody", () => {
  test("设置 Key 的请求体不落日志", () => {
    expect(redactRequestBody("/admin/ai/channels/a/secret", '{"value":"sk-123"}')).toBe(
      "（Key 已隐藏）",
    );
  });

  test("存储配置的请求体里 secret_key 被隐藏，其他字段照常记录", () => {
    const body = JSON.stringify({
      name: "OSS",
      bucket: "vc",
      access_key_id: "ak",
      secret_key: "sk-123",
    });
    for (const input of [body, JSON.parse(body)]) {
      const logged = redactRequestBody("/admin/storages", input) as Record<string, unknown>;
      expect(logged.secret_key).toBe("（已隐藏）");
      expect(logged).toMatchObject({ name: "OSS", bucket: "vc", access_key_id: "ak" });
    }
    expect(JSON.stringify(redactRequestBody("/admin/storages/test", body))).not.toContain("sk-123");
  });

  test("文件上传只记文件名", () => {
    const form = new FormData();
    form.append("file", new File(["x"], "kling.js"));
    expect(redactRequestBody("/admin/ai/plugins", form)).toBe("（文件上传：kling.js）");
  });

  test("JSON 字符串解析回对象，空体返回 undefined", () => {
    expect(redactRequestBody("/admin/ai/models/a", '{"key":"a"}')).toEqual({ key: "a" });
    expect(redactRequestBody("/admin/ai/models/a", undefined)).toBeUndefined();
  });
});

describe("recordRequest", () => {
  test("新的在前，可以清空", () => {
    clearRequestLog();
    const base = { time: 0, duration: 1, method: "GET", status: 200, ok: true };
    recordRequest({ ...base, url: "/admin/ai/a" });
    recordRequest({ ...base, url: "/admin/ai/b" });
    expect(getRequestLog().map((entry) => entry.url)).toEqual(["/admin/ai/b", "/admin/ai/a"]);
    clearRequestLog();
    expect(getRequestLog()).toEqual([]);
  });
});

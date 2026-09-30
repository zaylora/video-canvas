import { describe, expect, test } from "bun:test";

import { findPathInJson, formatJsonText, parseJsonText, readConfigKey } from "@/utils/admin/json";

describe("parseJsonText", () => {
  test("合法 JSON", () => {
    expect(parseJsonText('{"a":1}')).toEqual({ ok: true, value: { a: 1 } });
  });

  test("空内容与语法错误给出原因，能定位时带行列", () => {
    expect(parseJsonText("  ")).toMatchObject({ ok: false });
    const bad = parseJsonText('{\n  "a": 1,\n  "b": \n}');
    expect(bad.ok).toBe(false);
    // 行列取决于引擎的报错信息（V8 有位置，JSC 没有），有就必须是正数
    if (!bad.ok) {
      expect(bad.message.length).toBeGreaterThan(0);
      if (bad.line !== undefined) expect(bad.line).toBeGreaterThanOrEqual(1);
    }
  });
});

describe("formatJsonText / readConfigKey", () => {
  test("格式化为 2 空格缩进；非法内容返回错误", () => {
    expect(formatJsonText('{"a":[1,2]}')).toEqual({
      ok: true,
      text: '{\n  "a": [\n    1,\n    2\n  ]\n}',
    });
    expect(formatJsonText("{").ok).toBe(false);
  });

  test("取正文里的 key", () => {
    expect(readConfigKey({ key: " rh-1 " })).toBe("rh-1");
    expect(readConfigKey({ key: 3 })).toBe("");
    expect(readConfigKey(null)).toBe("");
    expect(readConfigKey([])).toBe("");
  });
});

describe("findPathInJson", () => {
  const text = JSON.stringify(
    {
      key: "a",
      channels: [{ channel: "c", upstream_model: "" }],
      input_schema: { prompt: { type: "text" }, image: { type: "image" } },
    },
    null,
    2,
  );

  test("顶层键定位到所在行", () => {
    const hit = findPathInJson(text, "key");
    expect(hit?.line).toBe(2);
    expect(text.slice(hit!.index, hit!.index + hit!.length)).toBe('"key"');
  });

  test("数组下标被跳过，命中最后一段键名", () => {
    const hit = findPathInJson(text, "channels[0].upstream_model");
    expect(text.slice(hit!.index, hit!.index + hit!.length)).toBe('"upstream_model"');
    expect(hit!.line).toBe(6);
  });

  test("按顺序逐段找，避免命中前面同名的键", () => {
    const hit = findPathInJson(text, "input_schema.image.type");
    const before = text.slice(0, hit!.index);
    expect(before.includes('"image"')).toBe(true);
    expect(text.slice(hit!.index, hit!.index + hit!.length)).toBe('"type"');
  });

  test("中途找不到就停在已命中的最深一段；完全找不到返回 null", () => {
    const partial = findPathInJson(text, "input_schema.nope.deep");
    expect(text.slice(partial!.index, partial!.index + partial!.length)).toBe('"input_schema"');
    expect(findPathInJson(text, "missing")).toBeNull();
    expect(findPathInJson(text, "")).toBeNull();
  });
});

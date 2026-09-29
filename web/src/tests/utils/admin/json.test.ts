import { describe, expect, test } from "bun:test";

import { formatJsonText, parseJsonText, readConfigKey } from "@/utils/admin/json";

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

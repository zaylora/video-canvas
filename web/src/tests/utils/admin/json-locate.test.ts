import { describe, expect, test } from "bun:test";

import { locateJsonError, parseJsonText } from "@/utils/admin/json";

describe("locateJsonError", () => {
  test("语法正确返回 null", () => {
    expect(
      locateJsonError('{"a":[1,2,{"b":null}],"c":"x\\n\\u00e9","d":-1.5e3,"e":true}'),
    ).toBeNull();
    expect(locateJsonError("  [ ]  ")).toBeNull();
    expect(locateJsonError("{}")).toBeNull();
  });

  test("值缺失、多余逗号、缺冒号、未闭合、结尾多余内容都能定位", () => {
    expect(locateJsonError('{"a": ,}')).toBe(6);
    expect(locateJsonError('{"a":1,}')).toBe(7);
    expect(locateJsonError('{"a" 1}')).toBe(5);
    expect(locateJsonError('{"a":1')).toBe(6);
    expect(locateJsonError('{"a":1} x')).toBe(8);
    expect(locateJsonError("[1,,2]")).toBe(3);
  });

  test("字符串里的非法转义与裸换行", () => {
    expect(locateJsonError('{"a":"\\q"}')).toBe(6);
    expect(locateJsonError('{"a":"x\ny"}')).toBe(7);
    expect(locateJsonError("{'a':1}")).toBe(1);
  });
});

describe("parseJsonText 的行列", () => {
  test("多行文本里给出正确的行与列，不依赖引擎的报错格式", () => {
    const text = `{
  "key": "a",
  "credits": ,
  "kind": "video"
}`;
    const result = parseJsonText(text);
    expect(result.ok).toBe(false);
    if (result.ok) return;
    expect(result.line).toBe(3);
    expect(result.column).toBe(14);
  });

  test("单行错误列号从 1 开始", () => {
    const result = parseJsonText('{"a": ,}');
    expect(result.ok).toBe(false);
    if (result.ok) return;
    expect(result.line).toBe(1);
    expect(result.column).toBe(7);
  });
});

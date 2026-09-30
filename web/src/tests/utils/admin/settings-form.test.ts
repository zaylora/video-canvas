import { describe, expect, test } from "bun:test";

import {
  initialSettingValues,
  settingFields,
  validateSettingValues,
} from "@/utils/admin/settings-form";

const schema = {
  region: { type: "enum", label: "区域", options: ["cn", "global"], default: "cn", required: true },
  ttl: { type: "number", label: "有效期", default: 1800 },
  note: { type: "string", label: "备注" },
  debug: { type: "boolean", label: "调试" },
};

describe("settingFields", () => {
  test("按声明顺序输出字段，缺 label 时用字段名", () => {
    const fields = settingFields({ b: { type: "string", label: "" }, a: { type: "number", label: "A" } });
    expect(fields.map((field) => field.name)).toEqual(["b", "a"]);
    expect(fields[0].label).toBe("b");
  });

  test("坏条目跳过、未知类型按 string、options 只留字符串", () => {
    const fields = settingFields({
      ok: { type: "weird", label: "x", options: ["a", 1, "b"] },
      bad: null,
      arr: [],
    } as never);
    expect(fields).toHaveLength(1);
    expect(fields[0].type).toBe("string");
    expect(fields[0].options).toEqual(["a", "b"]);
  });

  test("null、非对象返回空数组", () => {
    expect(settingFields(null)).toEqual([]);
    expect(settingFields(undefined)).toEqual([]);
  });
});

describe("initialSettingValues", () => {
  const fields = settingFields(schema);

  test("没有已有取值时用默认值，boolean 默认 false，enum 无默认为空串", () => {
    const values = initialSettingValues(fields, null);
    expect(values).toEqual({ region: "cn", ttl: "1800", note: "", debug: false });
    expect(initialSettingValues(settingFields({ e: { type: "enum", label: "e", options: ["a"] } }), null).e).toBe("");
  });

  test("已有取值优先；放不进控件的值当作没有", () => {
    const values = initialSettingValues(fields, { region: "global", ttl: 60, debug: true, note: 5 });
    expect(values.region).toBe("global");
    expect(values.ttl).toBe("60");
    expect(values.debug).toBe(true);
    expect(values.note).toBe("5");
    expect(initialSettingValues(fields, { region: "mars" }).region).toBe("cn");
  });
});

describe("validateSettingValues", () => {
  const fields = settingFields(schema);

  test("正常取值转换成对应类型，空的可选项不出现", () => {
    const result = validateSettingValues(fields, { region: "cn", ttl: " 30 ", note: "", debug: true });
    expect(result.ok).toBe(true);
    expect(result.values).toEqual({ region: "cn", ttl: 30, debug: true });
  });

  test("必填缺失、数字格式不对、enum 取值不在选项里都给出错误", () => {
    const result = validateSettingValues(fields, { region: "", ttl: "abc", note: "", debug: false });
    expect(result.ok).toBe(false);
    expect(result.errors.region).toContain("区域");
    expect(result.errors.ttl).toContain("数字");
    const bad = validateSettingValues(fields, { region: "mars", ttl: "", note: "", debug: false });
    expect(bad.errors.region).toContain("cn / global");
  });

  test("boolean 永远有值", () => {
    expect(validateSettingValues(fields, { region: "cn" }).values.debug).toBe(false);
  });
});

import { describe, expect, test } from "bun:test";

import { VENDORS, vendorOf } from "@/constants/vendors";
import { readModelStrings } from "@/utils/admin/model-body";

describe("vendorOf：厂商 slug -> 清单项", () => {
  test("清单里的 slug 返回名称与 logo 组件", () => {
    const kling = vendorOf("kling");
    expect(kling?.name).toBe("可灵");
    expect(kling?.Icon).toBeDefined();
  });

  test("空、undefined、清单外、原型链上的名字都当作没选", () => {
    expect(vendorOf("")).toBeUndefined();
    expect(vendorOf(undefined)).toBeUndefined();
    expect(vendorOf("no-such-vendor")).toBeUndefined();
    expect(vendorOf("constructor")).toBeUndefined();
  });

  test("slug 都符合后端 vendor 的格式", () => {
    for (const slug of Object.keys(VENDORS)) expect(slug).toMatch(/^[a-z0-9][a-z0-9-]{0,63}$/);
  });
});

describe("readModelStrings：读正文里的字符串数组", () => {
  test("缺失、不是数组返回空数组，非字符串元素被丢弃", () => {
    expect(readModelStrings({}, "tags")).toEqual([]);
    expect(readModelStrings({ tags: "x" }, "tags")).toEqual([]);
    expect(readModelStrings(null, "tags")).toEqual([]);
    expect(readModelStrings({ tags: ["推荐", 1, null, "带音轨"] }, "tags")).toEqual([
      "推荐",
      "带音轨",
    ]);
  });
});

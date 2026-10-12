import { describe, expect, test } from "bun:test";

import { parseListParams, toListQuery, withListParam } from "@/utils/admin/list-params";

const parse = (search: string, keys: string[] = []) =>
  parseListParams(new URLSearchParams(search), keys);

describe("parseListParams", () => {
  test("没有参数：空关键字、第 1 页、无筛选", () => {
    expect(parse("")).toEqual({ q: "", page: 1, filters: {} });
  });

  test("读取 q、page 和白名单里的筛选", () => {
    expect(parse("q=oss&page=3&status=on&other=x", ["status"])).toEqual({
      q: "oss",
      page: 3,
      filters: { status: "on" },
    });
  });

  test("非法页码回到 1", () => {
    expect(parse("page=0").page).toBe(1);
    expect(parse("page=-2").page).toBe(1);
    expect(parse("page=abc").page).toBe(1);
    expect(parse("page=2.7").page).toBe(2);
  });

  test("筛选值为空或 all 视为没选", () => {
    expect(parse("status=&kind=all", ["status", "kind"]).filters).toEqual({});
  });
});

describe("toListQuery", () => {
  test("去掉空值，q 去首尾空格，带上分页", () => {
    expect(
      toListQuery({ q: "  oss ", page: 2, filters: { status: "on" } }, 20),
    ).toEqual({ q: "oss", page: 2, page_size: 20, status: "on" });
  });

  test("q 为空时不带 q", () => {
    expect(toListQuery({ q: "   ", page: 1, filters: {} }, 10)).toEqual({ page: 1, page_size: 10 });
  });
});

describe("withListParam", () => {
  test("改搜索词或筛选时页码清零", () => {
    const next = withListParam(new URLSearchParams("q=a&page=3"), "q", "b");
    expect(next.get("q")).toBe("b");
    expect(next.has("page")).toBe(false);
    const next2 = withListParam(new URLSearchParams("page=3"), "status", "on");
    expect(next2.get("status")).toBe("on");
    expect(next2.has("page")).toBe(false);
  });

  test("设置页码保留其它参数，第 1 页不写进 URL", () => {
    const next = withListParam(new URLSearchParams("q=a"), "page", "4");
    expect(next.get("page")).toBe("4");
    expect(next.get("q")).toBe("a");
    expect(withListParam(next, "page", "1").has("page")).toBe(false);
  });

  test("值为空或 all 时删掉该参数；不修改传入对象", () => {
    const src = new URLSearchParams("status=on");
    const next = withListParam(src, "status", "all");
    expect(next.has("status")).toBe(false);
    expect(src.get("status")).toBe("on");
  });
});

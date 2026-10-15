import { describe, expect, test } from "bun:test";

import { modelListQuery } from "@/utils/admin/model-list";

describe("modelListQuery", () => {
  test("固定限定在当前渠道，其余参数原样带上", () => {
    expect(
      modelListQuery({ q: "gpt", page: 2, page_size: 20, kind: "text", status: "online" }, "main"),
    ).toEqual({
      q: "gpt",
      page: 2,
      page_size: 20,
      kind: "text",
      status: "online",
      channel: "main",
    });
  });

  test("没有筛选时不带 kind 与 status", () => {
    const query = modelListQuery({ page: 1, page_size: 20 }, "main");
    expect("kind" in query).toBe(false);
    expect("status" in query).toBe(false);
  });

  test("能力选“待配置”：改成 status=pending，不带 kind，覆盖状态筛选", () => {
    expect(
      modelListQuery({ page: 1, page_size: 20, kind: "pending", status: "online" }, "main"),
    ).toEqual({
      page: 1,
      page_size: 20,
      status: "pending",
      channel: "main",
    });
  });
});

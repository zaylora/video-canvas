import { describe, expect, test } from "bun:test";

import { mixWorks, parseWorksScope } from "@/utils/home/works";

describe("作品页的范围（地址栏 ?scope=）", () => {
  test("认得全部、个人画布、个人短剧", () => {
    expect(parseWorksScope("all")).toBe("all");
    expect(parseWorksScope("mine")).toBe("mine");
    expect(parseWorksScope("drama")).toBe("drama");
  });

  test("没写、写错或已下线的协作画布，都回到个人画布", () => {
    expect(parseWorksScope(null)).toBe("mine");
    expect(parseWorksScope("shared")).toBe("mine");
    expect(parseWorksScope("xx")).toBe("mine");
  });
});

describe("「全部」里画布和短剧混排", () => {
  const canvas = (id: string, updatedAt: string) => ({ id, updatedAt });
  const drama = (id: string, updatedAt: string) => ({ id, updatedAt });

  test("按更新时间从新到旧排，各自带上类型", () => {
    const out = mixWorks(
      [canvas("c1", "2026-10-29T10:00:00Z"), canvas("c2", "2026-10-27T10:00:00Z")],
      [drama("d1", "2026-10-28T10:00:00Z")],
    );
    expect(out.map((work) => `${work.kind}:${work.item.id}`)).toEqual([
      "canvas:c1",
      "drama:d1",
      "canvas:c2",
    ]);
  });

  test("时间相同的画布排在短剧前面，顺序稳定", () => {
    const out = mixWorks(
      [canvas("c1", "2026-10-28T10:00:00Z")],
      [drama("d1", "2026-10-28T10:00:00Z")],
    );
    expect(out.map((work) => work.kind)).toEqual(["canvas", "drama"]);
  });

  test("任一边为空时就是另一边", () => {
    expect(mixWorks([], [drama("d1", "2026-10-28T10:00:00Z")])).toHaveLength(1);
    expect(mixWorks([], [])).toEqual([]);
  });
});

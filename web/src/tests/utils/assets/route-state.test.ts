import { describe, expect, test } from "bun:test";

import { buildAssetsSearch, parseAssetsParams } from "@/utils/assets/route-state";

describe("parseAssetsParams：地址栏参数", () => {
  test("没有参数时默认看生成历史，不在任何文件夹里", () => {
    expect(parseAssetsParams(new URLSearchParams(""))).toEqual({ tab: "history", folder: null });
  });

  test("认得两个范围，已下线的画布和未知范围都回到生成历史", () => {
    expect(parseAssetsParams(new URLSearchParams("tab=canvas")).tab).toBe("history");
    expect(parseAssetsParams(new URLSearchParams("tab=library")).tab).toBe("library");
    expect(parseAssetsParams(new URLSearchParams("tab=docs")).tab).toBe("history");
  });

  test("folder 只在个人资产库里生效", () => {
    expect(parseAssetsParams(new URLSearchParams("tab=library&folder=1f")).folder).toBe("1f");
    expect(parseAssetsParams(new URLSearchParams("tab=history&folder=1f")).folder).toBeNull();
  });

  test("认得「角色卡」范围，且它没有文件夹", () => {
    expect(parseAssetsParams(new URLSearchParams("tab=characters"))).toEqual({
      tab: "characters",
      folder: null,
    });
    expect(parseAssetsParams(new URLSearchParams("tab=characters&folder=1f")).folder).toBeNull();
    expect(buildAssetsSearch({ tab: "characters", folder: null })).toBe("tab=characters");
  });

  test("folder 不是十六进制串时当作根目录，防止乱填的值打到接口", () => {
    expect(parseAssetsParams(new URLSearchParams("tab=library&folder=../x")).folder).toBeNull();
    expect(parseAssetsParams(new URLSearchParams("tab=library&folder=")).folder).toBeNull();
  });
});

describe("buildAssetsSearch：写回地址栏", () => {
  test("默认的生成历史不带任何参数", () => {
    expect(buildAssetsSearch({ tab: "history", folder: null })).toBe("");
  });

  test("个人资产库带 tab，文件夹带 folder", () => {
    expect(buildAssetsSearch({ tab: "library", folder: null })).toBe("tab=library");
    expect(buildAssetsSearch({ tab: "library", folder: "1f" })).toBe("tab=library&folder=1f");
  });

  test("非资产库范围丢掉 folder，解析再构建能还原", () => {
    expect(buildAssetsSearch({ tab: "history", folder: "1f" })).toBe("");
    const state = { tab: "library", folder: "a3" } as const;
    expect(parseAssetsParams(new URLSearchParams(buildAssetsSearch(state)))).toEqual(state);
  });
});

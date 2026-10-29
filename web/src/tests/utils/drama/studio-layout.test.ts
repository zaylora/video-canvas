import { describe, expect, test } from "bun:test";

import { studioColumns } from "@/utils/drama/studio-layout";

describe("studioColumns：片场三栏按窗口宽度显示哪些", () => {
  test("宽屏（≥1280）：资产库、预览、编辑区三栏都在", () => {
    expect(studioColumns({ lg: true, xl: true })).toEqual(["library", "preview", "editor"]);
  });

  test("中屏（1024–1279）：收起资产库，留预览和编辑区", () => {
    expect(studioColumns({ lg: true, xl: false })).toEqual(["preview", "editor"]);
  });

  test("窄屏（<1024）：只留编辑区", () => {
    expect(studioColumns({ lg: false, xl: false })).toEqual(["editor"]);
  });
});

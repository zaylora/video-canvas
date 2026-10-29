import { describe, expect, test } from "bun:test";

import { assetAspect } from "@/utils/assets/aspect";

const asset = (kind: "image" | "video" | "audio", width: number | null, height: number | null) => ({
  kind,
  width,
  height,
});

describe("瀑布流卡片的画幅", () => {
  test("有宽高时按素材真实比例", () => {
    expect(assetAspect(asset("image", 1200, 1600))).toBe("1200 / 1600");
    expect(assetAspect(asset("video", 1920, 1080))).toBe("1920 / 1080");
  });

  test("太细长的竖图收到 9:16，太扁的横图收到 21:9，免得一张卡占满一列或细成一条", () => {
    expect(assetAspect(asset("image", 300, 2000))).toBe("9 / 16");
    expect(assetAspect(asset("image", 4000, 500))).toBe("21 / 9");
  });

  test("缺宽高时按类型给默认：视频 16:10，图片正方形", () => {
    expect(assetAspect(asset("video", null, null))).toBe("16 / 10");
    expect(assetAspect(asset("image", 0, 800))).toBe("1 / 1");
  });

  test("音频没有画面，固定 16:10 的波形卡", () => {
    expect(assetAspect(asset("audio", 640, 640))).toBe("16 / 10");
  });
});

import { describe, expect, test } from "bun:test";

import { probeFileAspect, shouldRecordAspect } from "@/utils/canvas/media-aspect";

const file = new File(["x"], "a.bin");

describe("probeFileAspect 上传前读本地文件的画幅", () => {
  test("图片按解码出的宽高算比例", async () => {
    const aspect = await probeFileAspect(file, "image", {
      imageSize: async () => ({ width: 800, height: 800 }),
      videoSize: async () => null,
    });
    expect(aspect).toBe(1);
  });

  test("视频按元数据里的宽高算比例", async () => {
    const aspect = await probeFileAspect(file, "video", {
      imageSize: async () => null,
      videoSize: async () => ({ width: 1080, height: 1920 }),
    });
    expect(aspect).toBeCloseTo(9 / 16);
  });

  test("音频没有画面，不读", async () => {
    const aspect = await probeFileAspect(file, "audio", {
      imageSize: async () => ({ width: 1, height: 1 }),
      videoSize: async () => ({ width: 1, height: 1 }),
    });
    expect(aspect).toBeNull();
  });

  test("读不出来、尺寸为 0 或读取抛错都是 null", async () => {
    const deps = (size: { width: number; height: number } | null) => ({
      imageSize: async () => size,
      videoSize: async () => size,
    });
    expect(await probeFileAspect(file, "image", deps(null))).toBeNull();
    expect(await probeFileAspect(file, "image", deps({ width: 0, height: 10 }))).toBeNull();
    expect(
      await probeFileAspect(file, "image", {
        imageSize: async () => {
          throw new Error("decode");
        },
        videoSize: async () => null,
      }),
    ).toBeNull();
  });

  test("读得太久（超时）就放弃，不拖住上传", async () => {
    const aspect = await probeFileAspect(
      file,
      "video",
      { imageSize: async () => null, videoSize: () => new Promise(() => {}) },
      20,
    );
    expect(aspect).toBeNull();
  });
});

describe("shouldRecordAspect 量到成片后要不要回写节点", () => {
  test("节点还没记过就回写", () => {
    expect(shouldRecordAspect(undefined, 1)).toBe(true);
  });

  test("记的和量到的差不多（浮点误差内）不回写，免得白白触发保存", () => {
    expect(shouldRecordAspect(16 / 9, 1920 / 1080)).toBe(false);
    expect(shouldRecordAspect(1, 1.0001)).toBe(false);
  });

  test("记的和成片对不上（比如重新生成成了别的比例）就更新", () => {
    expect(shouldRecordAspect(16 / 9, 1)).toBe(true);
  });
});

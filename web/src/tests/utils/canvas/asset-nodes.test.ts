import { describe, expect, test } from "bun:test";

import type { AssetDto } from "@/api/asset/type";
import { ASSET_STACK_COLUMNS, ASSET_STACK_GAP } from "@/constants/canvas";
import { nodeFrameSize } from "@/utils/canvas/node-frame";
import { planAssetNodes } from "@/utils/canvas/asset-nodes";

const asset = (id: string, overrides: Partial<AssetDto> = {}): AssetDto => ({
  id,
  kind: "image",
  url: `/files/g/${id}.png`,
  mimeType: "image/png",
  byteSize: 1,
  width: 1024,
  height: 1024,
  durationMs: null,
  fileName: `${id}.png`,
  ...overrides,
});

let seq = 0;
const plan = (assets: AssetDto[], labels: string[] = [], origin = { x: 100, y: 50 }) =>
  planAssetNodes(assets, origin, labels, () => `n${++seq}`);

describe("planAssetNodes：从资产库引入素材的节点方案", () => {
  test("约定：横向最多 4 列、间距 40px", () => {
    expect(ASSET_STACK_COLUMNS).toBe(4);
    expect(ASSET_STACK_GAP).toBe(40);
  });

  test("一个素材一个节点，第一个落在落点上", () => {
    const [node] = plan([asset("1")]);
    expect(node.position).toEqual({ x: 100, y: 50 });
    expect(node.type).toBe("canvas");
  });

  test("按素材类型建图片、视频、音频节点，写入地址、素材 ID、类型，状态为 done", () => {
    const nodes = plan([
      asset("1"),
      asset("2", { kind: "video", url: "/files/g/2.mp4", width: 1920, height: 1080 }),
      asset("3", { kind: "audio", url: "/files/g/3.mp3", width: null, height: null }),
    ]);
    expect(nodes.map((node) => node.data.kind)).toEqual(["image", "video", "audio"]);
    expect(nodes.map((node) => node.data.mediaType)).toEqual(["image", "video", "audio"]);
    expect(nodes.map((node) => node.data.src)).toEqual([
      "/files/g/1.png",
      "/files/g/2.mp4",
      "/files/g/3.mp3",
    ]);
    expect(nodes.map((node) => node.data.assetId)).toEqual(["1", "2", "3"]);
    for (const node of nodes) {
      expect(node.data.status).toBe("done");
      // 和「上传素材」落成的节点是同一种数据：不出生成面板、不触发上传
      expect(node.data.uploaded).toBe(true);
      expect(node.data.uploadProgress).toBeUndefined();
    }
  });

  test("画幅按素材的宽高写进 mediaAspect，音频和没有尺寸的素材不写", () => {
    const nodes = plan([
      asset("1", { width: 1920, height: 1080 }),
      asset("2", { kind: "audio", width: null, height: null }),
      asset("3", { width: null, height: null }),
      asset("4", { width: 0, height: 0 }),
    ]);
    expect(nodes[0].data.mediaAspect).toBeCloseTo(16 / 9);
    expect(nodes[1].data.mediaAspect).toBeUndefined();
    expect(nodes[2].data.mediaAspect).toBeUndefined();
    expect(nodes[3].data.mediaAspect).toBeUndefined();
  });

  test("同一行的节点横向排开，间距 40px（按各自节点框的宽度算）", () => {
    const nodes = plan([asset("1"), asset("2", { width: 1920, height: 1080 }), asset("3")]);
    const w1 = nodeFrameSize(1).width;
    const w2 = nodeFrameSize(1920 / 1080).width;
    expect(nodes[1].position).toEqual({ x: 100 + w1 + 40, y: 50 });
    expect(nodes[2].position).toEqual({ x: 100 + w1 + 40 + w2 + 40, y: 50 });
  });

  test("满 4 个换行：第 5 个回到落点的 x，y 下移（上一行最高的节点 + 40px）", () => {
    const nodes = plan([asset("1"), asset("2"), asset("3"), asset("4"), asset("5"), asset("6")]);
    // 4 个都是 1:1，行高 324
    expect(nodes.slice(0, 4).map((node) => node.position.y)).toEqual([50, 50, 50, 50]);
    expect(nodes[4].position).toEqual({ x: 100, y: 50 + 324 + 40 });
    expect(nodes[5].position.y).toBe(50 + 324 + 40);
    expect(nodes[5].position.x).toBe(100 + nodeFrameSize(1).width + 40);
  });

  test("换行高度取上一行最高的那个节点", () => {
    const tall = nodeFrameSize(0.5).height;
    const nodes = plan([
      asset("1"),
      asset("2", { width: 500, height: 1000 }),
      asset("3"),
      asset("4"),
      asset("5"),
    ]);
    expect(tall).toBeGreaterThan(324);
    expect(nodes[4].position).toEqual({ x: 100, y: 50 + tall + 40 });
  });

  test("节点名取文件名（去扩展名），画布里已有同名的编号", () => {
    const nodes = plan(
      [asset("1", { fileName: "雪地.png" }), asset("2", { fileName: "雪地.png" })],
      ["雪地"],
    );
    expect(nodes.map((node) => node.data.label)).toEqual(["雪地 2", "雪地 3"]);
    expect(nodes.map((node) => node.data.fileName)).toEqual(["雪地.png", "雪地.png"]);
  });

  test("没有文件名时用种类名", () => {
    const [node] = plan([asset("1", { fileName: null })]);
    expect(node.data.label).toBe("图片");
  });

  test("节点 ID 各不相同", () => {
    const nodes = plan([asset("1"), asset("2"), asset("3")]);
    expect(new Set(nodes.map((node) => node.id)).size).toBe(3);
  });

  test("没有素材时返回空数组", () => {
    expect(plan([])).toEqual([]);
  });
});

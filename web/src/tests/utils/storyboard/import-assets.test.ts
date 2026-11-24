import { describe, expect, test } from "bun:test";

import type { CanvasNodeData, NodeStoryboard, StoryboardShot } from "@/types";
import {
  appendLinkedShot,
  canvasAssetsOf,
  defaultSelection,
  importTargets,
  planImport,
  usedByShots,
} from "@/utils/storyboard/import-assets";

/** 一镜：只要 id 和关联，其余字段取默认 */
const shot = (id: string, link: Partial<StoryboardShot> = {}): StoryboardShot =>
  ({
    id,
    scene: "",
    shot: "中景",
    camera: "固定",
    action: "",
    dialogue: "",
    duration: 5,
    ...link,
  }) as StoryboardShot;

const board = (...shots: StoryboardShot[]): NodeStoryboard => ({ shots });

/** 一个画布节点：位置和产物按需给 */
function node(
  id: string,
  kind: "video" | "image" | "script",
  position: { x: number; y: number },
  data: Partial<CanvasNodeData> = {},
) {
  return { id, position, data: { kind, label: id, ...data } as CanvasNodeData };
}

describe("canvasAssetsOf", () => {
  test("只收指定种类的节点，按画布位置排（上→下、同高左→右）", () => {
    const nodes = [
      node("c", "video", { x: 0, y: 800 }),
      node("b", "video", { x: 400, y: 200 }),
      node("a", "video", { x: 0, y: 200 }),
      node("img", "image", { x: 0, y: 0 }),
      node("txt", "script", { x: 0, y: 100 }),
    ];
    const list = canvasAssetsOf(nodes, "video", board());
    expect(list.map((item) => item.id)).toEqual(["a", "b", "c"]);
    expect(list.map((item) => item.seq)).toEqual([1, 2, 3]);
  });

  test("出过片才算 ready；只有本地 blob 的不算，老节点靠 src + assetId 兜底", () => {
    const nodes = [
      node("done", "video", { x: 0, y: 0 }, { src: "/f/1.mp4", assetId: "1" }),
      node("blob", "video", { x: 0, y: 100 }, { src: "blob:local", assetId: "2" }),
      node("idle", "video", { x: 0, y: 200 }, { status: "running" }),
    ];
    const list = canvasAssetsOf(nodes, "video", board());
    expect(list.map((item) => [item.id, item.ready])).toEqual([
      ["done", true],
      ["blob", false],
      ["idle", false],
    ]);
    expect(list[0].src).toBe("/f/1.mp4");
  });

  test("已经挂在哪几镜如实标出，一个节点被两镜共用就列两个镜号", () => {
    const nodes = [node("v1", "video", { x: 0, y: 0 }), node("v2", "video", { x: 0, y: 100 })];
    const list = canvasAssetsOf(
      nodes,
      "video",
      board(
        shot("s1", { videoNodeId: "v1" }),
        shot("s2", { videoNodeId: "v2" }),
        shot("s3", { videoNodeId: "v1" }),
      ),
    );
    expect(list[0].usedBy).toEqual([1, 3]);
    expect(list[1].usedBy).toEqual([2]);
  });

  test("图片和视频各看各的关联字段", () => {
    const nodes = [node("i1", "image", { x: 0, y: 0 }, { src: "/f/1.png", assetId: "9" })];
    const b = board(shot("s1", { imageNodeId: "i1" }));
    expect(canvasAssetsOf(nodes, "image", b)[0]).toMatchObject({ ready: true, usedBy: [1] });
    expect(canvasAssetsOf(nodes, "video", b)).toEqual([]);
  });
});

describe("usedByShots / importTargets", () => {
  test("没挂过的镜才算目标行，按行序", () => {
    const b = board(
      shot("s1", { videoNodeId: "v1" }),
      shot("s2"),
      shot("s3", { videoNodeId: "v2" }),
      shot("s4"),
    );
    expect(importTargets(b, "video")).toEqual([1, 3]);
    expect(importTargets(b, "image")).toEqual([0, 1, 2, 3]);
    expect(usedByShots(b, "video")).toEqual(
      new Map([
        ["v1", [1]],
        ["v2", [3]],
      ]),
    );
  });

  test("默认只勾没被用过的素材", () => {
    const nodes = [node("v1", "video", { x: 0, y: 0 }), node("v2", "video", { x: 0, y: 100 })];
    const list = canvasAssetsOf(nodes, "video", board(shot("s1", { videoNodeId: "v1" })));
    expect(defaultSelection(list)).toEqual(["v2"]);
  });
});

describe("planImport", () => {
  const assets = canvasAssetsOf(
    [
      node("v1", "video", { x: 0, y: 0 }, { src: "/f/1.mp4", assetId: "1" }),
      node("v2", "video", { x: 0, y: 100 }, { src: "/f/2.mp4", assetId: "2" }),
      node("v3", "video", { x: 0, y: 200 }, { src: "/f/3.mp4", assetId: "3" }),
    ],
    "video",
    board(),
  );

  test("按勾选顺序补进空镜头，跳过已有视频的行", () => {
    const b = board(shot("s1"), shot("s2", { videoNodeId: "old" }), shot("s3"));
    const plan = planImport(b, [assets[0], assets[1]], "video");
    expect(plan.links).toEqual([
      { shotId: "s1", nodeId: "v1" },
      { shotId: "s3", nodeId: "v2" },
    ]);
    expect(plan.leftover).toBe(0);
    expect(plan.appended).toEqual([]);
  });

  test("空镜头不够时不追加就记成挂不上", () => {
    const b = board(shot("s1"));
    const plan = planImport(b, assets, "video");
    expect(plan.links).toEqual([{ shotId: "s1", nodeId: "v1" }]);
    expect(plan.leftover).toBe(2);
    expect(plan.appended).toEqual([]);
  });

  test("勾了追加：多出来的按顺序变成新行，画面描述先取节点标题", () => {
    const b = board(shot("s1"));
    const plan = planImport(b, assets, "video", { append: true });
    expect(plan.links).toEqual([{ shotId: "s1", nodeId: "v1" }]);
    expect(plan.appended).toEqual([
      { nodeId: "v2", label: "v2" },
      { nodeId: "v3", label: "v3" },
    ]);
    expect(plan.leftover).toBe(0);
  });
});

describe("appendLinkedShot", () => {
  test("追加一行并把视频挂上，画面描述取节点标题", () => {
    let n = 0;
    const original = board(shot("s1"));
    const next = appendLinkedShot(original, "v9", "video", "S9-v2", () => `new${++n}`);
    expect(next.shots).toHaveLength(2);
    expect(next.shots[1]).toMatchObject({ id: "new1", scene: "S9-v2", videoNodeId: "v9" });
    // 原来那行不动
    expect(next.shots[0]).toBe(original.shots[0]);
  });

  test("图片走 imageNodeId，不动 videoNodeId", () => {
    const next = appendLinkedShot(board(), "i9", "image", "分镜图 1");
    expect(next.shots[0]).toMatchObject({ imageNodeId: "i9", scene: "分镜图 1" });
    expect(next.shots[0].videoNodeId).toBeUndefined();
  });
});

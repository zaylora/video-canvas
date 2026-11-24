import { describe, expect, test } from "bun:test";

import { ADD_NODE_MENU, NODE_META } from "@/constants/canvas/node-library";
import { NODE_TOOLBAR } from "@/constants/node-toolbar";
import type { CanvasNode } from "@/types";
import { canConnectKinds, getAllowedKinds } from "@/utils/canvas/canvas";
import { deserializeGraph, serializeGraph } from "@/utils/canvas/canvas-persistence";
import { canLinkNodes } from "@/utils/canvas/link-rule";
import { toolbarGate } from "@/utils/canvas/node-toolbar-state";

import { makeData } from "../tasks/fixtures";

const viewport = { x: 0, y: 0, zoom: 1 };

/** 只判种类时够用的最小节点数据 */
const end = (kind: CanvasNode["data"]["kind"]) => ({ kind }) as never;

describe("成片合成节点：注册与连线规则", () => {
  test("加号菜单里有「成片合成」，紧跟视频生成：它吃的是视频", () => {
    const kinds = ADD_NODE_MENU.map((item) => item.kind);
    expect(kinds).toContain("compose");
    expect(kinds.indexOf("compose")).toBe(kinds.indexOf("video") + 1);
  });

  test("节点种类清单里有 compose，菜单文案和图标齐全", () => {
    const meta = NODE_META.get("compose");
    expect(meta?.label).toBe("成片合成");
    expect(meta?.icon).toBeTruthy();
    expect(meta?.placeholderIcon).toBeTruthy();
  });

  test("视频能连进成片节点；图片、音频、文本都连不进来", () => {
    expect(canLinkNodes(end("video"), end("compose"))).toBe(true);
    expect(canLinkNodes(end("image"), end("compose"))).toBe(false);
    expect(canLinkNodes(end("audio"), end("compose"))).toBe(false);
    expect(canLinkNodes(end("script"), end("compose"))).toBe(false);
    expect(canLinkNodes(end("character"), end("compose"))).toBe(false);
  });

  test("成片节点是终点：不往任何节点连出去，也没有可拉线的出口", () => {
    expect(getAllowedKinds("compose", "source")).toEqual([]);
    expect(canConnectKinds("compose", "source", "video")).toBe(false);
    expect(NODE_TOOLBAR.compose.actions).toEqual([]);
  });

  test("从视频节点拉线时，可建的种类里有成片合成；从图片拉线时没有", () => {
    expect(getAllowedKinds("video", "source")).toContain("compose");
    expect(getAllowedKinds("image", "source")).not.toContain("compose");
    // 反查：能生成成片节点的上游只有视频
    expect(getAllowedKinds("compose", "target")).toEqual(["video"]);
  });

  test("成片节点不出功能区：合成与下载都在正文的合成条上，不给第二个入口", () => {
    expect(toolbarGate(makeData({ kind: "compose", label: "成片" })).visible).toBe(false);
  });
});

describe("成片合成节点：存档往返", () => {
  test("compose 节点的成片四件套存得下、读得回（不依赖 storyboard 字段）", () => {
    const node: CanvasNode = {
      id: "compose-1",
      type: "canvas",
      position: { x: 0, y: 0 },
      data: makeData({
        kind: "compose",
        label: "成片合成",
        finalAssetId: "2048",
        finalFingerprint: "ff00",
        finalDurationMs: 14848,
        finalSegmentCount: 3,
      }),
    };
    const saved = serializeGraph([node], [], viewport);
    expect(saved.nodes[0].data).toMatchObject({
      kind: "compose",
      finalAssetId: "2048",
      finalFingerprint: "ff00",
      finalDurationMs: 14848,
      finalSegmentCount: 3,
    });

    const back = deserializeGraph(saved).nodes as CanvasNode[];
    expect(back[0].data.kind).toBe("compose");
    expect(serializeGraph(back, [], viewport).nodes[0].data).toMatchObject({
      finalAssetId: "2048",
      finalSegmentCount: 3,
    });
  });

  test("手动调过的段序 composeOrder 存得下、读得回（否则刷新就回到按位置排）", () => {
    const node: CanvasNode = {
      id: "compose-2",
      type: "canvas",
      position: { x: 0, y: 0 },
      data: makeData({ kind: "compose", label: "成片合成", composeOrder: ["c", "a", "b"] }),
    };
    const saved = serializeGraph([node], [], viewport);
    expect(saved.nodes[0].data).toMatchObject({ composeOrder: ["c", "a", "b"] });

    const back = deserializeGraph(saved).nodes as CanvasNode[];
    expect(back[0].data.composeOrder).toEqual(["c", "a", "b"]);
  });

  test("没调过序的成片节点不写空数组：存档不留噪音字段", () => {
    const node: CanvasNode = {
      id: "compose-3",
      type: "canvas",
      position: { x: 0, y: 0 },
      data: makeData({ kind: "compose", label: "成片合成" }),
    };
    expect(serializeGraph([node], [], viewport).nodes[0].data).not.toHaveProperty("composeOrder");
  });
});

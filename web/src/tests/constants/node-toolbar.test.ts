import { describe, expect, test } from "bun:test";

import { NODE_LIBRARY } from "@/constants/canvas/node-library";
import { NODE_TOOLBAR, toolbarActionIds } from "@/constants/node-toolbar";

describe("节点功能区配置（设计稿 6.16）", () => {
  test("每种节点都有配置，不会选中某种节点时功能区是空的", () => {
    for (const { kind } of NODE_LIBRARY) {
      expect(NODE_TOOLBAR[kind]).toBeDefined();
    }
  });

  test("同一种节点里按钮和菜单项的 id 不重复", () => {
    for (const { kind } of NODE_LIBRARY) {
      const ids = toolbarActionIds(NODE_TOOLBAR[kind]);
      expect(new Set(ids).size).toBe(ids.length);
    }
  });

  test("带菜单的按钮，每一组至少有一项", () => {
    for (const { kind } of NODE_LIBRARY) {
      for (const action of NODE_TOOLBAR[kind].actions) {
        if (!action.menu) continue;
        expect(action.menu.length).toBeGreaterThan(0);
        for (const group of action.menu) expect(group.items.length).toBeGreaterThan(0);
      }
    }
  });

  test("历史入口改到节点右上角的徽标：功能区任何节点都不再有历史按钮", () => {
    for (const { kind } of NODE_LIBRARY) {
      expect(NODE_TOOLBAR[kind].tail as string[]).not.toContain("history");
    }
  });

  test("视频节点照参考图：截取帧有首帧、尾帧、自定义，工具里有补帧和深度动作捕捉", () => {
    const labels = (id: string) =>
      NODE_TOOLBAR.video.actions
        .find((action) => action.id === id)
        ?.menu?.flatMap((group) => group.items.map((item) => item.label));
    expect(labels("frame")).toEqual(["首帧", "尾帧", "自定义"]);
    expect(labels("tools")).toEqual(["补帧", "深度动作捕捉"]);
  });

  test("图片、视频、音频的查看类按钮最后一项是「加入资产库」，文本没有", () => {
    for (const kind of ["image", "video", "audio"] as const) {
      expect(NODE_TOOLBAR[kind].tail.at(-1)).toBe("addToLibrary");
    }
    expect(NODE_TOOLBAR.script.tail as string[]).not.toContain("addToLibrary");
  });

  test("音频没有放大，视频和图片有放大和下载", () => {
    expect(NODE_TOOLBAR.audio.tail).not.toContain("zoom");
    for (const kind of ["image", "video"] as const) {
      expect(NODE_TOOLBAR[kind].tail).toEqual(expect.arrayContaining(["zoom", "download"]));
    }
  });

  test("图片节点只剩图片编辑、扩图、宫格切分，占位按钮都删了", () => {
    expect(NODE_TOOLBAR.image.actions.map((action) => action.id)).toEqual([
      "edit",
      "outpaint",
      "grid",
    ]);
  });

  test("图片编辑是纯前端涂鸦，不带积分星；扩图仍收费", () => {
    const paid = (id: string) =>
      NODE_TOOLBAR.image.actions.find((action) => action.id === id)?.paid;
    expect(paid("edit")).toBeFalsy();
    expect(paid("grid")).toBeFalsy();
    expect(paid("outpaint")).toBe(true);
  });

  test("宫格切分的菜单：4/9/16/25 宫格，再加带子面板的「自定义」", () => {
    const grid = NODE_TOOLBAR.image.actions.find((action) => action.id === "grid");
    const items = grid?.menu?.flatMap((group) => group.items) ?? [];
    expect(items.map((item) => item.label)).toEqual([
      "4 宫格 (2×2)",
      "9 宫格 (3×3)",
      "16 宫格 (4×4)",
      "25 宫格 (5×5)",
      "自定义",
    ]);
    expect(items.map((item) => item.id)).toEqual([
      "grid-2x2",
      "grid-3x3",
      "grid-4x4",
      "grid-5x5",
      "grid-custom",
    ]);
    expect(items.at(-1)?.submenu).toBe("grid-picker");
    expect(items.slice(0, -1).every((item) => !item.submenu)).toBe(true);
  });

  test("文本节点没有加工类按钮，只剩复制和放大预览", () => {
    expect(NODE_TOOLBAR.script.actions).toEqual([]);
    expect(NODE_TOOLBAR.script.tail).toEqual(["copy", "zoom"]);
  });

  test("视频节点的按钮不受影响", () => {
    expect(NODE_TOOLBAR.video.actions.map((action) => action.id)).toEqual([
      "retake",
      "upscale",
      "edit",
      "frame",
      "trim",
      "tools",
    ]);
  });
});

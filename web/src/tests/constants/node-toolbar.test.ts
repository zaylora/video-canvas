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

  test("只有产出素材版本的节点带历史按钮，文本节点没有", () => {
    expect(NODE_TOOLBAR.script.tail).not.toContain("history");
    for (const kind of ["image", "video", "audio"] as const) {
      expect(NODE_TOOLBAR[kind].tail).toContain("history");
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

  test("音频没有放大，视频和图片有放大和下载", () => {
    expect(NODE_TOOLBAR.audio.tail).not.toContain("zoom");
    for (const kind of ["image", "video"] as const) {
      expect(NODE_TOOLBAR[kind].tail).toEqual(expect.arrayContaining(["zoom", "download"]));
    }
  });
});

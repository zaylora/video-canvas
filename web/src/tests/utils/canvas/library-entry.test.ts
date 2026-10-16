import { describe, expect, test } from "bun:test";

import { NODE_TOOLBAR } from "@/constants/node-toolbar";
import type { CanvasNodeData } from "@/types";
import { addNodeToLibrary, canAddToLibrary } from "@/utils/canvas/library-entry";
import { toolbarGate, visibleTail } from "@/utils/canvas/node-toolbar-state";

const node = (over: Partial<CanvasNodeData> = {}): CanvasNodeData => ({
  kind: "image",
  label: "图片",
  src: "/files/a.png",
  mediaType: "image",
  assetId: "12",
  status: "done",
  ...over,
});

describe("canAddToLibrary：节点「加入资产库」的显示条件", () => {
  test("图片、视频、音频节点有素材 ID 且 status 为 done 才显示", () => {
    expect(canAddToLibrary(node())).toBe(true);
    expect(canAddToLibrary(node({ kind: "video", mediaType: "video" }))).toBe(true);
    expect(canAddToLibrary(node({ kind: "audio", mediaType: "audio" }))).toBe(true);
  });

  test("不限来源：生成的、上传的都可以", () => {
    expect(canAddToLibrary(node({ uploaded: true, fileName: "a.png" }))).toBe(true);
    expect(canAddToLibrary(node({ uploaded: undefined, taskId: "9" }))).toBe(true);
  });

  test("没有素材 ID、还在生成中、失败、空闲都不显示", () => {
    expect(canAddToLibrary(node({ assetId: undefined }))).toBe(false);
    expect(canAddToLibrary(node({ status: "running" }))).toBe(false);
    expect(canAddToLibrary(node({ status: "error" }))).toBe(false);
    expect(canAddToLibrary(node({ status: undefined }))).toBe(false);
  });

  test("文本节点不显示", () => {
    expect(canAddToLibrary(node({ kind: "script", text: "x" }))).toBe(false);
  });
});

describe("visibleTail：功能区查看类按钮", () => {
  test("图片、视频、音频的配置里有「加入资产库」，文本没有", () => {
    expect(NODE_TOOLBAR.image.tail).toContain("addToLibrary");
    expect(NODE_TOOLBAR.video.tail).toContain("addToLibrary");
    expect(NODE_TOOLBAR.audio.tail).toContain("addToLibrary");
    expect(NODE_TOOLBAR.script.tail).not.toContain("addToLibrary");
  });

  test("没有处理函数时「加入资产库」不出现，其余按钮照旧", () => {
    expect(visibleTail(NODE_TOOLBAR.image.tail, {})).toEqual(["zoom", "download"]);
  });

  test("有处理函数时出现，排在放大、下载之后", () => {
    expect(visibleTail(NODE_TOOLBAR.image.tail, { addToLibrary: () => {} })).toEqual([
      "zoom",
      "download",
      "addToLibrary",
    ]);
  });
});

describe("不可用时的禁用：沿用功能区的 gate", () => {
  test("生成中时 actionsDisabled 为 true，加入资产库按钮随之禁用", () => {
    expect(toolbarGate(node({ status: "running" })).actionsDisabled).toBe(true);
    expect(toolbarGate(node()).actionsDisabled).toBe(false);
  });
});

describe("addNodeToLibrary：确认后调接口", () => {
  test("用节点的素材 ID 和选中的文件夹调 addToLibrary", async () => {
    const calls: [string, string | null][] = [];
    const result = await addNodeToLibrary(node(), "f1", {
      addToLibrary: async (assetId, folderId) => {
        calls.push([assetId, folderId]);
        return { id: "a1" } as never;
      },
    });
    expect(calls).toEqual([["12", "f1"]]);
    expect(result.id).toBe("a1");
  });

  test("节点不满足条件时直接抛错，不调接口", async () => {
    let called = false;
    await expect(
      addNodeToLibrary(node({ assetId: undefined }), null, {
        addToLibrary: async () => {
          called = true;
          return {} as never;
        },
      }),
    ).rejects.toThrow();
    expect(called).toBe(false);
  });
});

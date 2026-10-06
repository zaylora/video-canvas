import { describe, expect, test } from "bun:test";

import type { CanvasDetailDto, CanvasGraphDto } from "@/api/canvas/type";
import type { Draft } from "@/utils/canvas/draft-store";
import { loadCanvasForEditing } from "@/utils/canvas/open-canvas";

const graph = (ids: string[]) =>
  ({
    nodes: ids.map((id) => ({ id })),
    edges: [],
    viewport: { x: 0, y: 0, zoom: 1 },
  }) as unknown as CanvasGraphDto;

const cloud: CanvasDetailDto = {
  id: "c1",
  title: "标题",
  description: null,
  version: 5,
  graph: graph(["a"]),
  createdAt: "",
  updatedAt: "",
};

function setup(draft: Draft | null, userId: string | null = "7") {
  const removed: string[] = [];
  const deps = {
    canvasId: "c1",
    userId,
    getCanvas: async () => cloud,
    loadDraft: async () => draft,
    removeDraft: async (id: string) => void removed.push(id),
  };
  return { deps, removed };
}

const draft = (over: Partial<Draft> = {}): Draft => ({
  schemaVersion: 1,
  graph: graph(["a", "b"]),
  baseVersion: 5,
  cloudDirty: true,
  savedAt: 1,
  ...over,
});

describe("loadCanvasForEditing", () => {
  test("没有草稿：原样返回云端画布", async () => {
    const { deps } = setup(null);
    expect(await loadCanvasForEditing(deps)).toEqual({ canvas: cloud, recovery: null });
  });

  test("认不出当前用户：不看草稿，行为和以前一致", async () => {
    const { deps } = setup(draft(), null);
    expect(await loadCanvasForEditing(deps)).toEqual({ canvas: cloud, recovery: null });
  });

  test("草稿基于当前云端版本：用草稿内容打开，标记为已恢复，版本仍是云端版本", async () => {
    const { deps } = setup(draft());
    const opened = await loadCanvasForEditing(deps);
    expect(opened.recovery).toEqual({ kind: "restored" });
    expect(opened.canvas.graph).toEqual(graph(["a", "b"]));
    expect(opened.canvas.version).toBe(5);
    expect(opened.canvas.title).toBe("标题");
  });

  test("草稿基于旧版本：打开云端最新，并带上草稿内容交给冲突弹窗", async () => {
    const { deps } = setup(draft({ baseVersion: 4 }));
    const opened = await loadCanvasForEditing(deps);
    expect(opened.canvas).toEqual(cloud);
    expect(opened.recovery).toEqual({ kind: "conflict", graph: graph(["a", "b"]) });
  });

  test("草稿已同步：丢弃并删除，打开云端版本", async () => {
    const { deps, removed } = setup(draft({ cloudDirty: false }));
    const opened = await loadCanvasForEditing(deps);
    expect(opened).toEqual({ canvas: cloud, recovery: null });
    expect(removed).toEqual(["c1"]);
  });

  test("云端读取失败：错误照常抛出，由页面显示加载失败", async () => {
    const { deps } = setup(null);
    deps.getCanvas = async () => {
      throw new Error("404");
    };
    await expect(loadCanvasForEditing(deps)).rejects.toThrow("404");
  });
});

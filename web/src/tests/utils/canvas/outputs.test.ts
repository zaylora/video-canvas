import { describe, expect, test } from "bun:test";

import type { CanvasNodeData, NodeOutput } from "@/types";
import { serializeGraph } from "@/utils/canvas/canvas-persistence";
import {
  MAX_NODE_OUTPUTS,
  activeOutputIdOf,
  appendOutput,
  readOutputs,
  selectOutput,
} from "@/utils/canvas/outputs";
import { planBackfill } from "@/utils/tasks/backfill";
import { buildSubmittedPatch } from "@/utils/tasks/submit";

import { makeData, makeTask, succeeded } from "../tasks/fixtures";

const out = (id: string, over: Partial<NodeOutput> = {}): NodeOutput => ({
  id,
  src: `/files/${id}.png`,
  mediaType: "image",
  assetId: id,
  createdAt: 1,
  ...over,
});

describe("readOutputs：读出节点的历次产物", () => {
  test("旧画布只有 src + assetId 时当成唯一一版", () => {
    const data = makeData({ src: "/files/a.png", assetId: "5", mediaType: "image" });
    expect(readOutputs(data)).toEqual([
      expect.objectContaining({ id: "5", src: "/files/a.png", assetId: "5", mediaType: "image" }),
    ]);
  });

  test("本地 blob、没有 assetId 的素材不算一版", () => {
    expect(readOutputs(makeData({ src: "blob:x", assetId: "5" }))).toEqual([]);
    expect(readOutputs(makeData({ src: "/files/a.png" }))).toEqual([]);
  });
});

describe("appendOutput / selectOutput", () => {
  test("新产物追加到末尾并设为当前版本，镜像字段跟着换", () => {
    const data: CanvasNodeData = makeData({ outputs: [out("1")], activeOutputId: "1" });
    const patch = appendOutput(data, out("2", { mediaType: "video", src: "/files/2.mp4" }));
    expect(patch.outputs?.map((item) => item.id)).toEqual(["1", "2"]);
    expect(patch).toMatchObject({
      activeOutputId: "2",
      src: "/files/2.mp4",
      assetId: "2",
      mediaType: "video",
    });
  });

  test("同一素材不重复入列", () => {
    const data = makeData({ outputs: [out("1"), out("2")], activeOutputId: "1" });
    expect(appendOutput(data, out("2")).outputs).toHaveLength(2);
  });

  test(`超过 ${MAX_NODE_OUTPUTS} 版丢最旧的`, () => {
    const outputs = Array.from({ length: MAX_NODE_OUTPUTS }, (_, i) => out(String(i)));
    const patch = appendOutput(makeData({ outputs }), out("new"));
    expect(patch.outputs).toHaveLength(MAX_NODE_OUTPUTS);
    expect(patch.outputs?.[0].id).toBe("1");
    expect(patch.outputs?.at(-1)?.id).toBe("new");
  });

  test("切版本只改指向和镜像；找不到的版本返回 null", () => {
    const data = makeData({
      outputs: [out("1"), out("2")],
      activeOutputId: "2",
      src: "/files/2.png",
    });
    expect(selectOutput(data, "1")).toMatchObject({ activeOutputId: "1", src: "/files/1.png" });
    expect(selectOutput(data, "9")).toBeNull();
  });

  test("activeOutputId 失效时认最后一版", () => {
    expect(activeOutputIdOf(makeData({ outputs: [out("1"), out("2")], activeOutputId: "x" }))).toBe(
      "2",
    );
  });
});

describe("回填与提交保留历史", () => {
  test("成功回填把产物追加进 outputs，旧版本保留", () => {
    const data = makeData({
      status: "running",
      taskId: "1",
      outputs: [out("5", { mediaType: "video" })],
      activeOutputId: "5",
    });
    const patch = planBackfill(data, succeeded());
    expect(patch?.outputs?.map((item) => item.id)).toEqual(["5", "77"]);
    expect(patch).toMatchObject({ activeOutputId: "77", src: "/files/a.mp4", assetId: "77" });
    expect(patch?.outputs?.[1]).toMatchObject({ taskId: "1", mediaType: "video" });
  });

  test("取消时有历史就退回当前那一版", () => {
    const data = makeData({
      status: "running",
      taskId: "1",
      outputs: [out("5")],
      activeOutputId: "5",
    });
    expect(planBackfill(data, makeTask({ status: "canceled" }))).toMatchObject({
      status: "done",
      src: "/files/5.png",
      assetId: "5",
      taskId: undefined,
    });
  });

  test("取消时没有历史回到 idle", () => {
    const data = makeData({ status: "running", taskId: "1" });
    expect(planBackfill(data, makeTask({ status: "canceled" }))).toMatchObject({
      status: "idle",
      src: null,
    });
  });

  test("失败不动历史", () => {
    const data = makeData({ status: "running", taskId: "1", outputs: [out("5")] });
    const patch = planBackfill(data, makeTask({ status: "failed", error_message: "x" }));
    expect(patch && "outputs" in patch).toBe(false);
  });

  test("提交前把只存在 src 上的旧结果落进历史", () => {
    const data = makeData({ src: "/files/a.png", assetId: "5", mediaType: "image" });
    const patch = buildSubmittedPatch("image", "9", data);
    expect(patch.outputs?.map((item) => item.id)).toEqual(["5"]);
    expect(patch.src).toBeNull();
  });

  test("序列化带上 outputs 与 activeOutputId", () => {
    const graph = serializeGraph(
      [
        {
          id: "n1",
          type: "canvas",
          position: { x: 0, y: 0 },
          data: makeData({ outputs: [out("1")], activeOutputId: "1" }),
        },
      ],
      [],
      { x: 0, y: 0, zoom: 1 },
    );
    expect(graph.nodes[0].data).toMatchObject({ outputs: [out("1")], activeOutputId: "1" });
  });
});

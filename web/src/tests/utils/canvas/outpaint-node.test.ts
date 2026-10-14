import { describe, expect, test } from "bun:test";

import type { Capabilities, ModelInfo } from "@/api/model/type";
import type { CanvasNode, FlowNode } from "@/types";
import { deserializeGraph, serializeGraph } from "@/utils/canvas/canvas-persistence";
import { ratioParam } from "@/utils/canvas/node-frame";
import {
  OUTPAINT_INSTRUCTION,
  buildOutpaintNode,
  buildOutpaintPrompt,
  outpaintModels,
  pickOutpaintModel,
  resolveRatioParam,
  withoutOutpaintSource,
} from "@/utils/canvas/outpaint-node";

import { makeData } from "../tasks/fixtures";

const source: CanvasNode = {
  id: "src",
  type: "canvas",
  position: { x: 100, y: 200 },
  measured: { width: 576, height: 324 },
  data: makeData({ kind: "image", label: "图片 1", status: "done", src: "/a.png", assetId: "9" }),
};

const args = {
  source,
  nodes: [source] as FlowNode[],
  model: "m1",
  prompt: "把天空补完整",
  ratio: "16:9",
  ratioKey: "aspect_ratio" as string | undefined,
  newId: () => "new1",
  edgeId: () => "edge1",
};

describe("buildOutpaintPrompt 提示词", () => {
  test("用户没写就只有固定指令", () => {
    expect(buildOutpaintPrompt("")).toBe(OUTPAINT_INSTRUCTION);
    expect(buildOutpaintPrompt("   ")).toBe(OUTPAINT_INSTRUCTION);
  });

  test("用户写了就拼在固定指令后面", () => {
    expect(buildOutpaintPrompt(" 加一片海 ")).toBe(`${OUTPAINT_INSTRUCTION}加一片海`);
  });
});

describe("buildOutpaintNode 造新节点和连线", () => {
  test("节点是生成中的图片节点，参数里预置图生图、完整提示词和比例", () => {
    const { node } = buildOutpaintNode(args);
    expect(node.id).toBe("new1");
    expect(node.type).toBe("canvas");
    expect(node.data).toMatchObject({
      kind: "image",
      label: "图片 1-扩图",
      model: "m1",
      status: "running",
      outpaintFrom: "src",
    });
    const full = buildOutpaintPrompt("把天空补完整");
    expect(node.data.prompt).toBe(full);
    expect(node.data.params).toEqual({ prompt: full, op: "i2i", aspect_ratio: "16:9" });
    expect(node.data.taskId).toBeUndefined();
  });

  test("模型没有比例参数时不写比例", () => {
    const { node } = buildOutpaintNode({ ...args, ratioKey: undefined });
    expect(node.data.params).toEqual({ prompt: buildOutpaintPrompt("把天空补完整"), op: "i2i" });
  });

  test("节点记下所选比例作为画幅", () => {
    const { node } = buildOutpaintNode({ ...args, ratio: "9:16" });
    expect(node.data.mediaAspect).toBeCloseTo(9 / 16);
  });

  test("比例是 Auto（读不出来）时沿用原图的画幅", () => {
    const { node } = buildOutpaintNode({ ...args, ratio: "Auto" });
    expect(node.data.mediaAspect).toBeCloseTo(576 / 324);
  });

  test("连一条原图到新节点的普通连线", () => {
    const { edge } = buildOutpaintNode(args);
    expect(edge).toMatchObject({
      id: "edge1",
      source: "src",
      target: "new1",
      sourceHandle: null,
      targetHandle: null,
      type: "animatedSvgEdge",
    });
  });

  test("摆在原图右侧 80px 外，垂直居中", () => {
    const { node } = buildOutpaintNode(args);
    expect(node.position.x).toBe(100 + 576 + 80);
    // 比例 16:9 的新节点框高 324，与原图同高，所以顶边对齐
    expect(node.position.y).toBeCloseTo(200);
  });

  test("竖图比例的新节点框是 324×576，按它的高居中、按原图的右边摆", () => {
    const { node } = buildOutpaintNode({ ...args, ratio: "9:16" });
    expect(node.position.x).toBe(100 + 576 + 80);
    expect(node.position.y).toBeCloseTo(200 + 324 / 2 - 576 / 2);
  });

  test("新节点按自己的框宽判断右侧有没有被占", () => {
    const blocker: CanvasNode = {
      id: "b",
      type: "canvas",
      position: { x: 756 + 324 - 10, y: 200 },
      measured: { width: 576, height: 324 },
      data: makeData({ kind: "image", label: "挡路" }),
    };
    const { node } = buildOutpaintNode({ ...args, ratio: "9:16", nodes: [source, blocker] });
    expect(node.position.y).toBeGreaterThanOrEqual(200 + 324);
  });

  test("右侧被占了就向下错开，不压在别的节点上", () => {
    const blocker: CanvasNode = {
      id: "b",
      type: "canvas",
      position: { x: 756, y: 200 },
      measured: { width: 576, height: 324 },
      data: makeData({ kind: "image", label: "挡路" }),
    };
    const { node } = buildOutpaintNode({ ...args, nodes: [source, blocker] });
    expect(node.position.x).toBe(756);
    expect(node.position.y).toBeGreaterThanOrEqual(200 + 324);
  });

  test("重名时按现有规则编号", () => {
    const taken: CanvasNode = {
      ...source,
      id: "t",
      position: { x: 5000, y: 5000 },
      data: makeData({ kind: "image", label: "图片 1-扩图" }),
    };
    const { node } = buildOutpaintNode({ ...args, nodes: [source, taken] });
    expect(node.data.label).toBe("图片 1-扩图 2");
  });

  test("原标题很长时截断，整体不超过名字上限", () => {
    const long = { ...source, data: { ...source.data, label: "长".repeat(60) } };
    const { node } = buildOutpaintNode({ ...args, source: long, nodes: [long] });
    expect([...node.data.label].length).toBeLessThanOrEqual(40);
    expect(node.data.label.endsWith("-扩图")).toBe(true);
  });
});

describe("withoutOutpaintSource 参考图不含扩图的来源", () => {
  const connections = [
    { edgeId: "e1", source: "src" },
    { edgeId: "e2", source: "other" },
  ];

  test("跳过来自扩图原图的连线，其它照常", () => {
    expect(withoutOutpaintSource(connections, "src")).toEqual([{ edgeId: "e2", source: "other" }]);
  });

  test("不是扩图出来的节点原样返回", () => {
    expect(withoutOutpaintSource(connections, undefined)).toBe(connections);
  });
});

describe("存档", () => {
  test("outpaintFrom 存得住也读得回", () => {
    const { node } = buildOutpaintNode(args);
    const graph = serializeGraph([source, node], [], { x: 0, y: 0, zoom: 1 });
    const saved = graph.nodes.find((item) => item.id === "new1");
    expect(saved?.data).toMatchObject({ outpaintFrom: "src" });
    const loaded = deserializeGraph(graph);
    expect(loaded.nodes.find((item) => item.id === "new1")?.data).toMatchObject({
      outpaintFrom: "src",
    });
  });
});

describe("ratioParam 找模型的比例参数", () => {
  const caps = (params: NonNullable<Capabilities["params"]>): Capabilities => ({
    refs: {
      image: { on: true, max: 4, max_mb: 10 },
      video: { on: false, max: 0, max_mb: 0 },
      audio: { on: false, max: 0, max_mb: 0 },
    },
    prompt: { max_length: 100 },
    params,
  });

  test("认 aspect_ratio 和 ratio 两种名字，返回名字、是否开放和选项", () => {
    const a = ratioParam(
      caps({ aspect_ratio: { type: "enum", label: "比例", open: true, options: ["1:1", "16:9"] } }),
    );
    expect(a).toMatchObject({ name: "aspect_ratio", open: true, options: ["1:1", "16:9"] });
    expect(ratioParam(caps({ ratio: { type: "enum", label: "比例", open: false } }))?.name).toBe(
      "ratio",
    );
  });

  test("没有比例参数返回 undefined", () => {
    expect(
      ratioParam(caps({ quality: { type: "enum", label: "质量", open: true } })),
    ).toBeUndefined();
    expect(ratioParam(undefined)).toBeUndefined();
  });
});

const model = (key: string, ops: Capabilities["ops"], imageOn = true): ModelInfo => ({
  key,
  kind: "image",
  label: key,
  pricing: { billing: "per_call", unit: 1 },
  capabilities: {
    ops,
    refs: {
      image: { on: imageOn, max: 4, max_mb: 10 },
      video: { on: false, max: 0, max_mb: 0 },
      audio: { on: false, max: 0, max_mb: 0 },
    },
    prompt: { max_length: 100 },
  },
});

describe("outpaintModels 能做扩图的模型", () => {
  test("只留支持图生图并且开了图片参考的", () => {
    const models = [
      model("t2i-only", ["t2i"]),
      model("both", ["t2i", "i2i"]),
      model("i2i-no-ref", ["i2i"], false),
      model("i2i", ["i2i"]),
    ];
    expect(outpaintModels(models).map((m) => m.key)).toEqual(["both", "i2i"]);
  });
});

describe("pickOutpaintModel 默认选哪个模型", () => {
  const list = [model("a", ["i2i"]), model("b", ["i2i"])];

  test("原图节点的模型能做扩图就沿用", () => {
    expect(pickOutpaintModel(list, "b")?.key).toBe("b");
  });

  test("不能做或没选过就取第一个", () => {
    expect(pickOutpaintModel(list, "zzz")?.key).toBe("a");
    expect(pickOutpaintModel(list, undefined)?.key).toBe("a");
  });

  test("一个都没有返回 undefined", () => {
    expect(pickOutpaintModel([], "a")).toBeUndefined();
  });
});

describe("resolveRatioParam 比例写进哪个参数", () => {
  const withRatio = (options?: string[], open = true): Capabilities => ({
    refs: {
      image: { on: true, max: 4, max_mb: 10 },
      video: { on: false, max: 0, max_mb: 0 },
      audio: { on: false, max: 0, max_mb: 0 },
    },
    prompt: { max_length: 100 },
    params: { aspect_ratio: { type: "enum", label: "比例", open, options } },
  });

  test("参数是枚举时取最接近的选项", () => {
    expect(resolveRatioParam(withRatio(["1:1", "16:9", "21:9"]), "7:3")).toEqual({
      key: "aspect_ratio",
      value: "21:9",
    });
  });

  test("没有选项限制就原样写", () => {
    expect(resolveRatioParam(withRatio(undefined), "11:8")).toEqual({
      key: "aspect_ratio",
      value: "11:8",
    });
  });

  test("模型没有比例参数时不写", () => {
    const caps = { ...withRatio(), params: {} };
    expect(resolveRatioParam(caps, "16:9")).toEqual({ key: undefined, value: "16:9" });
  });
});

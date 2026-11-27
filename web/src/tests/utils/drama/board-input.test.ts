import { describe, expect, test } from "bun:test";

import type { Capabilities, ModelInfo } from "@/api/model/type";
import { boardNodeSeed, pickBoardModel, planBoardInput } from "@/utils/drama/board-input";
import type { RefAsset } from "@/utils/drama/segment-assets";

/** 图片模型：文生图 + 图生图，参考图 ≤ max，有比例参数 */
const image = (over: { max?: number } = {}): Capabilities => ({
  ops: ["t2i", "i2i"],
  refs: {
    image: { on: true, max: over.max ?? 4, max_mb: 10 },
    audio: { on: false, max: 0, max_mb: 0 },
    video: { on: false, max: 0, max_mb: 0 },
  },
  prompt: { max_length: 4000 },
  params: {
    aspect_ratio: {
      type: "enum",
      label: "比例",
      open: true,
      default: "1:1",
      options: ["1:1", "9:16", "16:9"],
    },
  },
});

const model = (caps: Capabilities = image()): ModelInfo => ({
  key: "img-1",
  kind: "image",
  label: "图片模型",
  pricing: {} as ModelInfo["pricing"],
  capabilities: caps,
});

const PROMPT = `【场景与光】@[周家厨房](n_kitchen)，午后。
【参考与人物】@[闰土](n_runtu) 外貌；@[已删的道具](n_gone)。
【时间轴】
0-3秒：全景。闰土靠着灶台。
3-6秒：中景。闰土抬头。`;

/** 故事板提示词模板（项目设置里选的那份） */
const TEMPLATE =
  "生成一张 {{格数}} 格分镜图：{{列数}} 列 {{行数}} 行，整图比例 {{比例}}。\n{{画面}}";

describe("boardNodeSeed：故事板节点的初始数据", () => {
  test("内置提示词、16:9、模型、drama.board 标记，以及要连线的资产节点（只连画布上还在的）", () => {
    const seed = boardNodeSeed({
      segment: { id: "s1", seq: 0, prompt: PROMPT },
      episodeNo: 3,
      hash: "h1",
      model: model(),
      template: TEMPLATE,
      nodeIds: new Set(["n_kitchen", "n_runtu"]),
    });
    expect(seed.label).toBe("故事板 · 第 3 集 · 01");
    expect(seed.data.prompt).toContain("4 格分镜图");
    expect(seed.data.prompt).toContain("整图比例 16:9");
    expect(seed.data.params).toEqual({ prompt: seed.data.prompt, aspect_ratio: "16:9" });
    expect(seed.data.model).toBe("img-1");
    expect(seed.data.drama).toEqual({
      episodes: [3],
      board: { segmentId: "s1", sourceHash: "h1", layout: "2x2", shotCount: 2 },
    });
    // 没有资产类型：不进资产列表，也不能被片段 @ 引用
    expect(seed.data.drama.type).toBeUndefined();
    expect(seed.refs).toEqual(["n_kitchen", "n_runtu"]);
  });

  test("提示词按传入的模板拼：自定义模板里的文字和占位符都生效", () => {
    const seed = boardNodeSeed({
      segment: { id: "s1", seq: 0, prompt: PROMPT },
      episodeNo: 1,
      hash: "h",
      model: model(),
      template: "水彩 {{格数}} 格\n{{画面}}",
      nodeIds: new Set(),
    });
    expect(seed.data.prompt.startsWith("水彩 4 格\n")).toBe(true);
    expect(seed.data.params).toMatchObject({ prompt: seed.data.prompt });
  });

  test("参考图有上限：只连前几个；没有模型时不带模型和比例，也不连线", () => {
    const one = boardNodeSeed({
      segment: { id: "s1", seq: 0, prompt: PROMPT },
      episodeNo: 1,
      hash: "h",
      model: model(image({ max: 1 })),
      template: TEMPLATE,
      nodeIds: new Set(["n_kitchen", "n_runtu"]),
    });
    expect(one.refs).toEqual(["n_kitchen"]);
    const none = boardNodeSeed({
      segment: { id: "s1", seq: 0, prompt: PROMPT },
      episodeNo: 1,
      hash: "h",
      model: undefined,
      template: TEMPLATE,
      nodeIds: new Set(["n_kitchen"]),
    });
    expect(none.data.model).toBeUndefined();
    expect(none.data.params).toEqual({ prompt: none.data.prompt });
    expect(none.refs).toEqual([]);
  });
});

describe("pickBoardModel：故事板用哪个图片模型", () => {
  const m = (key: string): ModelInfo => ({ ...model(), key, label: key });
  const models = [m("a"), m("b"), m("c")];

  test("本机选过且仍可用 → 建剧时的场景模型 → 第一个", () => {
    expect(pickBoardModel(models, "c", "b")?.key).toBe("c");
    expect(pickBoardModel(models, "", "b")?.key).toBe("b");
    expect(pickBoardModel(models, "gone", "gone")?.key).toBe("a");
    expect(pickBoardModel([], "a", "b")).toBeUndefined();
  });
});

describe("planBoardInput：片场生成故事板的请求", () => {
  const asset = (id: string, name: string, over: Partial<RefAsset> = {}): RefAsset => ({
    id,
    type: "scene",
    name,
    look: "",
    label: name,
    episodes: [],
    kind: "image",
    src: `https://cdn/${id}.png`,
    mediaType: "image",
    assetId: id === "n_kitchen" ? "5" : "6",
    generating: false,
    ...over,
  });
  const assets = [asset("n_kitchen", "周家厨房"), asset("n_runtu", "闰土")];
  const segment = { id: "s1", seq: 0, prompt: PROMPT };

  test("提示词按模板拼；@ 的资产带成参考图（画布上没有的不带），提示词里的 @ 换成图片编号；节点内容一并交给后端", () => {
    const plan = planBoardInput({
      segment,
      episodeNo: 2,
      hash: "h1",
      model: model(),
      template: TEMPLATE,
      assets,
    });
    expect(plan.error).toBeNull();
    const body = plan.body!;
    expect(body).toMatchObject({
      model_id: "img-1",
      label: "故事板 · 第 2 集 · 01",
      source_hash: "h1",
      layout: "2x2",
      shot_count: 2,
      refs: ["n_kitchen", "n_runtu"],
    });
    expect(body.prompt).toContain("@[周家厨房](n_kitchen)");
    expect(body.params).toMatchObject({ aspect_ratio: "16:9" });
    expect(body.input.images).toEqual([5, 6]);
    expect(String(body.input.prompt)).not.toContain("@[");
    expect(String(body.input.prompt)).toContain("图片1");
  });

  test("角色卡带进参考图时，提示词里的人物也写成图片编号，和发出去的参考图一一对上", () => {
    const role = (id: string, name: string, assetId: string): RefAsset =>
      asset(id, name, {
        type: "character",
        kind: "character",
        assetId,
        character: { name, look: "", description: "", extras: [], voice: null },
      });
    const plan = planBoardInput({
      segment: {
        id: "s1",
        seq: 0,
        prompt: [
          "【参考与人物】@[班婉居所](n_room)：门窗斜对案席；@[班婉](n_ban)：素白深衣；@[冯媛](n_feng)：浅黛青深衣；@[长信宫灯](n_lamp)：铜罩合拢。",
          "【时间轴】",
          "0-4秒：中全景。@[班婉](n_ban)在案前执卷，@[冯媛](n_feng)走到席边。",
        ].join("\n"),
      },
      episodeNo: 3,
      hash: "h",
      model: model(),
      template: TEMPLATE,
      assets: [
        asset("n_room", "班婉居所", { assetId: "377" }),
        role("n_ban", "班婉", "371"),
        role("n_feng", "冯媛", "373"),
        asset("n_lamp", "长信宫灯", { type: "prop", assetId: "376" }),
      ],
    });
    expect(plan.error).toBeNull();
    const input = plan.body!.input;
    // 普通资产在前、角色卡在后（参考图的既有排列规则）
    expect(input.images).toEqual([377, 376, 371, 373]);
    const prompt = String(input.prompt);
    expect(prompt).toContain(
      "人物与道具：图片1：门窗斜对案席；班婉（图片3中的人物）：素白深衣；冯媛（图片4中的人物）：浅黛青深衣；图片2：铜罩合拢。",
    );
    expect(prompt).toContain(
      "1. 中全景。班婉（图片3中的人物）在案前执卷，冯媛（图片4中的人物）走到席边。",
    );
    expect(prompt.endsWith("【人物参考】\n班婉：图片3中的人物。\n冯媛：图片4中的人物。")).toBe(
      true,
    );
  });

  test("参考图有上限：只带前几个", () => {
    const plan = planBoardInput({
      segment,
      episodeNo: 1,
      hash: "h",
      model: model(image({ max: 1 })),
      template: TEMPLATE,
      assets,
    });
    expect(plan.body?.refs).toEqual(["n_kitchen"]);
    expect(plan.body?.input.images).toEqual([5]);
  });

  test("没有模型、原稿是空的：不能提交并写原因", () => {
    expect(
      planBoardInput({
        segment,
        episodeNo: 1,
        hash: "h",
        model: undefined,
        template: TEMPLATE,
        assets,
      }),
    ).toMatchObject({ body: null, error: "没有可用的图片模型" });
    expect(
      planBoardInput({
        segment: { ...segment, prompt: "  " },
        episodeNo: 1,
        hash: "h",
        model: model(),
        template: TEMPLATE,
        assets,
      }),
    ).toMatchObject({ body: null, error: "片段原稿是空的" });
  });
});

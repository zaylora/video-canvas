import { describe, expect, test } from "bun:test";

import type { Capabilities } from "@/api/model/type";
import { defaultCapabilities } from "@/utils/admin/model-template";
import {
  buildTaskInput,
  computeHandleFixes,
  currentOp,
  inputPorts,
  paramSummary,
  readParams,
  refKindsOf,
  resolveBindings,
  switchModelParams,
  type IncomingLink,
} from "@/utils/tasks/capabilities";

const video = (): Capabilities => ({
  ops: ["t2v", "i2v", "omni"],
  refs: {
    image: { on: true, max: 2, max_mb: 10 },
    audio: { on: true, max: 1, max_mb: 15 },
    video: { on: false, max: 0, max_mb: 0 },
  },
  prompt: { max_length: 10 },
  params: {
    aspect_ratio: {
      type: "enum",
      label: "比例",
      open: true,
      options: ["Auto", "16:9"],
      default: "Auto",
    },
    duration: {
      type: "number",
      label: "时长",
      open: true,
      min: 4,
      max: 12,
      default: 5,
      unit: "秒",
    },
    generate_audio: { type: "boolean", label: "生成音频", open: true, default: true },
    quality: { type: "enum", label: "质量", open: false, options: [1, 2], default: 2 },
  },
});

const link = (patch: Partial<IncomingLink>): IncomingLink => ({
  edgeId: "e1",
  sourceId: "s1",
  sourceKind: "image",
  sourceLabel: "图片",
  targetHandle: null,
  ...patch,
});

describe("生成方式与输入口", () => {
  test("currentOp：节点选过且仍支持就用它，否则取第一种；文本没有生成方式", () => {
    expect(currentOp(video(), {})).toBe("t2v");
    expect(currentOp(video(), { op: "omni" })).toBe("omni");
    expect(currentOp(video(), { op: "t2i" })).toBe("t2v");
    expect(currentOp(defaultCapabilities("text"), { op: "t2v" })).toBeUndefined();
  });

  test("refKindsOf：文生不引用，图生只引用图片，全能参考按 refs 开关", () => {
    expect(refKindsOf(video(), "t2v")).toEqual([]);
    expect(refKindsOf(video(), "i2v")).toEqual(["image"]);
    expect(refKindsOf(video(), "omni")).toEqual(["image", "audio"]); // 视频素材关闭
  });

  test("inputPorts：提示词口 + 当前方式能接收的素材口", () => {
    expect(inputPorts(video(), "t2v").map((p) => p.id)).toEqual(["prompt"]);
    expect(inputPorts(video(), "omni").map((p) => p.id)).toEqual(["prompt", "images", "audios"]);
  });
});

describe("resolveBindings / computeHandleFixes", () => {
  test("素材口可接多根线，提示词口取第一根；当前方式不接收的线不绑定", () => {
    const links = [
      link({ edgeId: "t", sourceKind: "script", sourceLabel: "文本", text: "猫" }),
      link({ edgeId: "a", assetId: "1" }),
      link({ edgeId: "b", assetId: "2" }),
      link({ edgeId: "v", sourceKind: "video", assetId: "3" }),
    ];
    const bound = resolveBindings(video(), "omni", links);
    expect(bound.prompt?.edgeId).toBe("t");
    expect(bound.images.map((l) => l.edgeId)).toEqual(["a", "b"]);
    expect(bound.videos).toEqual([]); // 视频素材关闭
    const none = resolveBindings(video(), "t2v", links);
    expect(none.images).toEqual([]);
  });

  test("落在空口或失效口的线被改挂；不接收的线回到默认口", () => {
    const links = [
      link({ edgeId: "a", assetId: "1", targetHandle: null }),
      link({ edgeId: "b", assetId: "2", targetHandle: "images" }),
      link({ edgeId: "c", sourceKind: "video", assetId: "3", targetHandle: "videos" }),
    ];
    expect(computeHandleFixes(video(), "omni", links)).toEqual({ a: "images", c: null });
    expect(computeHandleFixes(undefined, "omni", links)).toEqual({});
  });
});

describe("buildTaskInput", () => {
  test("文生：提示词 + 方式 + 开放的参数（取默认），不提交素材与未开放的参数", () => {
    const { input, errors } = buildTaskInput(video(), { prompt: " 一只猫 " });
    expect(errors).toEqual({});
    expect(input).toEqual({
      prompt: "一只猫",
      op: "t2v",
      aspect_ratio: "Auto",
      duration: 5,
      generate_audio: true,
    });
  });

  test("上游文字优先于手填；上游还没有文字时报错", () => {
    const text = link({ sourceKind: "script", sourceLabel: "文本", text: "来自上游" });
    expect(
      buildTaskInput(video(), { prompt: "手填" }, { ...emptyB(), prompt: text }).input.prompt,
    ).toBe("来自上游");
    const empty = { ...text, text: "" };
    expect(buildTaskInput(video(), {}, { ...emptyB(), prompt: empty }).errors.prompt).toContain(
      "上游",
    );
  });

  test("提示词必填且受字数上限约束", () => {
    expect(buildTaskInput(video(), {}).errors.prompt).toBe("请填写提示词");
    expect(buildTaskInput(video(), { prompt: "一二三四五六七八九十甲" }).errors.prompt).toContain(
      "10",
    );
  });

  test("参数校验：枚举要在可选值里，数字要在范围内且为整数", () => {
    const bad = buildTaskInput(video(), { prompt: "x", aspect_ratio: "1:1", duration: 13 });
    expect(bad.errors.aspect_ratio).toContain("比例");
    expect(bad.errors.duration).toContain("不能大于 12");
    expect(buildTaskInput(video(), { prompt: "x", duration: 5.5 }).errors.duration).toContain(
      "整数",
    );
    expect(buildTaskInput(video(), { prompt: "x", duration: "8" }).input.duration).toBe(8);
  });

  test("图生：必须有图片；连线 + 手动添加合并去重，超上限报错", () => {
    expect(buildTaskInput(video(), { prompt: "x", op: "i2v" }).errors.images).toContain(
      "至少 1 张",
    );
    const a = link({ edgeId: "a", assetId: "1" });
    const b = link({ edgeId: "b", assetId: "2" });
    const ok = buildTaskInput(
      video(),
      { prompt: "x", op: "i2v", images: ["2"] },
      { ...emptyB(), images: [a, b] },
    );
    expect(ok.errors).toEqual({});
    expect(ok.input.images).toEqual([1, 2]); // 手动的 2 与连线的 2 去重
    const over = buildTaskInput(
      video(),
      { prompt: "x", op: "i2v", images: ["3"] },
      { ...emptyB(), images: [a, b] },
    );
    expect(over.errors.images).toContain("最多 2 个");
  });

  test("文生方式忽略已有的素材；上游素材还没生成好时报错", () => {
    expect(buildTaskInput(video(), { prompt: "x", images: ["1"] }).input.images).toBeUndefined();
    const pending = link({ edgeId: "a", assetId: undefined, sourceLabel: "图片 1" });
    expect(
      buildTaskInput(video(), { prompt: "x", op: "i2v" }, { ...emptyB(), images: [pending] }).errors
        .images,
    ).toContain("图片 1");
  });

  test("全能参考：至少一个素材", () => {
    expect(buildTaskInput(video(), { prompt: "x", op: "omni" }).errors.images).toContain(
      "至少 1 个",
    );
    const audio = link({ edgeId: "a", sourceKind: "audio", assetId: "9" });
    const built = buildTaskInput(
      video(),
      { prompt: "x", op: "omni" },
      { ...emptyB(), audios: [audio] },
    );
    expect(built.errors).toEqual({});
    expect(built.input.audios).toEqual([9]);
  });

  test("文本模型没有 op 和素材", () => {
    const { input, errors } = buildTaskInput(defaultCapabilities("text"), { prompt: "你好" });
    expect(errors).toEqual({});
    expect(input).toEqual({ prompt: "你好" });
  });

  test("还没拿到能力（模型清单未加载）时什么也不提交", () => {
    expect(buildTaskInput(undefined, { prompt: "x" })).toEqual({ input: {}, errors: {} });
  });
});

describe("switchModelParams", () => {
  test("保留提示词与同名同类型且仍成立的参数，丢弃其余", () => {
    const next = video();
    next.params!.duration.max = 6;
    const switched = switchModelParams(video(), next, {
      prompt: "猫",
      op: "omni",
      aspect_ratio: "16:9",
      duration: 10,
      images: ["1"],
      stale: "x",
    });
    expect(switched.params).toEqual({
      prompt: "猫",
      op: "omni",
      aspect_ratio: "16:9",
      images: ["1"],
    });
    expect(switched.droppedNames.sort()).toEqual(["duration", "stale"]);
  });

  test("新模型不再支持当前生成方式时回落到第一种，该方式下不能用的手动素材一并丢弃", () => {
    const next = video();
    next.ops = ["t2v", "i2v"]; // 去掉 omni
    const switched = switchModelParams(video(), next, { prompt: "猫", op: "omni", images: ["1"] });
    expect(switched.params).toEqual({ prompt: "猫" });
    expect(switched.droppedNames).toEqual(["images"]);
  });

  test("新模型不再接收手动素材时丢弃并报出", () => {
    const text = defaultCapabilities("text");
    const switched = switchModelParams(video(), text, { prompt: "x", op: "i2v", images: ["1"] });
    expect(switched.params).toEqual({ prompt: "x" });
    expect(switched.droppedLabels).toEqual(["参考图片"]);
  });
});

describe("其它", () => {
  test("readParams：旧节点的 prompt 兼容", () => {
    expect(readParams({ prompt: "旧" })).toEqual({ prompt: "旧" });
    expect(readParams({ prompt: "旧", params: { prompt: "新" } })).toEqual({ prompt: "新" });
  });

  test("paramSummary：开放参数的当前取值", () => {
    expect(paramSummary(video(), {})).toBe("Auto · 5秒 · 生成音频开");
    expect(
      paramSummary(video(), { aspect_ratio: "16:9", duration: 8, generate_audio: false }),
    ).toBe("16:9 · 8秒 · 生成音频关");
  });
});

function emptyB() {
  return { images: [], videos: [], audios: [] } as {
    prompt?: IncomingLink;
    images: IncomingLink[];
    videos: IncomingLink[];
    audios: IncomingLink[];
  };
}

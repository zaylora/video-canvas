import { describe, expect, test } from "bun:test";

import type { InputSchema } from "@/api/model/type";

import {
  buildTaskInput,
  computeHandleFixes,
  readParams,
  resolveBindings,
  schemaFields,
  switchModelParams,
  type IncomingLink,
} from "@/utils/tasks/input-schema";

const SCHEMA: InputSchema = {
  prompt: { type: "text", label: "提示词", required: true, max_length: 10, port: "text" },
  image: { type: "image", label: "首帧", required: true, port: "image" },
  duration: {
    type: "enum",
    label: "时长",
    options: [
      { value: 5, label: "5 秒" },
      { value: 10, label: "10 秒" },
    ],
    default: 5,
  },
  seed: { type: "number", label: "种子", min: 0, max: 100, advanced: true },
  hd: { type: "boolean", label: "高清", advanced: true },
};

const link = (over: Partial<IncomingLink> = {}): IncomingLink => ({
  edgeId: "e1",
  sourceId: "n1",
  sourceKind: "image",
  sourceLabel: "图片",
  targetHandle: null,
  assetId: "123",
  ...over,
});

describe("schemaFields", () => {
  test("按 schema 键顺序渲染", () => {
    expect(schemaFields(SCHEMA).map((field) => field.name)).toEqual([
      "prompt",
      "image",
      "duration",
      "seed",
      "hd",
    ]);
  });
});

describe("buildTaskInput：组装 input 并校验", () => {
  test("必填项没填 -> 标红原因，不能提交", () => {
    const { input, errors } = buildTaskInput(SCHEMA, {});
    expect(errors.prompt).toBe("请填写提示词");
    expect(errors.image).toBe("请上传或选择首帧");
    expect(input.duration).toBe(5); // 默认值
  });

  test("媒体字段的值是数字 assetId，不是 URL", () => {
    const { input, errors } = buildTaskInput(SCHEMA, { prompt: "海浪", image: "456" });
    expect(errors).toEqual({});
    expect(input).toEqual({ prompt: "海浪", image: 456, duration: 5 });
  });

  test("连线优先于手填，且用上游素材 id", () => {
    const bindings = { image: link({ assetId: "789" }) };
    const { input, errors } = buildTaskInput(SCHEMA, { prompt: "x", image: "456" }, bindings);
    expect(errors).toEqual({});
    expect(input.image).toBe(789);
  });

  test("上游还没有素材：报错并点名上游节点", () => {
    const bindings = { image: link({ assetId: undefined, sourceLabel: "图片（a.png）" }) };
    const { errors } = buildTaskInput(SCHEMA, { prompt: "x" }, bindings);
    expect(errors.image).toContain("图片（a.png）");
  });

  test("文本口连线：用上游文字覆盖手填；上游没文字且必填时报错", () => {
    const fromText = link({ sourceKind: "script", text: " 上游提示 ", sourceLabel: "文本" });
    expect(buildTaskInput(SCHEMA, { prompt: "手填", image: 1 }, { prompt: fromText }).input.prompt).toBe(
      "上游提示",
    );
    const empty = link({ sourceKind: "script", text: "  ", sourceLabel: "文本" });
    expect(buildTaskInput(SCHEMA, { image: 1 }, { prompt: empty }).errors.prompt).toContain("文本");
    // 上游没文字但手填了：退回手填
    expect(buildTaskInput(SCHEMA, { prompt: "手填", image: 1 }, { prompt: empty }).input.prompt).toBe("手填");
  });

  test("文本长度、数字范围、枚举合法性", () => {
    const tooLong = buildTaskInput(SCHEMA, { prompt: "一二三四五六七八九十十一", image: 1 });
    expect(tooLong.errors.prompt).toContain("10");
    const outOfRange = buildTaskInput(SCHEMA, { prompt: "x", image: 1, seed: 101 });
    expect(outOfRange.errors.seed).toContain("100");
    const badEnum = buildTaskInput(SCHEMA, { prompt: "x", image: 1, duration: 7 });
    expect(badEnum.errors.duration).toBeDefined();
    const okEnum = buildTaskInput(SCHEMA, { prompt: "x", image: 1, duration: "10" });
    expect(okEnum.input.duration).toBe(10); // 枚举值还原成 schema 里的原始类型
  });

  test("可选项不填就不进 input；布尔值按开关提交", () => {
    const { input } = buildTaskInput(SCHEMA, { prompt: "x", image: 1, hd: true });
    expect("seed" in input).toBe(false);
    expect(input.hd).toBe(true);
  });
});

describe("resolveBindings / computeHandleFixes：上游连线绑定输入口", () => {
  const PORTS: InputSchema = {
    prompt: { type: "text", label: "提示词", port: "text" },
    first: { type: "image", label: "首帧", port: "image" },
    last: { type: "image", label: "尾帧", port: "image" },
  };

  test("具名口优先；未指定口的线按种类落到第一个空的同类口", () => {
    const a = link({ edgeId: "a", targetHandle: "last", assetId: "1" });
    const b = link({ edgeId: "b", targetHandle: null, assetId: "2" });
    const t = link({ edgeId: "t", sourceKind: "script", targetHandle: null, text: "hi" });
    const bound = resolveBindings(PORTS, [b, a, t]);
    expect(bound.last).toBe(a);
    expect(bound.first).toBe(b);
    expect(bound.prompt).toBe(t);
  });

  test("多余的线（口已被占）不绑定", () => {
    const a = link({ edgeId: "a", targetHandle: "first" });
    const b = link({ edgeId: "b", targetHandle: "first", assetId: "9" });
    const c = link({ edgeId: "c", targetHandle: "first", assetId: "8" });
    const bound = resolveBindings(PORTS, [a, b, c]);
    expect(bound.first).toBe(a);
    expect(bound.last).toBe(b); // 第二根线落到还空着的同类型口
    expect(Object.values(bound)).not.toContain(c);
  });

  test("类型对不上的线不绑定", () => {
    const audio = link({ edgeId: "x", sourceKind: "audio" });
    expect(resolveBindings(PORTS, [audio])).toEqual({});
  });

  test("换模型后失效的口、空口的线被修正到实际绑定的口", () => {
    const stale = link({ edgeId: "s", targetHandle: "old_port" });
    const empty = link({ edgeId: "e", targetHandle: null, sourceKind: "script", text: "x" });
    const fixes = computeHandleFixes(PORTS, [stale, empty]);
    expect(fixes).toEqual({ s: "first", e: "prompt" });
  });

  test("已经对齐的线不需要修正；没有输入口的模型不动线", () => {
    const ok = link({ edgeId: "ok", targetHandle: "first" });
    expect(computeHandleFixes(PORTS, [ok])).toEqual({});
    expect(computeHandleFixes({ seed: { type: "number", label: "种子" } }, [ok])).toEqual({});
    expect(computeHandleFixes(undefined, [ok])).toEqual({});
  });
});

describe("switchModelParams：切换模型保留同名同类型参数", () => {
  const NEXT: InputSchema = {
    prompt: { type: "text", label: "提示词" },
    image: { type: "video", label: "参考视频" }, // 同名不同类型
    duration: {
      type: "enum",
      label: "时长",
      options: [{ value: 5, label: "5 秒" }], // 10 不再合法
    },
    seed: { type: "number", label: "种子", min: 0, max: 200 },
  };

  test("同名同类型且取值仍合法的保留，其余丢弃并报出用户填过的", () => {
    const result = switchModelParams(SCHEMA, NEXT, {
      prompt: "海浪",
      image: "77",
      duration: 10,
      seed: 150,
      hd: true,
    });
    expect(result.params).toEqual({ prompt: "海浪", seed: 150 });
    expect(result.droppedNames.sort()).toEqual(["duration", "hd", "image"]);
    expect(result.droppedLabels).toContain("首帧");
  });

  test("空值静默丢弃，不打扰用户", () => {
    const result = switchModelParams(SCHEMA, NEXT, { prompt: "x", image: "", seed: undefined });
    expect(result.params).toEqual({ prompt: "x" });
    expect(result.droppedNames).toEqual([]);
  });

  test("旧模型下线（没有旧 schema）时按新 schema 尽量保留", () => {
    const result = switchModelParams(undefined, NEXT, { prompt: "x", other: 1 });
    expect(result.params).toEqual({ prompt: "x" });
    expect(result.droppedNames).toEqual(["other"]);
  });
});

describe("readParams：提示词兼容旧字段", () => {
  test("只有旧的 prompt 时映射到 params.prompt；params 里有则以 params 为准", () => {
    expect(readParams({ prompt: "旧" })).toEqual({ prompt: "旧" });
    expect(readParams({ prompt: "旧", params: { prompt: "新", seed: 1 } })).toEqual({
      prompt: "新",
      seed: 1,
    });
    expect(readParams({})).toEqual({});
  });
});

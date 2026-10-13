import { describe, expect, test } from "bun:test";

import type { Capabilities } from "@/api/model/type";
import {
  AUDIO_NODE_ASPECT,
  DEFAULT_NODE_ASPECT,
  nodeFrameSize,
  paramAspect,
  parseAspect,
  resolveNodeAspect,
} from "@/utils/canvas/node-frame";

const capsWith = (params: NonNullable<Capabilities["params"]>): Capabilities => ({
  refs: { image: { max: 0 }, video: { max: 0 }, audio: { max: 0 } },
  prompt: { max_length: 100 },
  params,
});

const ratioField = (name: string, options: string[], fallback?: string) =>
  capsWith({
    [name]: { type: "enum", label: "比例", open: true, options, default: fallback },
  });

describe("parseAspect：把比例参数值读成宽高比", () => {
  test("W:H 写法换算成宽 / 高", () => {
    expect(parseAspect("16:9")).toBeCloseTo(16 / 9);
    expect(parseAspect("9:16")).toBeCloseTo(9 / 16);
    expect(parseAspect("1:1")).toBe(1);
  });

  test("Auto、空值和不认识的写法返回 null，交给调用方兜底", () => {
    for (const value of ["Auto", "auto", "", undefined, null, "abc", "0:9", "16:0", 1.5]) {
      expect(parseAspect(value)).toBeNull();
    }
  });
});

describe("nodeFrameSize：所有比例的短边都是 324", () => {
  test("16:9 保持 576×324", () => {
    expect(nodeFrameSize(16 / 9)).toEqual({ width: 576, height: 324 });
  });

  test("1:1 保持 324×324", () => {
    expect(nodeFrameSize(1)).toEqual({ width: 324, height: 324 });
  });

  test("横图高度固定 324，宽度按比例变宽", () => {
    expect(nodeFrameSize(4 / 3)).toEqual({ width: 432, height: 324 });
    expect(nodeFrameSize(21 / 9)).toEqual({ width: 756, height: 324 });
  });

  test("竖图宽度固定 324，高度按比例变高", () => {
    expect(nodeFrameSize(3 / 4)).toEqual({ width: 324, height: 432 });
    expect(nodeFrameSize(9 / 16)).toEqual({ width: 324, height: 576 });
  });

  test("极端比例长边不封顶，短边仍是 324", () => {
    expect(nodeFrameSize(1 / 8)).toEqual({ width: 324, height: 2592 });
    expect(nodeFrameSize(8)).toEqual({ width: 2592, height: 324 });
  });

  test("宽高比无效时按默认 16:9 画", () => {
    for (const bad of [0, -1, Number.NaN, Number.POSITIVE_INFINITY]) {
      expect(nodeFrameSize(bad)).toEqual(nodeFrameSize(DEFAULT_NODE_ASPECT));
    }
  });

  test("宽度 / 高度的比与目标比例一致，框不会和内容对不上", () => {
    for (const aspect of [9 / 16, 3 / 4, 1, 4 / 3, 16 / 9, 21 / 9]) {
      const { width, height } = nodeFrameSize(aspect);
      expect(Math.min(width, height)).toBe(324);
      expect(width / height).toBeCloseTo(aspect, 1);
    }
  });
});

describe("paramAspect：从模型参数里读用户选的比例", () => {
  test("读 aspect_ratio 参数的当前值", () => {
    const caps = ratioField("aspect_ratio", ["16:9", "9:16"], "16:9");
    expect(paramAspect(caps, { aspect_ratio: "9:16" })).toBeCloseTo(9 / 16);
  });

  test("没选过用参数默认值", () => {
    const caps = ratioField("aspect_ratio", ["16:9", "9:16"], "9:16");
    expect(paramAspect(caps, {})).toBeCloseTo(9 / 16);
  });

  test("有的模型把参数叫 ratio，也认", () => {
    const caps = ratioField("ratio", ["1:1", "4:3"]);
    expect(paramAspect(caps, { ratio: "4:3" })).toBeCloseTo(4 / 3);
  });

  test("选了 Auto、没有比例参数或没有能力描述时返回 null", () => {
    expect(
      paramAspect(ratioField("aspect_ratio", ["Auto", "16:9"]), { aspect_ratio: "Auto" }),
    ).toBeNull();
    expect(paramAspect(capsWith({}), {})).toBeNull();
    expect(paramAspect(undefined, {})).toBeNull();
  });
});

describe("resolveNodeAspect：成片 > 所选比例 > 16:9", () => {
  test("成片量到了比例，以成片为准，不管选的是什么", () => {
    expect(resolveNodeAspect({ measured: 3 / 4, selected: 16 / 9 })).toBeCloseTo(3 / 4);
  });

  test("还没量到，用所选比例", () => {
    expect(resolveNodeAspect({ measured: null, selected: 9 / 16 })).toBeCloseTo(9 / 16);
  });

  test("都没有，用默认 16:9", () => {
    expect(resolveNodeAspect({ measured: null, selected: null })).toBe(DEFAULT_NODE_ASPECT);
  });
});

describe("音频节点：固定 1:1", () => {
  test("音频不管量到什么、选了什么，画幅都是 1:1", () => {
    expect(AUDIO_NODE_ASPECT).toBe(1);
    expect(resolveNodeAspect({ measured: null, selected: null, mediaType: "audio" })).toBe(1);
    expect(resolveNodeAspect({ measured: 16 / 9, selected: 9 / 16, mediaType: "audio" })).toBe(1);
    expect(nodeFrameSize(AUDIO_NODE_ASPECT)).toEqual({ width: 324, height: 324 });
  });

  test("图片和视频照旧：成片 > 所选比例 > 16:9", () => {
    expect(resolveNodeAspect({ measured: null, selected: null, mediaType: "video" })).toBe(
      DEFAULT_NODE_ASPECT,
    );
    expect(resolveNodeAspect({ measured: 3 / 4, selected: null, mediaType: "image" })).toBeCloseTo(
      3 / 4,
    );
  });
});

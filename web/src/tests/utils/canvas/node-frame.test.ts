import { describe, expect, test } from "bun:test";

import type { Capabilities } from "@/api/model/type";
import {
  DEFAULT_NODE_ASPECT,
  NODE_FRAME_MAX,
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

describe("nodeFrameSize：框限定在 576×324 内", () => {
  test("16:9 就是整个包围盒", () => {
    expect(nodeFrameSize(16 / 9)).toEqual({ width: NODE_FRAME_MAX.width, height: 324 });
  });

  test("比 16:9 更宽的横图保持 576 宽，高度变矮", () => {
    const size = nodeFrameSize(21 / 9);
    expect(size.width).toBe(576);
    expect(size.height).toBeCloseTo(576 / (21 / 9), 0);
  });

  test("竖图保持 324 高，宽度按比例收窄", () => {
    const size = nodeFrameSize(9 / 16);
    expect(size.height).toBe(324);
    expect(size.width).toBeCloseTo(324 * (9 / 16), 0);
  });

  test("1:1 取高度 324 的正方形", () => {
    expect(nodeFrameSize(1)).toEqual({ width: 324, height: 324 });
  });

  test("极端比例收在上下限内，框不会窄到放不下按钮和文字", () => {
    expect(nodeFrameSize(1 / 10).width).toBeGreaterThanOrEqual(160);
    expect(nodeFrameSize(10).height).toBeGreaterThanOrEqual(120);
  });

  test("宽高比无效时按默认 16:9 画", () => {
    for (const bad of [0, -1, Number.NaN, Number.POSITIVE_INFINITY]) {
      expect(nodeFrameSize(bad)).toEqual(nodeFrameSize(DEFAULT_NODE_ASPECT));
    }
  });

  test("宽度 / 高度的比与目标比例一致，框不会和内容对不上", () => {
    for (const aspect of [9 / 16, 3 / 4, 1, 4 / 3, 16 / 9, 21 / 9]) {
      const { width, height } = nodeFrameSize(aspect);
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

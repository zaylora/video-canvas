import { describe, expect, test } from "bun:test";

import type { Capabilities } from "@/api/model/type";
import {
  NODE_ASPECT_MAX,
  NODE_ASPECT_MIN,
  clampNodeAspect,
  imageNodeAspect,
  parseRatio,
  ratioParamValue,
} from "@/utils/canvas/node-aspect";

const caps = (params: Capabilities["params"]): Capabilities => ({
  refs: {
    image: { on: true, max: 4, max_mb: 10 },
    video: { on: false, max: 0, max_mb: 0 },
    audio: { on: false, max: 0, max_mb: 0 },
  },
  prompt: { max_length: 1000 },
  ops: ["t2i", "i2i"],
  params,
});

const ratioParam = (extra: Record<string, unknown> = {}) =>
  caps({
    aspect_ratio: {
      type: "enum",
      label: "比例",
      open: true,
      options: ["Auto", "16:9", "1:1", "9:16"],
      default: "Auto",
      ...extra,
    } as never,
  });

describe("parseRatio：把比例文字读成宽 / 高", () => {
  test("常见写法：16:9、16/9、16x9、1024×1024，前后有空格也行", () => {
    expect(parseRatio("16:9")).toBeCloseTo(16 / 9);
    expect(parseRatio(" 4 : 3 ")).toBeCloseTo(4 / 3);
    expect(parseRatio("16/9")).toBeCloseTo(16 / 9);
    expect(parseRatio("1024x512")).toBeCloseTo(2);
    expect(parseRatio("1024×1024")).toBe(1);
  });

  test("Auto、空、乱写、零都读不出来", () => {
    for (const value of ["Auto", "auto", "", "abc", "0:9", "16:0", undefined, null, 5]) {
      expect(parseRatio(value)).toBeUndefined();
    }
  });
});

describe("ratioParamValue：从模型参数里找出「比例」当前选的是什么", () => {
  test("找到比例参数，取用户选的值", () => {
    expect(ratioParamValue(ratioParam(), { aspect_ratio: "9:16" })).toBeCloseTo(9 / 16);
  });

  test("没选就取默认值；默认是 Auto 则没有比例", () => {
    expect(ratioParamValue(ratioParam(), {})).toBeUndefined();
    expect(ratioParamValue(ratioParam({ default: "1:1" }), {})).toBe(1);
  });

  test("没开放给用户的比例参数，也按默认值生效（后端照默认值生成）", () => {
    expect(ratioParamValue(ratioParam({ open: false, default: "16:9" }), {})).toBeCloseTo(16 / 9);
  });

  test("参数名不叫比例也认：选项全是 宽:高 的枚举（label 带「比例」「画幅」「尺寸」）", () => {
    const c = caps({
      size: {
        type: "enum",
        label: "画幅",
        open: true,
        options: ["1:1", "3:4"],
        default: "3:4",
      } as never,
    });
    expect(ratioParamValue(c, {})).toBeCloseTo(3 / 4);
  });

  test("不是比例的参数不误认：清晰度、数量、开关", () => {
    const c = caps({
      quality: {
        type: "enum",
        label: "清晰度",
        open: true,
        options: ["720P", "1080P"],
        default: "720P",
      } as never,
      count: { type: "number", label: "数量", open: true, min: 1, max: 4, default: 1 } as never,
    });
    expect(ratioParamValue(c, {})).toBeUndefined();
  });

  test("没有模型能力时没有比例", () => {
    expect(ratioParamValue(undefined, {})).toBeUndefined();
  });
});

describe("clampNodeAspect：极端比例限制在 1:2 到 2:1", () => {
  test("范围内原样，超出收回", () => {
    expect(clampNodeAspect(1.5)).toBe(1.5);
    expect(clampNodeAspect(5)).toBe(NODE_ASPECT_MAX);
    expect(clampNodeAspect(0.1)).toBe(NODE_ASPECT_MIN);
  });
});

describe("imageNodeAspect：图片节点此刻该用什么画幅", () => {
  const params = { aspect_ratio: "1:1" };

  test("出图了：用图片自己的真实比例（已量出来的）", () => {
    expect(imageNodeAspect({ done: true, measured: 2, caps: ratioParam(), params })).toBe(2);
  });

  test("出图了但还没量出来：先用模型选的比例顶着，再不行是 16:9", () => {
    expect(imageNodeAspect({ done: true, caps: ratioParam(), params })).toBe(1);
    expect(imageNodeAspect({ done: true, caps: ratioParam(), params: {} })).toBeCloseTo(16 / 9);
  });

  test("还没出图（空、排队、生成中、失败）：跟着面板选的比例", () => {
    expect(
      imageNodeAspect({
        done: false,
        measured: 2,
        caps: ratioParam(),
        params: { aspect_ratio: "9:16" },
      }),
    ).toBeCloseTo(9 / 16);
  });

  test("还没出图且面板没有比例：用已知的比例（扩图结果节点建出来时就知道），都没有是 16:9", () => {
    expect(imageNodeAspect({ done: false, measured: 1.5, caps: undefined, params: {} })).toBe(1.5);
    expect(imageNodeAspect({ done: false, caps: undefined, params: {} })).toBeCloseTo(16 / 9);
  });

  test("结果统一限制在 1:2 到 2:1", () => {
    expect(imageNodeAspect({ done: true, measured: 4, caps: undefined, params: {} })).toBe(
      NODE_ASPECT_MAX,
    );
    expect(imageNodeAspect({ done: true, measured: 0.2, caps: undefined, params: {} })).toBe(
      NODE_ASPECT_MIN,
    );
  });
});

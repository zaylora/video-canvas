import { describe, expect, test } from "bun:test";

import { defaultCapabilities, defaultPricing } from "@/utils/admin/model-template";
import { staleCondition, tierDimensions } from "@/pages/admin/ai/models/price-editors";

describe("按种类预填的能力与定价", () => {
  for (const kind of ["video", "image", "text", "audio"]) {
    test(`${kind}：规格价格的条件都能对上模板里的规格维度`, () => {
      const dims = tierDimensions(defaultCapabilities(kind));
      for (const tier of defaultPricing(kind).tiers ?? [])
        expect(staleCondition(tier, dims)).toBeNull();
    });
  }

  test("按秒计费的模板有 duration 数字参数；文本按 Token、没有生成方式", () => {
    expect(defaultPricing("video").billing).toBe("per_second");
    expect(defaultCapabilities("video").params?.duration?.type).toBe("number");
    expect(defaultPricing("text").billing).toBe("token");
    expect(defaultCapabilities("text").ops).toBeUndefined();
  });

  test("视频、图片带生成数量（fanout）参数", () => {
    expect(defaultCapabilities("video").params?.count?.fanout).toBe(true);
    expect(defaultCapabilities("image").params?.count?.options).toEqual([1, 2, 4]);
  });
});

describe("staleCondition", () => {
  const dims = tierDimensions(defaultCapabilities("video"));
  test("可选值被取消、维度不存在时给出原因", () => {
    expect(staleCondition({ on: true, when: { resolution: "8K" }, unit: 1 }, dims)).toContain(
      "8K 已不可选",
    );
    expect(staleCondition({ on: true, when: { fps: 30 }, unit: 1 }, dims)).toContain("fps");
    expect(
      staleCondition({ on: true, when: { resolution: "4K", op: "omni" }, unit: 1 }, dims),
    ).toBeNull();
  });
});

import { describe, expect, test } from "bun:test";

import {
  blankModelBody,
  defaultCapabilities,
  defaultDeadline,
  defaultPricing,
} from "@/utils/admin/model-template";
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

  test("agent：只按 Token 计费，有上下文和看图，没有生成方式、素材开关、生成参数、系统提示", () => {
    const caps = defaultCapabilities("agent");
    expect(defaultPricing("agent")).toEqual({ billing: "token", token: { in: 3, out: 15 } });
    expect(caps.context).toEqual({ window: 200000, output: 8192 });
    expect(caps.vision).toBe(true);
    expect(caps.ops).toBeUndefined();
    expect(caps.params).toEqual({});
    expect(caps.system).toBeUndefined();
    expect(Object.values(caps.refs).every((r) => !r.on)).toBe(true);
    expect(defaultCapabilities("text").vision).toBeUndefined();
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

describe("defaultDeadline / blankModelBody", () => {
  test("文本与 Agent 5 分钟，图片 10 分钟，视频与音频 30 分钟", () => {
    expect(defaultDeadline("text")).toBe("5m");
    expect(defaultDeadline("agent")).toBe("5m");
    expect(defaultDeadline("image")).toBe("10m");
    expect(defaultDeadline("video")).toBe("30m");
    expect(defaultDeadline("audio")).toBe("30m");
  });

  test("手动新增的起始正文是“待配置”半成品：没有能力、协议与定价，带预选渠道", () => {
    const body = blankModelBody("main", "gpt-x");
    expect(body).toMatchObject({
      key: "",
      kind: "",
      enabled: false,
      channels: [{ channel: "main", upstream_model: "gpt-x" }],
    });
    expect("capabilities" in body).toBe(false);
    expect("pricing" in body).toBe(false);
    expect(blankModelBody().channels).toEqual([{ channel: "", upstream_model: "" }]);
  });
});

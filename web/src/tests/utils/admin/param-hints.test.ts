import { describe, expect, test } from "bun:test";

import { draftToModelBody } from "@/utils/admin/model-body";
import { defaultCapabilities, defaultPricing } from "@/utils/admin/model-template";
import { applyParamHints, ignoredHints } from "@/utils/admin/param-hints";

const video = () => [defaultCapabilities("video"), defaultPricing("video")] as const;
const image = () => [defaultCapabilities("image"), defaultPricing("image")] as const;

describe("applyParamHints", () => {
  test("枚举：可选值与默认值；默认值不在可选值里时取第一个", () => {
    const [caps, pricing] = image();
    const a = applyParamHints(caps, pricing, {
      resolution: { options: ["2K", "4K"], default: "2K" },
    });
    expect(a.capabilities.params?.resolution).toMatchObject({
      options: ["2K", "4K"],
      default: "2K",
      spec: true,
    });
    const b = applyParamHints(caps, pricing, { resolution: { options: ["2K", "4K"] } }); // 模板默认 1K 已不可选
    expect(b.capabilities.params?.resolution.default).toBe("2K");
    const c = applyParamHints(caps, pricing, { resolution: { options: ["2K"], default: "8K" } });
    expect(c.capabilities.params?.resolution.default).toBe("2K");
  });

  test("数字：最小 / 最大 / 步长 / 默认，默认值落进范围", () => {
    const [caps, pricing] = video();
    const a = applyParamHints(caps, pricing, {
      duration: { min: 4, max: 15, step: 1, default: 5 },
    });
    expect(a.capabilities.params?.duration).toMatchObject({ min: 4, max: 15, step: 1, default: 5 });
    const b = applyParamHints(caps, pricing, { duration: { min: 8, max: 12 } }); // 模板默认 5 小于新的最小值
    expect(b.capabilities.params?.duration.default).toBe(8);
  });

  test("开关默认值、是否开放给用户", () => {
    const [caps, pricing] = video();
    const a = applyParamHints(caps, pricing, {
      generate_audio: { default: false },
      aspect_ratio: { open: false },
    });
    expect(a.capabilities.params?.generate_audio.default).toBe(false);
    expect(a.capabilities.params?.aspect_ratio.open).toBe(false);
  });

  test("删掉参数，保持其余参数的书写顺序，并清掉引用它的规格价格", () => {
    const [caps, pricing] = image();
    const a = applyParamHints(caps, pricing, { resolution: { remove: true } });
    expect(Object.keys(a.capabilities.params ?? {})).toEqual(["aspect_ratio", "count"]);
    expect(a.pricing.tiers).toBeUndefined(); // 图片模板的规格价都按清晰度
    expect(a.pricing.unit).toBe(pricing.unit);
  });

  test("可选值收窄后，引用已不可选档位的规格价格被清掉，其余保留", () => {
    const [caps, pricing] = image(); // 规格价：2K、4K
    const a = applyParamHints(caps, pricing, { resolution: { options: ["1K", "4K"] } });
    expect(a.pricing.tiers?.map((tier) => tier.when)).toEqual([{ resolution: "4K" }]);
  });

  test("类型不对的字段被忽略：enum 上的 min、boolean 上的 options、非法可选值", () => {
    const [caps, pricing] = video();
    const a = applyParamHints(caps, pricing, {
      resolution: { min: 1, options: ["", "720P"] },
      generate_audio: { options: ["x"], default: "yes" },
    });
    expect(a.capabilities.params?.resolution).toMatchObject({ options: ["720P"], default: "720P" });
    expect(a.capabilities.params?.resolution.min).toBeUndefined();
    expect(a.capabilities.params?.generate_audio).toEqual(caps.params?.generate_audio);
  });

  test("模板里没有的参数不新增，列为忽略；没有建议时原样返回", () => {
    const [caps, pricing] = image();
    const a = applyParamHints(caps, pricing, {
      quality: { options: ["hd"] },
      resolution: { remove: true },
    });
    expect(a.ignored).toEqual(["quality"]);
    expect(a.capabilities.params?.quality).toBeUndefined();
    expect(ignoredHints(caps, { quality: {}, count: {} })).toEqual(["quality"]);
    expect(applyParamHints(caps, pricing, null).capabilities).toBe(caps);
  });
});

describe("draftToModelBody 带上预填建议", () => {
  test("天才猴子三代：清晰度只有 2K / 4K，默认 2K", () => {
    const body = draftToModelBody(
      {
        upstream_model: "yswg-monkey-3",
        kind: "image",
        label: "天才猴子三代（Gemini）",
        params: { groups: { "2K": "4", "4K": "6" } },
        param_hints: { resolution: { options: ["2K", "4K"], default: "2K" } },
      },
      "yswg",
    );
    const caps = body.capabilities as ReturnType<typeof defaultCapabilities>;
    expect(caps.params?.resolution).toMatchObject({ options: ["2K", "4K"], default: "2K" });
    expect(body.params).toEqual({ groups: { "2K": "4", "4K": "6" } });
  });
});

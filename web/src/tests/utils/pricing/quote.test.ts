import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { join } from "node:path";

import type { Capabilities, Pricing } from "@/api/model/type";
import { fanoutCount, priceLabel, quote, settle, type Usage } from "@/utils/pricing/quote";

type Vectors = {
  quote: Array<{
    name: string;
    capabilities: Capabilities;
    pricing: Pricing;
    spec: { op: string; ref_video: boolean; params: Record<string, unknown>; prompt_chars: number };
    one: number;
    n: number;
    total: number;
  }>;
  settle: Array<{
    name: string;
    pricing: Pricing;
    frozen: number;
    usage: Usage | null;
    charge: number;
  }>;
};

// 与后端共用的测试向量
const vectors = JSON.parse(
  readFileSync(
    join(import.meta.dir, "../../../../../backend/internal/tests/testdata/pricing_vectors.json"),
    "utf-8",
  ),
) as Vectors;

describe("quote：与后端共用的计价向量", () => {
  for (const c of vectors.quote) {
    test(c.name, () => {
      const spec = {
        op: c.spec.op || undefined,
        refVideo: c.spec.ref_video,
        params: c.spec.params,
        promptChars: c.spec.prompt_chars,
      };
      const one = quote(c.pricing, c.capabilities, spec);
      const n = fanoutCount(c.capabilities, c.spec.params);
      expect([one, n, one * n]).toEqual([c.one, c.n, c.total]);
    });
  }
});

describe("settle：与后端共用的结算向量", () => {
  for (const c of vectors.settle) {
    test(c.name, () => expect(settle(c.pricing, c.frozen, c.usage)).toBe(c.charge));
  }
});

describe("priceLabel", () => {
  test("按次 / 按秒 / Token，有更便宜的规格价时显示「起」", () => {
    expect(priceLabel({ billing: "per_call", unit: 10 })).toBe("10 积分");
    expect(priceLabel({ billing: "per_second", per_second: 2 })).toBe("2 积分/秒");
    expect(priceLabel({ billing: "token", token: { in: 1, out: 1 } })).toBe("按 Token");
    expect(
      priceLabel({ billing: "per_call", unit: 10, tiers: [{ on: true, when: { a: 1 }, unit: 6 }] }),
    ).toBe("6 积分起");
    expect(
      priceLabel({ billing: "per_call", unit: 4, tiers: [{ on: true, when: { a: 1 }, unit: 16 }] }),
    ).toBe("4 积分起");
    // 停用的、没有条件的规格价不算
    expect(
      priceLabel({
        billing: "per_call",
        unit: 4,
        tiers: [
          { on: false, when: { a: 1 }, unit: 1 },
          { on: true, when: {}, unit: 1 },
        ],
      }),
    ).toBe("4 积分");
  });
});

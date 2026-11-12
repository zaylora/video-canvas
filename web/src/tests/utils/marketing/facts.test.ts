import { describe, expect, test } from "bun:test";

import type { ProductFacts } from "@/api/marketing/type";
import { buildFacts, toLines } from "@/utils/marketing/facts";

const base: ProductFacts = {
  name: "晨雾香水 50ml",
  category: "香水",
  anchors: ["方形磨砂玻璃瓶身"],
  sellingPoints: [{ text: "留香 8 小时", proof: "时间跳切", source: "image" }],
  prohibited: ["不编造价格"],
  audience: "通勤女性",
  cta: "点击下方链接",
};

describe("事实卡编辑", () => {
  test("多行文本拆成条目，去掉空行和首尾空白", () => {
    expect(toLines(" 一 \n\n二\n  \n")).toEqual(["一", "二"]);
  });

  test("原有卖点保留 proof / source，新增的标为来自描述；品类和人群原样保留", () => {
    const next = buildFacts(base, {
      name: "晨雾 50ml",
      anchors: "瓶身\n瓶盖",
      selling: "留香 8 小时\n新卖点",
      prohibited: "",
      cta: " 立即下单 ",
    });
    expect(next.name).toBe("晨雾 50ml");
    expect(next.anchors).toEqual(["瓶身", "瓶盖"]);
    expect(next.sellingPoints).toEqual([
      { text: "留香 8 小时", proof: "时间跳切", source: "image" },
      { text: "新卖点", proof: "", source: "description" },
    ]);
    expect(next.prohibited).toEqual([]);
    expect(next.cta).toBe("立即下单");
    expect(next.category).toBe("香水");
    expect(next.audience).toBe("通勤女性");
  });

  test("商品名留空保持原名", () => {
    expect(
      buildFacts(base, { name: "  ", anchors: "", selling: "", prohibited: "", cta: "" }).name,
    ).toBe("晨雾香水 50ml");
  });
});

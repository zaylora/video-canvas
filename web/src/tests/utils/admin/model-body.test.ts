import { describe, expect, test } from "bun:test";

import {
  checkDeadline,
  checkIdleTimeout,
  describeDefaultPrice,
  readModelBool,
  readModelChannel,
  readModelKind,
  readModelNumber,
  readModelString,
  withDefaultPrice,
  withModelChannel,
  withModelField,
  withModelUpstream,
} from "@/utils/admin/model-body";

const body = {
  key: "kling-i2v",
  kind: "video",
  label: "可灵",
  credits: 10,
  channels: [{ channel: "old", upstream_model: "v1" }],
  params: { b: 1, a: 2 },
  capabilities: { ops: [], refs: {}, prompt: {}, params: { b: {}, a: {} } },
};

describe("读取正文字段", () => {
  test("kind 与 channels[0]；缺失返回空串", () => {
    expect(readModelKind(body)).toBe("video");
    expect(readModelKind(null)).toBe("");
    expect(readModelChannel(body)).toEqual({ channel: "old", upstreamModel: "v1" });
    expect(readModelChannel({})).toEqual({ channel: "", upstreamModel: "" });
    expect(readModelChannel({ channels: [null] })).toEqual({ channel: "", upstreamModel: "" });
  });

  test("数字、字符串、布尔字段的读取", () => {
    expect(readModelNumber(body, "credits")).toBe(10);
    expect(readModelNumber(body, "label")).toBeNull();
    expect(readModelNumber({ credits: Number.NaN }, "credits")).toBeNull();
    expect(readModelString(body, "label")).toBe("可灵");
    expect(readModelString(body, "nope")).toBe("");
    expect(readModelBool({ enabled: true }, "enabled")).toBe(true);
    expect(readModelBool({}, "enabled")).toBe(false);
  });
});

describe("withModelChannel", () => {
  test("只改渠道，其余字段与键顺序不动", () => {
    const next = withModelChannel(body, "new");
    expect(next).not.toBeNull();
    expect(Object.keys(next!)).toEqual(Object.keys(body));
    expect(next!.channels).toEqual([{ channel: "new", upstream_model: "v1" }]);
    expect(Object.keys(next!.capabilities as object)).toEqual(["ops", "refs", "prompt", "params"]);
  });

  test("没有 channels 时追加一项，正文不是对象时返回 null", () => {
    const next = withModelChannel({ key: "a" }, "c");
    expect(next).toEqual({ key: "a", channels: [{ channel: "c", upstream_model: "" }] });
    expect(withModelChannel(null, "c")).toBeNull();
    expect(withModelChannel([], "c")).toBeNull();
  });

  test("不修改传入的对象", () => {
    const copy = structuredClone(body);
    withModelChannel(copy, "x");
    expect(copy).toEqual(body);
  });
});

describe("withModelField / withModelUpstream", () => {
  test("已有字段原位替换，键顺序不变；新字段追加到末尾", () => {
    const next = withModelField(body, "credits", 20)!;
    expect(Object.keys(next)).toEqual(Object.keys(body));
    expect(next.credits).toBe(20);
    const added = withModelField(body, "hint", "提示")!;
    expect(Object.keys(added).at(-1)).toBe("hint");
    expect(withModelField("x", "a", 1)).toBeNull();
  });

  test("上游模型只改 upstream_model，渠道保持", () => {
    const next = withModelUpstream(body, "v2")!;
    expect(next.channels).toEqual([{ channel: "old", upstream_model: "v2" }]);
    expect(Object.keys(next)).toEqual(Object.keys(body));
    expect(withModelUpstream({ key: "a" }, "m")!.channels).toEqual([
      { channel: "", upstream_model: "m" },
    ]);
  });

  test("用 JSON 文本来回转一遍，键顺序仍然保持", () => {
    const step = withModelField(withModelChannel(body, "n")!, "label", "改")!;
    const round = JSON.parse(JSON.stringify(step)) as typeof body;
    expect(Object.keys(round)).toEqual(Object.keys(body));
    expect(Object.keys(round.params)).toEqual(["b", "a"]);
  });
});

describe("checkDeadline", () => {
  test("Go 时长写法通过", () => {
    for (const ok of ["30m", "1h", "90s", "1h30m", "1.5h", "500ms"])
      expect(checkDeadline(ok)).toBeNull();
  });

  test("空值与错误格式给出提示", () => {
    expect(checkDeadline("")).toContain("请填写");
    for (const bad of ["30", "m", "30 分钟", "-5m", "1d"])
      expect(checkDeadline(bad)).toContain("格式");
  });
});

describe("checkIdleTimeout", () => {
  test("留空表示用平台默认值，不报错", () => {
    expect(checkIdleTimeout("")).toBeNull();
    expect(checkIdleTimeout("   ")).toBeNull();
  });

  test("Go 时长写法通过", () => {
    for (const ok of ["30s", "90s", "2m", "1m30s", "10m"]) expect(checkIdleTimeout(ok)).toBeNull();
  });

  test("错误格式给出提示，范围 10s – 10m 之外也提示", () => {
    for (const bad of ["30", "m", "两分钟", "-5s"]) expect(checkIdleTimeout(bad)).toContain("格式");
    for (const out of ["5s", "9s", "11m", "1h"]) expect(checkIdleTimeout(out)).toContain("10s");
  });
});

describe("默认价格读写", () => {
  test("按次改 unit、按秒改 per_second，其余定价与键顺序不动；Token 计费不适用", () => {
    const perCall = { key: "a", pricing: { billing: "per_call", unit: 4, tiers: [] } };
    expect(withDefaultPrice(perCall, 9)?.pricing).toEqual({
      billing: "per_call",
      unit: 9,
      tiers: [],
    });
    const perSecond = { pricing: { billing: "per_second", per_second: 2 } };
    expect(withDefaultPrice(perSecond, 3)?.pricing).toEqual({
      billing: "per_second",
      per_second: 3,
    });
    expect(
      withDefaultPrice({ pricing: { billing: "token", token: { in: 1, out: 1 } } }, 3),
    ).toBeNull();
    expect(withDefaultPrice({}, 3)).toBeNull();
  });

  test("describeDefaultPrice", () => {
    expect(describeDefaultPrice({ billing: "per_call", unit: 4 })).toBe("4 积分 / 次");
    expect(describeDefaultPrice({ billing: "per_second", per_second: 2 })).toBe("2 积分 / 秒");
    expect(describeDefaultPrice({ billing: "token", token: { in: 2, out: 8 } })).toContain(
      "输入 2",
    );
    expect(describeDefaultPrice(null)).toBe("-");
  });
});

describe("换渠道不动协议", () => {
  test("协议与渠道无关：withModelChannel 保留 channels[0].protocol", () => {
    const protocol = { plugin: "openai", version: "1.0.0", settings: { api: "responses" } };
    const next = withModelChannel(
      { key: "m", channels: [{ channel: "old", upstream_model: "v1", protocol }] },
      "new",
    );
    expect(next?.channels).toEqual([{ channel: "new", upstream_model: "v1", protocol }]);
  });
});

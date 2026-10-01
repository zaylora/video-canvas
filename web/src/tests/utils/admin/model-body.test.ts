import { describe, expect, test } from "bun:test";

import {
  checkDeadline,
  draftToModelBody,
  normalizeDraft,
  readModelBool,
  readModelChannel,
  readModelKind,
  readModelNumber,
  readModelString,
  suggestModelKey,
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

describe("suggestModelKey", () => {
  test("小写、非字母数字变连字符、去首尾连字符、最长 64", () => {
    expect(suggestModelKey("GPT-4o mini")).toBe("gpt-4o-mini");
    expect(suggestModelKey("  kling/v2.master__x ")).toBe("kling-v2-master-x");
    expect(suggestModelKey("---")).toBe("");
    expect(suggestModelKey("a".repeat(100))).toHaveLength(64);
  });
});

describe("normalizeDraft / draftToModelBody", () => {
  test("同时认 snake_case 与 camelCase；缺上游模型名的丢掉", () => {
    expect(normalizeDraft({ upstreamModel: "m1", kind: "text", inputSchema: { p: {} } })).toEqual({
      upstream_model: "m1",
      kind: "text",
      label: "",
      params: null,
    });
    expect(normalizeDraft({ kind: "text" })).toBeNull();
    expect(normalizeDraft(null)).toBeNull();
  });

  test("草稿 → 新建正文：渠道预填、不上架、文本与其他 kind 的默认时限不同", () => {
    const video = draftToModelBody(
      { upstream_model: "Kling V2", kind: "video", label: "", params: null },
      "ch",
    );
    expect(video).toMatchObject({
      key: "kling-v2",
      kind: "video",
      label: "Kling V2",
      enabled: false,
      deadline: "30m",
      channels: [{ channel: "ch", upstream_model: "Kling V2" }],
      params: {},
    });
    // 能力按种类预填（草稿不带）
    expect(Object.keys((video.capabilities as { params: object }).params)).toEqual([
      "aspect_ratio",
      "resolution",
      "duration",
      "generate_audio",
    ]);
    const text = draftToModelBody(
      { upstream_model: "gpt", kind: "text", label: "GPT", params: { a: 1 } },
      "ch",
    );
    expect(text.deadline).toBe("5m");
    expect(text.params).toEqual({ a: 1 });
    expect(draftToModelBody({ upstream_model: "x", kind: "", label: "" }, "ch").kind).toBe("video");
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

import { describe, expect, test } from "bun:test";
import { createHash } from "node:crypto";

import type { Segment } from "@/api/drama/type";
import type { Capabilities } from "@/api/model/type";
import {
  adaptState,
  formatLabel,
  sha256Hex,
  sha256HexSync,
  splitBatch,
  submitText,
  targetFormat,
} from "@/utils/drama/adaptation";

const caps = (prompt_format?: Capabilities["prompt_format"]): Capabilities => ({
  refs: {
    image: { on: true, max: 9, max_mb: 10 },
    audio: { on: false, max: 0, max_mb: 0 },
    video: { on: false, max: 0, max_mb: 0 },
  },
  prompt: { max_length: 2000 },
  ...(prompt_format === undefined ? {} : { prompt_format }),
});

const seg = (patch: Partial<Segment> = {}): Segment => ({
  id: "s1",
  episodeNo: 1,
  seq: 0,
  seconds: 10,
  prompt: "原稿",
  takes: [],
  chosenTaskId: "",
  status: "idle",
  revision: 1,
  updatedAt: "",
  adaptations: {},
  ...patch,
});

const ad = (prompt: string, sourceHash: string, edited = false) => ({
  prompt,
  sourceHash,
  edited,
  updatedAt: "2026-11-09T10:00:00Z",
});

describe("目标格式", () => {
  test("Seedance 模型不需要转写", () => {
    expect(targetFormat(caps("seedance"))).toBe("seedance");
  });
  test("H3 模型转写为 h3", () => {
    expect(targetFormat(caps("h3"))).toBe("h3");
  });
  test("没配格式（空 / 缺省）按通用", () => {
    expect(targetFormat(caps(""))).toBe("generic");
    expect(targetFormat(caps())).toBe("generic");
  });
  test("还没有模型时不显示转写（当作 Seedance）", () => {
    expect(targetFormat(undefined)).toBe("seedance");
  });
  test("标签用短名", () => {
    expect(formatLabel("seedance")).toBe("Seedance");
    expect(formatLabel("h3")).toBe("H3");
    expect(formatLabel("generic")).toBe("通用");
  });
});

describe("sha256Hex", () => {
  test("与后端一致：UTF-8 全文的小写 hex", async () => {
    expect(await sha256Hex("abc")).toBe(
      "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad",
    );
    const text = "【场景】@[垂钓台](n_1)";
    expect(await sha256Hex(text)).toBe(createHash("sha256").update(text, "utf8").digest("hex"));
  });

  test("没有 crypto.subtle（http + 局域网 IP 访问）时用纯 JS 实现，结果一致", () => {
    for (const text of ["", "abc", "【场景】@[垂钓台](n_1) 😀", "x".repeat(1000)]) {
      expect(sha256HexSync(text)).toBe(createHash("sha256").update(text, "utf8").digest("hex"));
    }
  });
});

describe("转写稿状态", () => {
  test("没有这个格式的转写稿 = 未转写", () => {
    expect(adaptState(seg(), "h3", "h")).toBe("none");
  });
  test("指纹与当前原稿一致 = 最新；不一致 = 已过期", () => {
    const s = seg({ adaptations: { h3: ad("x", "h1") } });
    expect(adaptState(s, "h3", "h1")).toBe("fresh");
    expect(adaptState(s, "h3", "h2")).toBe("stale");
  });
  test("别的格式的转写稿不算", () => {
    const s = seg({ adaptations: { generic: ad("x", "h1") } });
    expect(adaptState(s, "h3", "h1")).toBe("none");
  });
});

describe("提交的文字", () => {
  test("Seedance 提交原稿", () => {
    expect(submitText(seg({ adaptations: { h3: ad("转写", "h") } }), "seedance")).toBe("原稿");
  });
  test("其他格式提交转写稿；没有为空串", () => {
    expect(submitText(seg({ adaptations: { h3: ad("转写", "h") } }), "h3")).toBe("转写");
    expect(submitText(seg(), "h3")).toBe("");
  });
});

describe("批量拆分", () => {
  test("分出已就绪、未转写、已过期，并数出手改过的过期稿", () => {
    const list = [
      seg({ id: "a", prompt: "A", adaptations: { h3: ad("ta", "hA") } }),
      seg({ id: "b", prompt: "B" }),
      seg({ id: "c", prompt: "C", adaptations: { h3: ad("tc", "old", true) } }),
      seg({ id: "d", prompt: "D", adaptations: { h3: ad("td", "old") } }),
    ];
    const hashes: Record<string, string> = { a: "hA", b: "hB", c: "hC", d: "hD" };
    const split = splitBatch(list, "h3", (s) => hashes[s.id]);
    expect(split.ready.map((s) => s.id)).toEqual(["a"]);
    expect(split.none.map((s) => s.id)).toEqual(["b"]);
    expect(split.stale.map((s) => s.id)).toEqual(["c", "d"]);
    expect(split.edited).toBe(1);
  });
});

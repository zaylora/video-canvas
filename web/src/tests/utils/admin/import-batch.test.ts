import { describe, expect, test } from "bun:test";

import type { ModelDraft } from "@/api/admin-ai/type";
import {
  buildImportBody,
  checkImportKey,
  inferDraftKind,
  initialImportRow,
  keyFromUpstream,
  parseImportPrice,
  runImportJobs,
  savedKeys,
  summarizeImport,
  supportedKinds,
  type ImportApi,
  type ImportJob,
} from "@/utils/admin/import-batch";

const draft = (patch: Partial<ModelDraft> = {}): ModelDraft => ({
  upstream_model: "Seedance-1.0_Pro",
  kind: "",
  label: "",
  params: null,
  param_hints: null,
  ...patch,
});

describe("supportedKinds", () => {
  test("按固定顺序列出 endpoints 里的 kind", () => {
    expect(supportedKinds({ endpoints: { image: {}, video: {}, other: {} } })).toEqual([
      "video",
      "image",
    ]);
  });
  test("meta 缺失视为全部支持；endpoints 为 null 视为都不支持", () => {
    expect(supportedKinds(undefined)).toEqual(["text", "video", "image", "audio"]);
    expect(supportedKinds({ endpoints: null })).toEqual([]);
  });
});

describe("inferDraftKind", () => {
  test("草稿给了且渠道支持 → 确定", () => {
    expect(inferDraftKind(draft({ kind: "image" }), ["video", "image"])).toEqual({
      kind: "image",
      certain: true,
    });
  });
  test("渠道只支持一种 → 确定", () => {
    expect(inferDraftKind(draft({ kind: "text" }), ["video"])).toEqual({
      kind: "video",
      certain: true,
    });
  });
  test("按上游名关键词猜，不确定", () => {
    expect(inferDraftKind(draft({ upstream_model: "flux-dev" }), ["video", "image"])).toEqual({
      kind: "image",
      certain: false,
    });
    expect(inferDraftKind(draft(), ["video", "image"])).toEqual({ kind: "video", certain: false });
  });
  test("猜的不在支持范围里就取第一个", () => {
    expect(inferDraftKind(draft({ upstream_model: "gpt-4o" }), ["image", "audio"])).toEqual({
      kind: "image",
      certain: false,
    });
  });
  test("支持列表为空按全部 kind 处理", () => {
    expect(inferDraftKind(draft({ kind: "audio" }), [])).toEqual({ kind: "audio", certain: true });
  });
});

describe("keyFromUpstream / checkImportKey", () => {
  test("上游名转 key，全是非 ASCII 时兜底", () => {
    expect(keyFromUpstream("Seedance-1.0_Pro")).toBe("seedance-1-0-pro");
    expect(keyFromUpstream("通义万相")).toBe("model");
  });
  test("各种不通过的情况", () => {
    const existing = new Set(["taken"]);
    expect(checkImportKey(" ", existing)).toBe("请填写产品标识");
    expect(checkImportKey("-bad", existing)).toContain("字母");
    expect(checkImportKey("有中文", existing)).toContain("字母");
    expect(checkImportKey("a".repeat(129), existing)).toContain("128");
    expect(checkImportKey("taken", existing)).toBe("已存在同名模型");
    expect(checkImportKey("dup", existing, ["dup"])).toBe("和本批其他行重复");
    expect(checkImportKey("ok_1.2-x", existing, ["other"])).toBeNull();
  });
});

describe("initialImportRow", () => {
  test("展示名缺失回退上游名", () => {
    expect(initialImportRow(draft({ kind: "video" }), ["video", "image"])).toEqual({
      key: "seedance-1-0-pro",
      label: "Seedance-1.0_Pro",
      kind: "video",
      certain: true,
    });
  });
});

describe("buildImportBody", () => {
  test("写入 key / 展示名 / kind / 渠道，键顺序与 draftToModelBody 一致", () => {
    const { body, priceSkipped } = buildImportBody(draft(), "ch1", {
      key: " my-key ",
      label: " 我的模型 ",
      kind: "image",
    });
    expect(priceSkipped).toBe(false);
    expect(Object.keys(body).slice(0, 3)).toEqual(["key", "kind", "label"]);
    expect(body.key).toBe("my-key");
    expect(body.kind).toBe("image");
    expect(body.label).toBe("我的模型");
    expect(body.enabled).toBe(false);
    expect(body.channels).toEqual([{ channel: "ch1", upstream_model: "Seedance-1.0_Pro" }]);
    expect((body.pricing as { billing: string }).billing).toBe("per_call");
  });
  test("展示名留空回退上游名", () => {
    const { body } = buildImportBody(draft(), "ch1", { key: "k", label: "", kind: "video" });
    expect(body.label).toBe("Seedance-1.0_Pro");
  });
  test("统一价格：按秒改 per_second，按次改 unit，Token 计费跳过", () => {
    const video = buildImportBody(draft(), "ch1", { key: "k", label: "", kind: "video" }, 7);
    expect((video.body.pricing as { per_second: number }).per_second).toBe(7);
    const image = buildImportBody(draft(), "ch1", { key: "k", label: "", kind: "image" }, 9);
    expect((image.body.pricing as { unit: number }).unit).toBe(9);
    const text = buildImportBody(draft(), "ch1", { key: "k", label: "", kind: "text" }, 9);
    expect(text.priceSkipped).toBe(true);
    expect((text.body.pricing as { billing: string }).billing).toBe("token");
  });
});

describe("parseImportPrice", () => {
  test("留空不改、整数通过、其他报错", () => {
    expect(parseImportPrice("  ")).toEqual({ ok: true });
    expect(parseImportPrice("12")).toEqual({ ok: true, value: 12 });
    expect(parseImportPrice("0")).toEqual({ ok: true, value: 0 });
    expect(parseImportPrice("1.5").ok).toBe(false);
    expect(parseImportPrice("-1").ok).toBe(false);
  });
});

describe("runImportJobs", () => {
  const job = (key: string, priceSkipped = false): ImportJob => ({
    key,
    label: key.toUpperCase(),
    body: { key },
    priceSkipped,
  });

  /** 假接口：按 key 决定行为，并记录调用 */
  const fakeApi = (opts: { issues?: string[]; createFail?: string[]; publishFail?: string[] }) => {
    const calls: string[] = [];
    const api: ImportApi = {
      createDraft: async (body) => {
        const key = body.key as string;
        calls.push(`create:${key}`);
        if (opts.createFail?.includes(key)) throw { message: `${key} 创建失败` };
        return { issues: opts.issues?.includes(key) ? [{}, {}] : [] };
      },
      publish: async (key) => {
        calls.push(`publish:${key}`);
        if (opts.publishFail?.includes(key)) throw { message: "渠道未设置 Key" };
      },
      setEnabled: async (key, enabled) => void calls.push(`enable:${key}:${enabled}`),
    };
    return { api, calls };
  };
  const describeError = (error: unknown) => (error as { message: string }).message;

  test("draft：只保存草稿，失败不中断，进度逐个回调", async () => {
    const { api, calls } = fakeApi({ createFail: ["b"] });
    const progress: string[] = [];
    const out = await runImportJobs(
      [job("a", true), job("b"), job("c")],
      "draft",
      api,
      (done, total) => progress.push(`${done}/${total}`),
      describeError,
    );
    expect(calls).toEqual(["create:a", "create:b", "create:c"]);
    expect(progress).toEqual(["1/3", "2/3", "3/3"]);
    expect(out.map((item) => item.status)).toEqual(["done", "failed", "done"]);
    expect(out[0].reason).toContain("Token");
    expect(out[1].reason).toBe("b 创建失败");
    expect(savedKeys(out)).toEqual(["a", "c"]);
  });

  test("online：有问题的只存草稿记为跳过，没问题的发布并上线", async () => {
    const { api, calls } = fakeApi({ issues: ["b"], publishFail: ["c"] });
    const out = await runImportJobs(
      [job("a"), job("b"), job("c")],
      "online",
      api,
      undefined,
      describeError,
    );
    expect(calls).toEqual([
      "create:a",
      "publish:a",
      "enable:a:true",
      "create:b",
      "create:c",
      "publish:c",
    ]);
    expect(out[0]).toMatchObject({ status: "done", saved: true });
    expect(out[1]).toMatchObject({ status: "skipped", reason: "有 2 个问题，已存为草稿" });
    expect(out[2]).toMatchObject({
      status: "failed",
      saved: true,
      reason: "已存为草稿，上线失败：渠道未设置 Key",
    });
    expect(savedKeys(out)).toEqual(["a", "b", "c"]);
    expect(summarizeImport(out)).toEqual({ done: 1, skipped: 1, failed: 1 });
  });
});

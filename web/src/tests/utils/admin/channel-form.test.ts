import { describe, expect, test } from "bun:test";

import type { ChannelView } from "@/api/admin/ai/type";
import {
  buildChannelRequest,
  channelFormFromView,
  checkBaseUrl,
  checkHeaders,
  emptyChannelForm,
  formatLoad,
  formatRateLimit,
  MAX_CHANNEL_HEADERS,
  type ChannelFormState,
} from "@/utils/admin/channel-form";

const view = (extra: Partial<ChannelView> = {}): ChannelView => ({
  key: "openai-main",
  name: "OpenAI 主渠道",
  base_url: "https://api.openai.com",
  headers: { "User-Agent": "video-canvas-test" },
  rate_limit: { rps: 5, max_concurrency: 0 },
  sort: 10,
  enabled: true,
  secret_set: true,
  secret_key_set: false,
  updated_by: 1,
  updated_at: "",
  created_at: "",
  ...extra,
});

const newForm = (extra: Partial<ChannelFormState> = {}): ChannelFormState => ({
  ...emptyChannelForm(),
  name: "我的渠道",
  baseUrl: "https://api.example.com",
  ...extra,
});

describe("emptyChannelForm", () => {
  test("默认启用、没有请求头、限流留空", () => {
    expect(emptyChannelForm()).toEqual({
      name: "",
      baseUrl: "",
      headers: [],
      enabled: true,
      rps: "",
      maxConcurrency: "",
      maxRunning: "",
    });
  });
});

describe("channelFormFromView", () => {
  test("渠道视图转表单：请求头展开成逐行，0 限流留空", () => {
    expect(channelFormFromView(view())).toEqual({
      name: "OpenAI 主渠道",
      baseUrl: "https://api.openai.com",
      headers: [{ name: "User-Agent", value: "video-canvas-test" }],
      enabled: true,
      rps: "5",
      maxConcurrency: "",
      maxRunning: "",
    });
  });

  test("headers 为空或缺失时没有行", () => {
    expect(channelFormFromView(view({ headers: {} })).headers).toEqual([]);
    expect(channelFormFromView(view({ headers: null as never })).headers).toEqual([]);
  });
});

describe("checkBaseUrl", () => {
  test("合法的 http / https 地址通过", () => {
    expect(checkBaseUrl("https://api.example.com")).toBeNull();
    expect(checkBaseUrl("http://10.0.0.1:8080/v1")).toBeNull();
  });

  test("空值、非法、非 http、带账号密码、带查询参数或片段都拒绝", () => {
    expect(checkBaseUrl("  ")).toContain("Base URL");
    expect(checkBaseUrl("not a url")).toBeTruthy();
    expect(checkBaseUrl("ftp://example.com")).toBeTruthy();
    expect(checkBaseUrl("https://user:pw@example.com")).toBeTruthy();
    expect(checkBaseUrl("https://example.com/v1?key=1")).toBeTruthy();
    expect(checkBaseUrl("https://example.com/v1#top")).toBeTruthy();
  });
});

describe("checkHeaders", () => {
  test("合法请求头无错误，完全空白的行被忽略", () => {
    expect(
      checkHeaders([
        { name: "User-Agent", value: "x" },
        { name: "", value: "" },
        { name: "X-Org", value: "acme" },
      ]),
    ).toEqual({});
  });

  test("名称为空但有值、名称不是合法 token", () => {
    const errors = checkHeaders([
      { name: "", value: "v" },
      { name: "bad name", value: "v" },
      { name: "中文", value: "v" },
    ]);
    expect(errors["headers.0"]).toContain("名称");
    expect(errors["headers.1"]).toContain("不合法");
    expect(errors["headers.2"]).toContain("不合法");
  });

  test("禁用头不区分大小写", () => {
    for (const name of [
      "Host",
      "content-length",
      "Transfer-Encoding",
      "CONNECTION",
      "Upgrade",
      "Authorization",
      "Proxy-Authorization",
      "Cookie",
    ]) {
      expect(checkHeaders([{ name, value: "v" }])["headers.0"]).toContain("不能设置");
    }
  });

  test("名称不区分大小写不得重复，错误挂在后出现的那行", () => {
    const errors = checkHeaders([
      { name: "X-Org", value: "a" },
      { name: "x-org", value: "b" },
    ]);
    expect(errors["headers.0"]).toBeUndefined();
    expect(errors["headers.1"]).toContain("重复");
  });

  test("值超过 1024 字符或含换行 / 控制字符", () => {
    expect(checkHeaders([{ name: "X-A", value: "a".repeat(1025) }])["headers.0"]).toContain("1024");
    expect(checkHeaders([{ name: "X-A", value: "a".repeat(1024) }])).toEqual({});
    expect(checkHeaders([{ name: "X-A", value: "a\nb" }])["headers.0"]).toContain("换行");
    expect(checkHeaders([{ name: "X-A", value: "a\u0007b" }])["headers.0"]).toContain("换行");
  });

  test("最多 32 条", () => {
    const rows = Array.from({ length: MAX_CHANNEL_HEADERS + 1 }, (_, i) => ({
      name: `X-H${i}`,
      value: "v",
    }));
    expect(checkHeaders(rows).headers).toContain("32");
    expect(checkHeaders(rows.slice(0, MAX_CHANNEL_HEADERS))).toEqual({});
  });
});

describe("buildChannelRequest（新建）", () => {
  test("合法表单：生成 create 请求体，限流 0 表示不限", () => {
    const result = buildChannelRequest(
      newForm({ rps: "3", headers: [{ name: " User-Agent ", value: " ua " }] }),
    );
    expect(result.ok).toBe(true);
    if (!result.ok) return;
    expect(result.changed).toBe(true);
    // 不带 key：渠道 key 由后端生成
    expect(result.create).toEqual({
      name: "我的渠道",
      base_url: "https://api.example.com",
      headers: { "User-Agent": "ua" },
      rate_limit: { rps: 3, max_concurrency: 0, max_running: 0 },
      enabled: true,
    });
  });

  test("名称、地址、请求头、限流错误一次报出", () => {
    const result = buildChannelRequest(
      newForm({
        name: "  ",
        baseUrl: "",
        rps: "-1",
        maxConcurrency: "1.5",
        maxRunning: "x",
        headers: [{ name: "Cookie", value: "v" }],
      }),
    );
    expect(result.ok).toBe(false);
    if (result.ok) return;
    expect(Object.keys(result.errors).sort()).toEqual(
      ["baseUrl", "headers.0", "maxConcurrency", "maxRunning", "name", "rps"].sort(),
    );
  });
});

describe("buildChannelRequest（编辑）", () => {
  test("没改动：changed=false，update 为空", () => {
    const original = view();
    const result = buildChannelRequest(channelFormFromView(original), original);
    expect(result.ok).toBe(true);
    if (!result.ok) return;
    expect(result.changed).toBe(false);
    expect(result.update).toEqual({});
  });

  test("只带改过的字段", () => {
    const original = view();
    const form = { ...channelFormFromView(original), name: "新名字", enabled: false };
    const result = buildChannelRequest(form, original);
    expect(result.ok).toBe(true);
    if (!result.ok) return;
    expect(result.update).toEqual({ name: "新名字", enabled: false });
  });

  test("请求头整体替换：改值、增删、清空都算变化；顺序不同不算", () => {
    const original = view({ headers: { A: "1", B: "2" } });
    const reordered = buildChannelRequest(
      {
        ...channelFormFromView(original),
        headers: [
          { name: "B", value: "2" },
          { name: "A", value: "1" },
        ],
      },
      original,
    );
    expect(reordered.ok && reordered.changed).toBe(false);

    const cleared = buildChannelRequest(
      { ...channelFormFromView(original), headers: [] },
      original,
    );
    expect(cleared.ok && cleared.update).toEqual({ headers: {} });

    const changed = buildChannelRequest(
      { ...channelFormFromView(original), headers: [{ name: "A", value: "9" }] },
      original,
    );
    expect(changed.ok && changed.update).toEqual({ headers: { A: "9" } });
  });

  test("限流整体替换：任一维度变了就带全部三个", () => {
    const original = view();
    const result = buildChannelRequest(
      { ...channelFormFromView(original), maxRunning: "4" },
      original,
    );
    expect(result.ok && result.update).toEqual({
      rate_limit: { rps: 5, max_concurrency: 0, max_running: 4 },
    });
  });

  test("编辑的 update 不含 key（渠道 key 创建后不可改）", () => {
    const original = view();
    const result = buildChannelRequest({ ...channelFormFromView(original), name: "x" }, original);
    expect(result.ok && "key" in result.update).toBe(false);
  });
});

describe("formatRateLimit", () => {
  test("没有任何限流写“不限”", () => {
    expect(formatRateLimit(null)).toBe("不限");
    expect(formatRateLimit({})).toBe("不限");
    expect(formatRateLimit({ rps: 0, max_concurrency: 0, max_running: 0 })).toBe("不限");
  });

  test("有任一限流就三项都写，没设的显示不限", () => {
    expect(formatRateLimit({ rps: 5, max_running: 3 })).toBe("rps 5 · 并发 不限 · 同时生成 3");
  });
});

describe("formatLoad", () => {
  test("生成中 / 上限 + 排队", () => {
    expect(formatLoad({ channel: "a", running: 2, waiting: 5 }, 4)).toBe("生成中 2/4 · 排队 5");
  });

  test("没有负载记录算空闲 0；上限为 0 或缺省写不限", () => {
    expect(formatLoad(undefined, 4)).toBe("生成中 0/4 · 排队 0");
    expect(formatLoad({ channel: "a", running: 1, waiting: 0 }, 0)).toBe("生成中 1/不限 · 排队 0");
    expect(formatLoad(undefined, undefined)).toBe("生成中 0/不限 · 排队 0");
  });
});

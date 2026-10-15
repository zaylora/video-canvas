import { describe, expect, test } from "bun:test";

import type { ChannelView, PluginView } from "@/api/admin/ai/type";
import { publishBlockReason, resolveModelChannel } from "@/utils/admin/model-channel";

const version = (
  plugin: string,
  ver: string,
  meta: PluginView["versions"][number]["meta"],
  sha256 = "",
) => ({
  id: 1,
  plugin_key: plugin,
  version: ver,
  sha256,
  created_at: "",
  created_by: 1,
  model_count: 0,
  meta,
});

const plugins: PluginView[] = [
  {
    key: "kling",
    name: "可灵直连",
    source: "uploaded",
    enabled: true,
    updated_at: "",
    versions: [
      version(
        "kling",
        "0.2.0",
        {
          auth: { type: "custom" },
          endpoints: { video: { mode: "async" } },
          channelSettings: { region: { type: "string", label: "区域", required: true } },
        },
        "c41d07abc41d07ab",
      ),
    ],
  },
  {
    key: "open",
    name: "开放接口",
    source: "uploaded",
    enabled: true,
    updated_at: "",
    versions: [
      version("open", "1.0.0", { auth: { type: "none" }, endpoints: { text: { mode: "sync" } } }),
    ],
  },
  {
    key: "off",
    name: "停用的协议",
    source: "uploaded",
    enabled: false,
    updated_at: "",
    versions: [
      version("off", "1.0.0", { auth: { type: "bearer" }, endpoints: { text: { mode: "sync" } } }),
    ],
  },
];

const channel = (over: Partial<ChannelView> = {}): ChannelView => ({
  key: "main",
  name: "主渠道",
  base_url: "https://x.test",
  headers: {},
  rate_limit: null,
  sort: 10,
  enabled: true,
  secret_set: true,
  secret_key_set: false,
  updated_by: 0,
  updated_at: "",
  created_at: "",
  ...over,
});

type ProtocolInput = { plugin: string; version: string; settings?: Record<string, unknown> };

const body = (channelKey: string, kind: string, protocol?: ProtocolInput | null) => ({
  kind,
  channels: [
    {
      channel: channelKey,
      upstream_model: "m",
      ...(protocol ? { protocol: { settings: {}, ...protocol } } : {}),
    },
  ],
});

const kling = { plugin: "kling", version: "0.2.0", settings: { region: "cn" } };

describe("resolveModelChannel", () => {
  test("带出协议的插件名、版本、sha 前 8 位、鉴权与渠道 Key 状态", () => {
    const info = resolveModelChannel(body("main", "video", kling), [channel()], plugins);
    expect(info).toMatchObject({
      channelKey: "main",
      kind: "video",
      pluginName: "可灵直连",
      pluginVersion: "0.2.0",
      sha8: "c41d07ab",
      authType: "custom",
      secretSet: true,
      keyMissing: false,
      supportsKind: true,
      protocolProblem: null,
    });
    expect(info.authLabel).toContain("自行签名");
  });

  test("协议版本需要鉴权（非 none）而渠道没设 Key：keyMissing 为 true", () => {
    const info = resolveModelChannel(
      body("main", "video", kling),
      [channel({ secret_set: false })],
      plugins,
    );
    expect(info.keyMissing).toBe(true);
  });

  test("鉴权为 none 的协议不需要 Key", () => {
    const info = resolveModelChannel(
      body("main", "text", { plugin: "open", version: "1.0.0" }),
      [channel({ secret_set: false })],
      plugins,
    );
    expect(info.authType).toBe("none");
    expect(info.keyMissing).toBe(false);
  });

  test("Agent 模型只看渠道 Key，不需要协议", () => {
    const missing = resolveModelChannel(
      body("main", "agent"),
      [channel({ secret_set: false })],
      plugins,
    );
    expect(missing.keyMissing).toBe(true);
    expect(missing.protocolProblem).toBeNull();
    expect(missing.authType).toBeNull();
    expect(resolveModelChannel(body("main", "agent"), [channel()], plugins).keyMissing).toBe(false);
  });

  test("协议没选、插件不存在、已停用、版本不存在、不支持该能力、设置项不合法都给出问题", () => {
    const problem = (kind: string, protocol: ProtocolInput | null) =>
      resolveModelChannel(body("main", kind, protocol), [channel()], plugins).protocolProblem;
    expect(problem("video", null)).toBe("该模型还没有选择调用协议");
    expect(problem("video", { plugin: "ghost", version: "1.0.0" })).toContain("不存在");
    expect(problem("text", { plugin: "off", version: "1.0.0" })).toContain("已停用");
    expect(problem("video", { ...kling, version: "9.9.9" })).toContain("版本");
    expect(problem("text", kling)).toBe("协议 kling@0.2.0 不支持 text 类型的模型");
    expect(problem("video", { ...kling, settings: {} })).toContain("协议设置不合法");
    expect(problem("video", { ...kling, settings: {} })).toContain("区域");
  });

  test("协议信息未知时不断言鉴权与 Key", () => {
    const info = resolveModelChannel(
      body("main", "video", { plugin: "ghost", version: "1.0.0" }),
      [channel({ secret_set: false })],
      plugins,
    );
    expect(info.authType).toBeNull();
    expect(info.keyMissing).toBe(false);
    expect(info.supportsKind).toBeNull();
  });

  test("没选渠道或渠道清单里没有时 channel 为 null", () => {
    expect(resolveModelChannel(body("", "video", kling), [channel()], plugins).channel).toBeNull();
    const none = resolveModelChannel(body("nope", "video", kling), [channel()], plugins);
    expect(none.channel).toBeNull();
    expect(none.channelKey).toBe("nope");
    expect(none.keyMissing).toBe(false);
  });
});

describe("publishBlockReason（镜像后端的启用检查链）", () => {
  const info = (b: unknown, channels: ChannelView[] = [channel()]) =>
    resolveModelChannel(b, channels, plugins);

  test("一切正常返回 null；Agent 渠道有 Key 也正常", () => {
    expect(publishBlockReason(info(body("main", "video", kling)), true)).toBeNull();
    expect(publishBlockReason(info(body("main", "agent")), true)).toBeNull();
  });

  test("没选能力、没选渠道、渠道不存在、渠道停用", () => {
    expect(publishBlockReason(info(body("main", "")), true)).toContain("能力");
    expect(publishBlockReason(info(body("", "video", kling)), true)).toContain("选择渠道");
    expect(publishBlockReason(info(body("nope", "video", kling)), true)).toContain("不存在");
    expect(
      publishBlockReason(info(body("main", "video", kling), [channel({ enabled: false })]), true),
    ).toContain("停用");
  });

  test("Agent 模型带了协议：正文校验不过", () => {
    expect(publishBlockReason(info(body("main", "agent", kling)), true)).toContain("Agent");
  });

  test("Agent 模型渠道没有 Key：写明渠道名与去处", () => {
    const reason = publishBlockReason(
      info(body("main", "agent"), [channel({ secret_set: false })]),
      true,
    );
    expect(reason).toContain("主渠道");
    expect(reason).toContain("Key");
  });

  test("协议层面的问题原样给出，并排在 Key 之前", () => {
    expect(publishBlockReason(info(body("main", "video")), true)).toBe("该模型还没有选择调用协议");
    expect(
      publishBlockReason(info(body("main", "text", { plugin: "off", version: "1.0.0" })), true),
    ).toContain("已停用");
    expect(
      publishBlockReason(
        info(body("main", "video", { ...kling, settings: {} }), [channel({ secret_set: false })]),
        true,
      ),
    ).toContain("协议设置不合法");
  });

  test("协议需要鉴权而渠道没设 Key：带渠道名", () => {
    const reason = publishBlockReason(
      info(body("main", "video", kling), [channel({ secret_set: false })]),
      true,
    );
    expect(reason).toContain("Key");
    expect(reason).toContain("主渠道");
  });

  test("清单还没加载好时不下结论", () => {
    expect(publishBlockReason(info(body("nope", "video", kling), []), false)).toBeNull();
  });
});

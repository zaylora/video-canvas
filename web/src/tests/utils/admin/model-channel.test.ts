import { describe, expect, test } from "bun:test";

import type { ChannelView, PluginView } from "@/api/admin-ai/type";
import { publishBlockReason, resolveModelChannel } from "@/utils/admin/model-channel";

const plugins = [
  {
    key: "kling",
    name: "可灵直连",
    source: "uploaded",
    enabled: true,
    updated_at: "",
    versions: [
      {
        id: 2,
        plugin_key: "kling",
        version: "0.2.0",
        sha256: "c41d07abc41d07ab",
        created_at: "",
        created_by: 1,
        channel_count: 1,
        meta: { auth: { type: "custom" }, endpoints: { video: { mode: "async" } } },
      },
    ],
  },
  {
    key: "open",
    name: "开放接口",
    source: "uploaded",
    enabled: true,
    updated_at: "",
    versions: [
      {
        id: 5,
        plugin_key: "open",
        version: "1.0.0",
        sha256: "",
        created_at: "",
        created_by: 1,
        channel_count: 1,
        meta: { auth: { type: "none" }, endpoints: { text: { mode: "sync" } } },
      },
    ],
  },
] as PluginView[];

const channel = (over: Partial<ChannelView>): ChannelView => ({
  key: "kling-direct",
  name: "可灵直连",
  plugin_key: "kling",
  plugin_version_id: 2,
  plugin_version: "0.2.0",
  base_url: "https://x.test",
  trusted_internal: false,
  allow_credentials: true,
  settings: null,
  rate_limit: null,
  enabled: true,
  secret_set: true,
  updated_by: 0,
  updated_at: "",
  created_at: "",
  ...over,
});

const body = (channelKey: string, kind = "video") => ({
  kind,
  channels: [{ channel: channelKey, upstream_model: "m" }],
});

describe("resolveModelChannel", () => {
  test("选中渠道后带出插件名、版本、sha 前 8 位、鉴权与 Key 状态", () => {
    const info = resolveModelChannel(body("kling-direct"), [channel({})], plugins);
    expect(info).toMatchObject({
      channelKey: "kling-direct",
      pluginName: "可灵直连",
      pluginVersion: "0.2.0",
      sha8: "c41d07ab",
      authType: "custom",
      secretSet: true,
      keyMissing: false,
      supportsKind: true,
    });
    expect(info.authLabel).toContain("自行签名");
  });

  test("需要 Key（非 none 鉴权）却没设置：keyMissing 为 true", () => {
    const info = resolveModelChannel(
      body("kling-direct"),
      [channel({ secret_set: false })],
      plugins,
    );
    expect(info.keyMissing).toBe(true);
  });

  test("鉴权为 none 的插件不需要 Key，没设置也不算缺失", () => {
    const info = resolveModelChannel(
      body("o", "text"),
      [
        channel({
          key: "o",
          plugin_key: "open",
          plugin_version_id: 5,
          plugin_version: "1.0.0",
          secret_set: false,
        }),
      ],
      plugins,
    );
    expect(info.authType).toBe("none");
    expect(info.keyMissing).toBe(false);
  });

  test("插件信息未知时不断言：authType 为 null，keyMissing 为 false，supportsKind 为 null", () => {
    const info = resolveModelChannel(
      body("k"),
      [channel({ key: "k", plugin_key: "ghost", plugin_version_id: 9, secret_set: false })],
      plugins,
    );
    expect(info.authType).toBeNull();
    expect(info.keyMissing).toBe(false);
    expect(info.supportsKind).toBeNull();
  });

  test("kind 不在插件 endpoints 里时 supportsKind 为 false；没选渠道时各字段为空", () => {
    expect(
      resolveModelChannel(body("kling-direct", "text"), [channel({})], plugins).supportsKind,
    ).toBe(false);
    const none = resolveModelChannel(body(""), [channel({})], plugins);
    expect(none.channel).toBeNull();
    expect(none.channelKey).toBe("");
  });
});

describe("publishBlockReason", () => {
  const ok = resolveModelChannel(body("kling-direct"), [channel({})], plugins);

  test("一切正常返回 null", () => {
    expect(publishBlockReason(ok, true)).toBeNull();
  });

  test("Key 未设置时禁用并写明原因，带渠道名与去处", () => {
    const info = resolveModelChannel(
      body("kling-direct"),
      [channel({ secret_set: false })],
      plugins,
    );
    const reason = publishBlockReason(info, true);
    expect(reason).toContain("Key");
    expect(reason).toContain("可灵直连");
  });

  test("渠道不存在、已停用、没选渠道都给出原因", () => {
    expect(
      publishBlockReason(resolveModelChannel(body("nope"), [channel({})], plugins), true),
    ).toContain("不存在");
    expect(
      publishBlockReason(
        resolveModelChannel(body("kling-direct"), [channel({ enabled: false })], plugins),
        true,
      ),
    ).toContain("停用");
    expect(
      publishBlockReason(resolveModelChannel(body(""), [channel({})], plugins), true),
    ).toContain("选择渠道");
  });

  test("清单没加载好时不下结论", () => {
    expect(publishBlockReason(resolveModelChannel(body("nope"), [], plugins), false)).toBeNull();
  });
});

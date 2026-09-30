import { describe, expect, test } from "bun:test";

import type {
  ChannelView,
  PluginMeta,
  PluginVersionView,
  PluginView,
} from "@/api/admin-ai/type";
import {
  availableUpgrade,
  channelSupportsKind,
  channelsForKind,
  checkPluginFile,
  compareSemver,
  describeAuth,
  findPluginVersion,
  latestVersion,
  metaSummary,
  normalizeIssues,
  pluginChannelCount,
  shortSha,
  summarizeUpload,
  versionDeleteBlock,
} from "@/utils/admin/plugin";

const version = (
  id: number,
  ver: string,
  meta: PluginMeta | null,
  channelCount = 0,
): PluginVersionView => ({
  id,
  plugin_key: "p",
  version: ver,
  sha256: "abcdef1234567890",
  created_at: "",
  created_by: 0,
  channel_count: channelCount,
  meta,
});

const plugin = (
  key: string,
  versions: PluginVersionView[],
  extra: Partial<PluginView> = {},
): PluginView => ({
  key,
  name: key,
  source: "uploaded",
  enabled: true,
  updated_at: "",
  versions,
  ...extra,
});

const channel = (extra: Partial<ChannelView> = {}): ChannelView => ({
  key: "c1",
  name: "c1",
  plugin_key: "p",
  plugin_version_id: 1,
  plugin_version: "1.0.0",
  base_url: "https://x.test",
  trusted_internal: false,
  allow_credentials: false,
  settings: null,
  rate_limit: null,
  enabled: true,
  secret_set: true,
  updated_by: 0,
  updated_at: "",
  created_at: "",
  ...extra,
});

const videoMeta: PluginMeta = { endpoints: { video: { mode: "async" } } };
const textMeta: PluginMeta = { endpoints: { text: { mode: "sync" } } };

describe("checkPluginFile", () => {
  test("合法文件通过", () => {
    expect(checkPluginFile({ name: "kling.js", size: 1000 })).toBeNull();
    expect(checkPluginFile({ name: "KLING.JS", size: 1 })).toBeNull();
  });

  test("扩展名不对、为空、超过 512KB 都被拦下", () => {
    expect(checkPluginFile({ name: "a.ts", size: 10 })).toContain(".js");
    expect(checkPluginFile({ name: "a.js", size: 0 })).toContain("空");
    expect(checkPluginFile({ name: "a.js", size: 512 * 1024 })).toBeNull();
    expect(checkPluginFile({ name: "a.js", size: 512 * 1024 + 1 })).toContain("512KB");
  });
});

describe("normalizeIssues / summarizeUpload", () => {
  test("丢掉坏条目、去重、按 path 排序且同 path 保持原顺序", () => {
    const issues = normalizeIssues([
      { path: "meta.version", message: "b" },
      null,
      { path: "meta.auth", message: "x" },
      { path: "meta.version", message: "b" },
      { path: "meta.version", message: "a" },
      { path: 1, message: "" },
    ]);
    expect(issues.map((item) => `${item.path}|${item.message}`)).toEqual([
      "|未知问题",
      "meta.auth|x",
      "meta.version|b",
      "meta.version|a",
    ]);
  });

  test("非数组返回空数组", () => {
    expect(normalizeIssues(undefined)).toEqual([]);
    expect(normalizeIssues("x")).toEqual([]);
  });

  test("通过：标题带插件与版本；未通过：标题带问题数", () => {
    const ok = summarizeUpload({
      accepted: true,
      issues: [],
      version: version(3, "1.2.0", null),
    });
    expect(ok.tone).toBe("success");
    expect(ok.title).toContain("p@1.2.0");
    const bad = summarizeUpload({
      accepted: false,
      issues: [{ path: "meta.version", message: "已存在" }],
      version: null,
    });
    expect(bad.tone).toBe("error");
    expect(bad.title).toContain("1 个问题");
    expect(summarizeUpload(null).tone).toBe("error");
  });
});

describe("shortSha / describeAuth / metaSummary", () => {
  test("sha 取前 8 位，空值不崩", () => {
    expect(shortSha("0123456789abcdef")).toBe("01234567");
    expect(shortSha(null)).toBe("");
  });

  test("鉴权说明覆盖各种方式", () => {
    expect(describeAuth(null)).toBe("无需鉴权");
    expect(describeAuth({ type: "bearer" })).toContain("Bearer");
    expect(describeAuth({ type: "header", name: "X-Key" })).toContain("X-Key");
    expect(describeAuth({ type: "custom" })).toContain("自行签名");
    expect(describeAuth({ type: "weird" })).toBe("weird");
  });

  test("meta 缺失或字段为 null 时摘要不崩", () => {
    expect(metaSummary(null).endpoints).toEqual([]);
    const summary = metaSummary({
      auth: { type: "custom" },
      allowedHosts: null,
      endpoints: { video: { mode: "async" }, text: {} },
      channelSettings: { region: { type: "enum", label: "区域" } },
      import: {},
    });
    expect(summary.authType).toBe("custom");
    expect(summary.allowedHosts).toEqual([]);
    expect(summary.endpoints).toEqual([
      { kind: "video", mode: "async" },
      { kind: "text", mode: "?" },
    ]);
    expect(summary.settingNames).toEqual(["region"]);
    expect(summary.importable).toBe(true);
  });
});

describe("findPluginVersion / channelSupportsKind", () => {
  const plugins = [
    plugin("p", [version(2, "2.0.0", textMeta), version(1, "1.0.0", videoMeta)]),
  ];

  test("先按版本 ID 找，找不到再按版本号", () => {
    expect(findPluginVersion(plugins, "p", { id: 1 })?.version).toBe("1.0.0");
    expect(findPluginVersion(plugins, "p", { id: 99, version: "2.0.0" })?.id).toBe(2);
    expect(findPluginVersion(plugins, "nope", { id: 1 })).toBeUndefined();
  });

  test("渠道是否支持 kind：找不到插件版本时返回 null 而不是 false", () => {
    expect(channelSupportsKind(plugins, channel(), "video")).toBe(true);
    expect(channelSupportsKind(plugins, channel(), "text")).toBe(false);
    expect(channelSupportsKind([], channel(), "video")).toBeNull();
  });
});

describe("channelsForKind", () => {
  const plugins = [
    plugin("p", [version(1, "1.0.0", videoMeta)]),
    plugin("off", [version(5, "1.0.0", videoMeta)], { enabled: false }),
    plugin("t", [version(7, "1.0.0", textMeta)]),
  ];

  test("只列启用、插件未停用、且支持该 kind 的渠道", () => {
    const list = [
      channel({ key: "ok" }),
      channel({ key: "disabled", enabled: false }),
      channel({ key: "plugin-off", plugin_key: "off", plugin_version_id: 5 }),
      channel({ key: "wrong-kind", plugin_key: "t", plugin_version_id: 7 }),
    ];
    expect(channelsForKind(list, plugins, "video").map((item) => item.key)).toEqual(["ok"]);
    expect(channelsForKind(list, plugins, "text").map((item) => item.key)).toEqual(["wrong-kind"]);
  });

  test("插件信息还没加载好时保留渠道，不把它误判成不支持", () => {
    const list = [channel({ key: "unknown", plugin_key: "ghost", plugin_version_id: 404 })];
    expect(channelsForKind(list, plugins, "video")).toHaveLength(1);
  });
});

describe("版本比较与升级", () => {
  test("compareSemver 按主次修订逐段比较", () => {
    expect(compareSemver("1.10.0", "1.9.0")).toBeGreaterThan(0);
    expect(compareSemver("1.0.0", "1.0.0")).toBe(0);
    expect(compareSemver("0.3.0", "0.20.0")).toBeLessThan(0);
    expect(compareSemver("1.0", "1.0.0")).toBe(0);
  });

  const plugins = [
    plugin("p", [
      version(3, "1.1.0", videoMeta),
      version(2, "1.0.5", videoMeta),
      version(1, "1.0.0", videoMeta),
    ]),
  ];

  test("availableUpgrade 找出比当前更新的最高版本", () => {
    expect(availableUpgrade(plugins, { plugin_key: "p", plugin_version: "1.0.0" })?.version).toBe("1.1.0");
    expect(availableUpgrade(plugins, { plugin_key: "p", plugin_version: "1.1.0" })).toBeNull();
    expect(availableUpgrade(plugins, { plugin_key: "ghost", plugin_version: "1.0.0" })).toBeNull();
  });

  test("latestVersion 取 semver 最高的，不依赖数组顺序", () => {
    const shuffled = plugin("p", [version(1, "1.0.0", null), version(3, "2.0.0", null), version(2, "1.5.0", null)]);
    expect(latestVersion(shuffled)?.version).toBe("2.0.0");
    expect(latestVersion(undefined)).toBeUndefined();
  });
});

describe("versionDeleteBlock / pluginChannelCount", () => {
  test("内置版本与有渠道引用的版本不能删，并写明原因", () => {
    expect(versionDeleteBlock({ source: "builtin" }, { channel_count: 0 })).toContain("内置");
    expect(versionDeleteBlock({ source: "uploaded" }, { channel_count: 2 })).toContain("2 个渠道");
    expect(versionDeleteBlock({ source: "uploaded" }, { channel_count: 0 })).toBeNull();
  });

  test("插件在用渠道数是各版本之和", () => {
    expect(
      pluginChannelCount({ versions: [version(1, "1.0.0", null, 2), version(2, "2.0.0", null, 1)] }),
    ).toBe(3);
    expect(pluginChannelCount({ versions: [] })).toBe(0);
  });
});

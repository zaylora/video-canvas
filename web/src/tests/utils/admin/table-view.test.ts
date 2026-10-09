import { describe, expect, test } from "bun:test";

import type { ChannelView, PluginMeta, PluginVersionView, PluginView } from "@/api/admin/ai/type";
import { filterChannels, filterPlugins, paginate } from "@/utils/admin/table-view";

const bearer: PluginMeta = {
  auth: { type: "bearer" },
  endpoints: { image: { mode: "async" }, video: { mode: "async" } },
};
const imageOnly: PluginMeta = { auth: { type: "bearer" }, endpoints: { image: { mode: "async" } } };

const version = (
  id: number,
  plugin_key: string,
  ver: string,
  meta: PluginMeta = bearer,
): PluginVersionView => ({
  id,
  plugin_key,
  version: ver,
  sha256: "",
  created_at: "",
  created_by: 0,
  channel_count: 0,
  meta,
});

const plugin = (extra: Partial<PluginView> = {}): PluginView => ({
  key: "p",
  name: "插件P",
  source: "builtin",
  enabled: true,
  updated_at: "",
  versions: [version(2, "p", "1.1.0"), version(1, "p", "1.0.0")],
  ...extra,
});

const channel = (extra: Partial<ChannelView> = {}): ChannelView => ({
  key: "c1",
  name: "主线",
  plugin_key: "p",
  plugin_version_id: 2,
  plugin_version: "1.1.0",
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

const noChannelFilter = { query: "", kind: "", plugin: "", status: "" };
const noPluginFilter = { query: "", kind: "", source: "", status: "" };

describe("paginate", () => {
  const list = Array.from({ length: 25 }, (_, index) => index + 1);

  test("取指定页", () => {
    expect(paginate(list, 2, 10)).toEqual({
      rows: [11, 12, 13, 14, 15, 16, 17, 18, 19, 20],
      page: 2,
      pageCount: 3,
    });
  });

  test("页码超出总页数时落到最后一页", () => {
    const result = paginate(list, 9, 10);
    expect(result.page).toBe(3);
    expect(result.rows).toEqual([21, 22, 23, 24, 25]);
  });

  test("页码小于 1 时落到第一页", () => {
    expect(paginate(list, 0, 10).page).toBe(1);
  });

  test("空列表：一页、没有行", () => {
    expect(paginate([], 1, 10)).toEqual({ rows: [], page: 1, pageCount: 1 });
  });
});

describe("filterChannels", () => {
  const plugins = [
    plugin(),
    plugin({
      key: "img",
      name: "生图",
      source: "uploaded",
      versions: [version(3, "img", "1.0.0", imageOnly)],
    }),
  ];
  const channels = [
    channel({ key: "a", name: "华东", base_url: "https://east.test" }),
    channel({
      key: "b",
      name: "亚盛生图",
      plugin_key: "img",
      plugin_version_id: 3,
      plugin_version: "1.0.0",
      secret_set: false,
    }),
    channel({ key: "c", name: "旧版", plugin_version_id: 1, plugin_version: "1.0.0" }),
    channel({ key: "d", name: "停用的", enabled: false }),
  ];
  const keys = (list: ChannelView[]) => list.map((item) => item.key);

  test("没有筛选条件返回全部", () => {
    expect(keys(filterChannels(channels, plugins, noChannelFilter))).toEqual(["a", "b", "c", "d"]);
  });

  test("搜索匹配名称、key、地址，忽略大小写和首尾空格", () => {
    expect(
      keys(filterChannels(channels, plugins, { ...noChannelFilter, query: " 华东 " })),
    ).toEqual(["a"]);
    expect(keys(filterChannels(channels, plugins, { ...noChannelFilter, query: "EAST" }))).toEqual([
      "a",
    ]);
    expect(keys(filterChannels(channels, plugins, { ...noChannelFilter, query: "b" }))).toContain(
      "b",
    );
  });

  test("按能力筛：插件版本声明了该能力的渠道", () => {
    expect(keys(filterChannels(channels, plugins, { ...noChannelFilter, kind: "video" }))).toEqual([
      "a",
      "c",
      "d",
    ]);
  });

  test("按能力筛：读不到插件版本的渠道不被排除", () => {
    const orphan = channel({ key: "x", plugin_key: "gone", plugin_version_id: 9 });
    expect(keys(filterChannels([orphan], plugins, { ...noChannelFilter, kind: "video" }))).toEqual([
      "x",
    ]);
  });

  test("按插件筛", () => {
    expect(keys(filterChannels(channels, plugins, { ...noChannelFilter, plugin: "img" }))).toEqual([
      "b",
    ]);
  });

  test("按状态筛：与状态列的健康状态一致", () => {
    const status = (value: string) =>
      keys(filterChannels(channels, plugins, { ...noChannelFilter, status: value }));
    expect(status("ok")).toEqual(["a", "c"]);
    expect(status("warn")).toEqual(["b"]);
    expect(status("off")).toEqual(["d"]);
    expect(status("bad")).toEqual([]);
  });

  test("按状态筛：可升级 = 插件有更新版本", () => {
    expect(
      keys(filterChannels(channels, plugins, { ...noChannelFilter, status: "upgrade" })),
    ).toEqual(["c"]);
  });

  test("插件停用的渠道算不可用", () => {
    const off = [plugin({ enabled: false })];
    expect(keys(filterChannels([channel()], off, { ...noChannelFilter, status: "bad" }))).toEqual([
      "c1",
    ]);
  });

  test("多个条件同时生效", () => {
    expect(
      keys(
        filterChannels(channels, plugins, {
          query: "旧",
          kind: "image",
          plugin: "p",
          status: "upgrade",
        }),
      ),
    ).toEqual(["c"]);
  });
});

describe("filterPlugins", () => {
  const plugins = [
    plugin({ key: "newapi", name: "New API" }),
    plugin({
      key: "img",
      name: "生图",
      source: "uploaded",
      versions: [version(3, "img", "1.0.0", imageOnly)],
    }),
    plugin({ key: "off", name: "已关", enabled: false, source: "uploaded" }),
  ];
  const channels = [
    channel({ key: "a", plugin_key: "newapi", plugin_version: "1.0.0" }),
    channel({ key: "b", plugin_key: "img", plugin_version_id: 3, plugin_version: "1.0.0" }),
  ];
  const keys = (list: PluginView[]) => list.map((item) => item.key);

  test("没有筛选条件返回全部", () => {
    expect(keys(filterPlugins(plugins, channels, noPluginFilter))).toEqual([
      "newapi",
      "img",
      "off",
    ]);
  });

  test("搜索匹配名称和 key", () => {
    expect(keys(filterPlugins(plugins, channels, { ...noPluginFilter, query: "new" }))).toEqual([
      "newapi",
    ]);
    expect(keys(filterPlugins(plugins, channels, { ...noPluginFilter, query: "生图" }))).toEqual([
      "img",
    ]);
  });

  test("按能力筛：最新版本声明了该能力", () => {
    expect(keys(filterPlugins(plugins, channels, { ...noPluginFilter, kind: "video" }))).toEqual([
      "newapi",
      "off",
    ]);
  });

  test("按来源筛", () => {
    expect(
      keys(filterPlugins(plugins, channels, { ...noPluginFilter, source: "uploaded" })),
    ).toEqual(["img", "off"]);
  });

  test("按状态筛：已启用 / 已停用 / 有旧版渠道", () => {
    const status = (value: string) =>
      keys(filterPlugins(plugins, channels, { ...noPluginFilter, status: value }));
    expect(status("on")).toEqual(["newapi", "img"]);
    expect(status("off")).toEqual(["off"]);
    expect(status("outdated")).toEqual(["newapi"]);
  });
});

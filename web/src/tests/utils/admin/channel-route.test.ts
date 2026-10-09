import { describe, expect, test } from "bun:test";

import { parseChannelRoute, parsePluginRoute } from "@/utils/admin/channel-route";

const channels = [{ key: "newapi" }, { key: "yswg" }];
const plugins = [{ key: "newapi" }, { key: "yswg-image" }];
const params = (query: string) => new URLSearchParams(query);

describe("parseChannelRoute", () => {
  test("没有参数：不开弹窗", () => {
    expect(parseChannelRoute(params(""), channels, { canWrite: true, ready: true })).toEqual({
      target: null,
      missing: null,
    });
  });

  test("?key=<key> 打开该渠道的配置", () => {
    expect(
      parseChannelRoute(params("key=newapi"), channels, { canWrite: true, ready: true }).target,
    ).toEqual({ kind: "edit", key: "newapi", tab: "config" });
  });

  test("?key=<key>&tab= 指定页签；未知页签回到配置", () => {
    const parse = (query: string) =>
      parseChannelRoute(params(query), channels, { canWrite: true, ready: true }).target;
    expect(parse("key=newapi&tab=config")).toEqual({ kind: "edit", key: "newapi", tab: "config" });
    expect(parse("key=newapi&tab=models")).toEqual({ kind: "edit", key: "newapi", tab: "models" });
    expect(parse("key=newapi&tab=xxx")).toEqual({ kind: "edit", key: "newapi", tab: "config" });
  });

  test("旧链接 ?edit=<key> 打开配置页签", () => {
    expect(
      parseChannelRoute(params("edit=yswg"), channels, { canWrite: true, ready: true }).target,
    ).toEqual({ kind: "edit", key: "yswg", tab: "config" });
  });

  test("?new=1 与旧链接 ?edit=new 都是新建，可预选插件", () => {
    const parse = (query: string) =>
      parseChannelRoute(params(query), channels, { canWrite: true, ready: true }).target;
    expect(parse("new=1")).toEqual({ kind: "new", pluginKey: undefined });
    expect(parse("edit=new&plugin=newapi")).toEqual({ kind: "new", pluginKey: "newapi" });
  });

  test("没有写权限不能新建", () => {
    expect(
      parseChannelRoute(params("new=1"), channels, { canWrite: false, ready: true }).target,
    ).toBeNull();
  });

  test("渠道不存在：不开弹窗并报告缺失的 key；清单没加载完时先不报", () => {
    expect(
      parseChannelRoute(params("key=gone"), channels, { canWrite: true, ready: true }),
    ).toEqual({ target: null, missing: "gone" });
    expect(
      parseChannelRoute(params("key=gone"), channels, { canWrite: true, ready: false }),
    ).toEqual({ target: null, missing: null });
  });
});

describe("parsePluginRoute", () => {
  test("没有参数：不开弹窗", () => {
    expect(parsePluginRoute(params(""), plugins, true)).toEqual({ target: null, missing: null });
  });

  test("?key=<key> 打开概览，?tab=versions 打开版本历史", () => {
    expect(parsePluginRoute(params("key=newapi"), plugins, true).target).toEqual({
      key: "newapi",
      tab: "overview",
    });
    expect(parsePluginRoute(params("key=newapi&tab=versions"), plugins, true).target).toEqual({
      key: "newapi",
      tab: "versions",
    });
    expect(parsePluginRoute(params("key=newapi&tab=xxx"), plugins, true).target).toEqual({
      key: "newapi",
      tab: "overview",
    });
  });

  test("插件不存在：报告缺失；清单没加载完时先不报", () => {
    expect(parsePluginRoute(params("key=gone"), plugins, true)).toEqual({
      target: null,
      missing: "gone",
    });
    expect(parsePluginRoute(params("key=gone"), plugins, false)).toEqual({
      target: null,
      missing: null,
    });
  });
});

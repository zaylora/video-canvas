import { describe, expect, test } from "bun:test";

import { resolveChannelDialog } from "@/utils/admin/channel-route";

const channels = [{ key: "newapi" }, { key: "yswg" }];
const ready = { canWrite: true, ready: true };

describe("resolveChannelDialog", () => {
  test("没有要打开的弹窗：不开", () => {
    expect(resolveChannelDialog(null, channels, ready)).toEqual({ target: null, missing: null });
  });

  test("打开已有渠道，默认停在配置页签", () => {
    expect(resolveChannelDialog({ kind: "edit", key: "newapi" }, channels, ready).target).toEqual({
      kind: "edit",
      key: "newapi",
      tab: "config",
    });
  });

  test("可以指定页签", () => {
    expect(
      resolveChannelDialog({ kind: "edit", key: "newapi", tab: "models" }, channels, ready).target,
    ).toEqual({ kind: "edit", key: "newapi", tab: "models" });
  });

  test("新建可预选插件", () => {
    expect(
      resolveChannelDialog({ kind: "new", pluginKey: "newapi" }, channels, ready).target,
    ).toEqual({ kind: "new", pluginKey: "newapi" });
    expect(resolveChannelDialog({ kind: "new" }, channels, ready).target).toEqual({
      kind: "new",
      pluginKey: undefined,
    });
  });

  test("没有写权限不能新建", () => {
    expect(
      resolveChannelDialog({ kind: "new" }, channels, { canWrite: false, ready: true }).target,
    ).toBeNull();
  });

  test("渠道不存在：不开弹窗并报告缺失的 key；清单没加载完时先不报", () => {
    expect(resolveChannelDialog({ kind: "edit", key: "gone" }, channels, ready)).toEqual({
      target: null,
      missing: "gone",
    });
    expect(
      resolveChannelDialog({ kind: "edit", key: "gone" }, channels, {
        canWrite: true,
        ready: false,
      }),
    ).toEqual({ target: null, missing: null });
  });
});

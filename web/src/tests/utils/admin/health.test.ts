import { describe, expect, test } from "bun:test";

import type {
  ChannelView,
  ConfigListItem,
  PluginMeta,
  PluginVersionView,
  PluginView,
} from "@/api/admin-ai/type";
import { adminTodos, channelHealth, modelHealth } from "@/utils/admin/health";

const bearer: PluginMeta = { auth: { type: "bearer" }, endpoints: { video: { mode: "async" } } };

const version = (id: number, ver: string, meta: PluginMeta | null = bearer): PluginVersionView => ({
  id,
  plugin_key: "p",
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
  source: "uploaded",
  enabled: true,
  updated_at: "",
  versions: [version(1, "1.0.0")],
  ...extra,
});

const channel = (extra: Partial<ChannelView> = {}): ChannelView => ({
  key: "c1",
  name: "主线",
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

const model = (extra: Partial<ConfigListItem> = {}): ConfigListItem => ({
  key: "m1",
  channel: "c1",
  enabled: true,
  published_revision_id: 1,
  published_revision_no: 1,
  draft_revision_no: null,
  has_unpublished_draft: false,
  updated_at: "",
  ...extra,
});

describe("channelHealth", () => {
  test("渠道停用、插件不存在、插件停用、缺 Key、正常", () => {
    expect(channelHealth(channel({ enabled: false }), [plugin()]).tone).toBe("off");
    expect(channelHealth(channel(), []).reason).toContain("不存在");
    expect(channelHealth(channel(), [plugin({ enabled: false })]).reason).toContain("已停用");
    expect(channelHealth(channel({ secret_set: false }), [plugin()]).label).toBe("未设 Key");
    expect(channelHealth(channel(), [plugin()])).toEqual({
      tone: "ok",
      label: "可用",
      reason: null,
    });
  });

  test("插件不需要鉴权，或读不到插件版本时，不断言缺 Key", () => {
    const none = plugin({ versions: [version(1, "1.0.0", { auth: { type: "none" } })] });
    expect(channelHealth(channel({ secret_set: false }), [none]).tone).toBe("ok");
    const unknown = plugin({ versions: [] });
    expect(channelHealth(channel({ secret_set: false }), [unknown]).tone).toBe("ok");
  });
});

describe("modelHealth", () => {
  test("插件停用会传到已上架的模型上：显示不可用", () => {
    const health = modelHealth(model(), [channel()], [plugin({ enabled: false })]);
    expect(health.status).toBe("broken");
    expect(health.reason).toBe("渠道「主线」插件「插件P」已停用");
  });

  test("未上线 / 已下线优先，但仍带上渠道问题", () => {
    const missingKey = [channel({ secret_set: false })];
    expect(modelHealth(model({ published_revision_no: null }), missingKey, [plugin()])).toEqual({
      status: "unpublished",
      reason: "渠道「主线」还没有设置 Key",
    });
    expect(modelHealth(model({ enabled: false }), [channel()], [plugin()]).status).toBe("offline");
  });

  test("渠道不存在、没选渠道、清单未加载", () => {
    expect(modelHealth(model({ channel: "x" }), [], [plugin()]).reason).toBe("渠道 x 不存在");
    expect(modelHealth(model({ channel: undefined }), [], []).status).toBe("broken");
    expect(modelHealth(model(), [], [], false)).toEqual({ status: "online", reason: null });
  });

  test("一切正常为在线", () => {
    expect(modelHealth(model(), [channel()], [plugin()])).toEqual({
      status: "online",
      reason: null,
    });
  });
});

describe("adminTodos", () => {
  test("按严重度排序：插件停用 > 缺 Key > 可升级 > 未上线的修改", () => {
    const plugins = [
      plugin({ versions: [version(2, "1.1.0"), version(1, "1.0.0")] }),
      plugin({ key: "q", name: "插件Q", enabled: false }),
    ];
    const channels = [
      channel({ secret_set: false }),
      channel({ key: "c2", name: "Q线", plugin_key: "q" }),
    ];
    const models = [model({ has_unpublished_draft: true }), model({ key: "m2", channel: "c2" })];
    const todos = adminTodos(models, channels, plugins);
    expect(todos.map((t) => t.id)).toEqual(["plugin-off:q", "key:c1", "upgrade:p", "drafts"]);
    expect(todos[2].text).toBe("插件P v1.1.0 可用，1 个渠道还在旧版本");
    expect(todos[0].text).toBe("插件「插件Q」已停用，1 个渠道、1 个上架模型实际不可用");
    expect(todos[1].action).toEqual({ kind: "set-key", channelKey: "c1" });
  });

  test("停用的渠道下还有上架模型才提示；全部正常时为空", () => {
    expect(adminTodos([model()], [channel({ enabled: false })], [plugin()])[0].id).toBe(
      "channel-off:c1",
    );
    expect(
      adminTodos([model({ enabled: false })], [channel({ enabled: false })], [plugin()]),
    ).toEqual([]);
    expect(adminTodos([model()], [channel()], [plugin()])).toEqual([]);
  });
});

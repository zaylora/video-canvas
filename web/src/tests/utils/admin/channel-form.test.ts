import { describe, expect, test } from "bun:test";

import type { ChannelView, PluginView } from "@/api/admin-ai/type";
import {
  buildChannelRequest,
  channelFormFromView,
  checkBaseUrl,
  CHANNEL_KEY_PATTERN,
  emptyChannelForm,
  rebaseSettings,
  type ChannelFormState,
} from "@/utils/admin/channel-form";
import { settingFields } from "@/utils/admin/settings-form";

const fields = settingFields({
  region: { type: "enum", label: "区域", options: ["cn", "global"], default: "cn", required: true },
  ttl: { type: "number", label: "有效期" },
});

const view = (extra: Partial<ChannelView> = {}): ChannelView => ({
  key: "kling-direct",
  name: "可灵",
  plugin_key: "kling",
  plugin_version_id: 2,
  plugin_version: "0.2.0",
  base_url: "https://api.klingai.com",
  trusted_internal: false,
  allow_credentials: true,
  settings: { region: "cn" },
  rate_limit: { rps: 5, max_concurrency: 0 },
  enabled: true,
  secret_set: true,
  updated_by: 1,
  updated_at: "",
  created_at: "",
  ...extra,
});

const newForm = (extra: Partial<ChannelFormState> = {}): ChannelFormState => ({
  ...emptyChannelForm([]),
  key: "my-channel",
  name: "我的渠道",
  pluginKey: "kling",
  pluginVersion: "0.2.0",
  baseUrl: "https://api.example.com",
  settings: { region: "cn", ttl: "" },
  ...extra,
});

describe("emptyChannelForm", () => {
  test("默认选第一个启用且有版本的插件的第一个版本", () => {
    const plugins = [
      { key: "off", enabled: false, versions: [{ version: "1.0.0" }] },
      { key: "empty", enabled: true, versions: [] },
      { key: "ok", enabled: true, versions: [{ version: "2.0.0" }, { version: "1.0.0" }] },
    ] as unknown as PluginView[];
    const form = emptyChannelForm(plugins);
    expect(form.pluginKey).toBe("ok");
    expect(form.pluginVersion).toBe("2.0.0");
    expect(form.enabled).toBe(true);
    expect(form.trustedInternal).toBe(false);
  });

  test("没有可用插件时插件为空", () => {
    expect(emptyChannelForm([]).pluginKey).toBe("");
  });
});

describe("channelFormFromView", () => {
  test("渠道视图 → 表单：限流为 0 时输入框留空，设置项按声明填充", () => {
    const form = channelFormFromView(view(), fields);
    expect(form.key).toBe("kling-direct");
    expect(form.rps).toBe("5");
    expect(form.maxConcurrency).toBe("");
    expect(form.settings.region).toBe("cn");
    expect(form.allowCredentials).toBe(true);
  });
});

describe("rebaseSettings", () => {
  test("换版本后同名字段的取值带过去，新字段用默认值，消失的字段丢弃", () => {
    const oldFields = settingFields({
      region: { type: "enum", label: "区域", options: ["cn", "global"], required: true },
      old: { type: "string", label: "旧项" },
    });
    const newFields = settingFields({
      region: { type: "enum", label: "区域", options: ["cn", "global"], required: true },
      ttl: { type: "number", label: "有效期", default: 1800 },
    });
    const next = rebaseSettings(oldFields, { region: "global", old: "x" }, newFields);
    expect(next).toEqual({ region: "global", ttl: "1800" });
  });

  test("带过去的取值在新声明里不合法时退回默认值", () => {
    const oldFields = settingFields({
      region: { type: "enum", label: "r", options: ["cn", "eu"] },
    });
    const newFields = settingFields({
      region: { type: "enum", label: "r", options: ["cn"], default: "cn" },
    });
    expect(rebaseSettings(oldFields, { region: "eu" }, newFields).region).toBe("cn");
  });
});

describe("checkBaseUrl / CHANNEL_KEY_PATTERN", () => {
  test("合法地址通过", () => {
    expect(checkBaseUrl("https://gw.example.com")).toBeNull();
    expect(checkBaseUrl("http://10.0.3.12:3000/v1")).toBeNull();
  });

  test("空值、非 http、带用户名密码、不合法都被拒绝", () => {
    expect(checkBaseUrl("  ")).toContain("请填写");
    expect(checkBaseUrl("ftp://a.com")).toContain("http");
    expect(checkBaseUrl("https://user:pw@a.com")).toContain("用户名");
    expect(checkBaseUrl("not a url")).toContain("合法");
  });

  test("渠道 key 只能是小写字母数字连字符且以字母数字开头", () => {
    expect(CHANNEL_KEY_PATTERN.test("newapi-main")).toBe(true);
    expect(CHANNEL_KEY_PATTERN.test("-bad")).toBe(false);
    expect(CHANNEL_KEY_PATTERN.test("Bad")).toBe(false);
    expect(CHANNEL_KEY_PATTERN.test("a".repeat(65))).toBe(false);
  });
});

describe("buildChannelRequest（新建）", () => {
  test("合法表单生成 create 请求，限流空值为 0，设置项转成对应类型", () => {
    const result = buildChannelRequest(
      newForm({ rps: "3", settings: { region: "global", ttl: "60" } }),
      fields,
    );
    expect(result.ok).toBe(true);
    if (!result.ok) return;
    expect(result.create).toMatchObject({
      key: "my-channel",
      plugin_key: "kling",
      plugin_version: "0.2.0",
      base_url: "https://api.example.com",
      rate_limit: { rps: 3, max_concurrency: 0, max_running: 0 },
      settings: { region: "global", ttl: 60 },
    });
  });

  test("字段错误按键返回：key、名称、地址、限流、设置项", () => {
    const result = buildChannelRequest(
      newForm({
        key: "Bad Key",
        name: " ",
        baseUrl: "x",
        rps: "-1",
        maxConcurrency: "1.5",
        maxRunning: "-2",
        settings: { region: "", ttl: "" },
      }),
      fields,
    );
    expect(result.ok).toBe(false);
    if (result.ok) return;
    expect(Object.keys(result.errors).sort()).toEqual(
      ["baseUrl", "key", "maxConcurrency", "maxRunning", "name", "rps", "settings.region"].sort(),
    );
  });

  test("没选插件版本时报 plugin 错误", () => {
    const result = buildChannelRequest(newForm({ pluginKey: "", pluginVersion: "" }), fields);
    expect(result.ok).toBe(false);
    if (!result.ok) expect(result.errors.plugin).toBeDefined();
  });
});

describe("buildChannelRequest（编辑）", () => {
  const original = view();

  test("没改动时 changed 为 false，update 为空", () => {
    const form = channelFormFromView(original, fields);
    const result = buildChannelRequest(form, fields, original);
    expect(result.ok).toBe(true);
    if (!result.ok) return;
    expect(result.changed).toBe(false);
    expect(result.update).toEqual({});
  });

  test("只带改过的字段；改版本时同时带 plugin_key 与 plugin_version；key 不可改", () => {
    const form = {
      ...channelFormFromView(original, fields),
      key: "ignored",
      name: "改名",
      pluginVersion: "0.3.0",
      trustedInternal: true,
      rps: "10",
    };
    const result = buildChannelRequest(form, fields, original);
    expect(result.ok).toBe(true);
    if (!result.ok) return;
    expect(result.update).toEqual({
      name: "改名",
      plugin_key: "kling",
      plugin_version: "0.3.0",
      trusted_internal: true,
      rate_limit: { rps: 10, max_concurrency: 0, max_running: 0 },
    });
    expect(result.create.key).toBe("kling-direct");
  });

  test("最大同时生成数：回填、修改后进 rate_limit，留空等于不限", () => {
    const withRunning = view({ rate_limit: { rps: 5, max_concurrency: 0, max_running: 2 } });
    const form = channelFormFromView(withRunning, fields);
    expect(form.maxRunning).toBe("2");
    const changed = buildChannelRequest({ ...form, maxRunning: "4" }, fields, withRunning);
    expect(changed.ok && changed.update.rate_limit).toEqual({
      rps: 5,
      max_concurrency: 0,
      max_running: 4,
    });
    const cleared = buildChannelRequest({ ...form, maxRunning: "" }, fields, withRunning);
    expect(cleared.ok && cleared.update.rate_limit?.max_running).toBe(0);
    const same = buildChannelRequest(form, fields, withRunning);
    expect(same.ok && "rate_limit" in same.update).toBe(false);
  });

  test("设置项变化会带上 settings，没变化不带", () => {
    const form = channelFormFromView(original, fields);
    const changed = buildChannelRequest(
      { ...form, settings: { ...form.settings, region: "global" } },
      fields,
      original,
    );
    expect(changed.ok && changed.update.settings).toEqual({ region: "global" });
    const same = buildChannelRequest(form, fields, original);
    expect(same.ok && "settings" in same.update).toBe(false);
  });
});

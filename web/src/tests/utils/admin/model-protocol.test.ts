import { describe, expect, test } from "bun:test";

import type { PluginMeta, PluginView } from "@/api/admin/ai/type";
import {
  changeProtocolVersion,
  filterProtocolOptions,
  normalizeProtocolSettings,
  protocolOptions,
  protocolVersions,
  readModelProtocol,
  selectProtocol,
  switchModelKind,
  withBatchProtocol,
  withModelKind,
  withModelProtocol,
} from "@/utils/admin/model-protocol";

/** 正文里 capabilities.ops */
const capsOps = (body: Record<string, unknown> | null | undefined) =>
  ((body?.capabilities ?? {}) as { ops?: string[] }).ops;

/** 正文里 pricing.billing */
const billingOf = (body: Record<string, unknown> | null | undefined) =>
  ((body?.pricing ?? {}) as { billing?: string }).billing;

/** 取正文 channels[0]（测试里 withModelProtocol 返回的一定是对象） */
const firstChannel = (body: Record<string, unknown> | null) =>
  ((body?.channels ?? []) as Array<Record<string, unknown>>)[0];

const version = (plugin: string, ver: string, meta: PluginMeta, id = 1) => ({
  id,
  plugin_key: plugin,
  version: ver,
  sha256: "",
  created_at: "",
  created_by: 0,
  model_count: 0,
  meta,
});

const plugin = (
  key: string,
  name: string,
  enabled: boolean,
  versions: PluginView["versions"],
): PluginView => ({ key, name, source: "builtin", enabled, updated_at: "", versions });

const plugins: PluginView[] = [
  plugin("openai", "OpenAI 兼容", true, [
    // 新到旧
    version(
      "openai",
      "1.1.0",
      {
        vendor: "openai",
        description: "对话与图片",
        auth: { type: "bearer" },
        endpoints: {
          text: { mode: "sync", method: "POST", path: "/v1/chat/completions" },
          image: { mode: "sync", method: "POST", path: "/v1/images/generations" },
        },
        channelSettings: {
          api: {
            type: "enum",
            label: "接口",
            options: ["chat_completions", "responses"],
            default: "responses",
          },
          region: { type: "string", label: "区域", required: true },
        },
      },
      2,
    ),
    version(
      "openai",
      "1.0.0",
      {
        vendor: "openai",
        auth: { type: "bearer" },
        endpoints: { text: { mode: "sync", method: "POST", path: "/v1/chat/completions" } },
        channelSettings: {
          api: {
            type: "enum",
            label: "接口",
            options: ["chat_completions", "responses"],
            default: "chat_completions",
          },
          legacy: { type: "boolean", label: "旧行为" },
        },
      },
      1,
    ),
  ]),
  plugin("anthropic", "Anthropic Messages", true, [
    version(
      "anthropic",
      "1.0.0",
      {
        vendor: "anthropic",
        auth: { type: "header", name: "x-api-key" },
        endpoints: { text: { mode: "sync", method: "POST", path: "/v1/messages" } },
      },
      3,
    ),
  ]),
  plugin("vidu", "生数 Vidu", false, [
    version(
      "vidu",
      "1.0.0",
      {
        vendor: "shengshu",
        auth: { type: "custom" },
        endpoints: { video: { mode: "async", path: "/ent/v2/text2video" } },
      },
      4,
    ),
  ]),
  plugin("jimeng", "即梦", true, [
    version(
      "jimeng",
      "1.0.0",
      {
        vendor: "volcengine",
        auth: { type: "custom" },
        endpoints: { video: { mode: "async", method: "POST", path: "/?Action=Submit" } },
      },
      5,
    ),
  ]),
];

describe("读写 channels[0].protocol", () => {
  test("读出插件、版本与设置；缺失或不合法返回 null", () => {
    const body = {
      channels: [
        { channel: "a", upstream_model: "m", protocol: { plugin: "p", version: "1.0.0" } },
      ],
    };
    expect(readModelProtocol(body)).toEqual({ plugin: "p", version: "1.0.0", settings: {} });
    expect(
      readModelProtocol({
        channels: [{ protocol: { plugin: "p", version: "1.0.0", settings: { a: 1 } } }],
      })?.settings,
    ).toEqual({ a: 1 });
    expect(readModelProtocol({ channels: [{ channel: "a" }] })).toBeNull();
    expect(readModelProtocol({ channels: [{ protocol: { plugin: 1 } }] })).toBeNull();
    expect(readModelProtocol({})).toBeNull();
    expect(readModelProtocol(null)).toBeNull();
  });

  test("写入保留其他字段与键顺序；传 null 删除这一项", () => {
    const body = { key: "m", channels: [{ channel: "a", upstream_model: "u" }], pricing: {} };
    const next = withModelProtocol(body, { plugin: "p", version: "1.0.0", settings: { x: 1 } });
    expect(Object.keys(next ?? {})).toEqual(["key", "channels", "pricing"]);
    expect(firstChannel(next)).toEqual({
      channel: "a",
      upstream_model: "u",
      protocol: { plugin: "p", version: "1.0.0", settings: { x: 1 } },
    });
    const cleared = withModelProtocol(next, null);
    expect(firstChannel(cleared)).toEqual({
      channel: "a",
      upstream_model: "u",
    });
    expect(withModelProtocol("坏数据", null)).toBeNull();
  });

  test("正文没有 channels 时写入会补一项，清除则不动", () => {
    const next = withModelProtocol({ key: "m" }, { plugin: "p", version: "1.0.0", settings: {} });
    expect(next?.channels).toHaveLength(1);
    expect(withModelProtocol({ key: "m" }, null)).toEqual({ key: "m" });
  });
});

describe("protocolOptions：按能力列出可选协议", () => {
  test("只列最新版本声明了该能力的插件，带请求方法、路径、厂商、同步 / 异步", () => {
    const list = protocolOptions(plugins, "text");
    expect(list.map((item) => item.key)).toEqual(["openai", "anthropic"]);
    expect(list[0]).toMatchObject({
      name: "OpenAI 兼容",
      vendor: "openai",
      latestVersion: "1.1.0",
      method: "POST",
      path: "/v1/chat/completions",
      mode: "sync",
      authType: "bearer",
      description: "对话与图片",
      enabled: true,
      disabledReason: null,
    });
    expect(protocolOptions(plugins, "image").map((item) => item.key)).toEqual(["openai"]);
  });

  test("method 缺省按 POST 显示；已停用的插件也列出，置灰并排在最后", () => {
    const list = protocolOptions(plugins, "video");
    expect(list.map((item) => item.key)).toEqual(["jimeng", "vidu"]);
    const vidu = list[1];
    expect(vidu.method).toBe("POST");
    expect(vidu.enabled).toBe(false);
    expect(vidu.disabledReason).toBe("插件已停用，不可选");
  });

  test("Agent 与没选能力不需要协议，返回空", () => {
    expect(protocolOptions(plugins, "agent")).toEqual([]);
    expect(protocolOptions(plugins, "")).toEqual([]);
  });

  test("没有版本或 meta 损坏的插件不列出", () => {
    const broken = [
      plugin("a", "A", true, []),
      plugin("b", "B", true, [version("b", "1.0.0", {})]),
    ];
    expect(protocolOptions(broken, "text")).toEqual([]);
  });
});

describe("filterProtocolOptions：搜索协议名称、厂商、请求路径", () => {
  const options = protocolOptions(plugins, "text");

  test("空搜索词返回全部", () => {
    expect(filterProtocolOptions(options, "  ")).toHaveLength(2);
  });

  test("按名称、厂商、路径匹配，忽略大小写", () => {
    expect(filterProtocolOptions(options, "anthropic").map((item) => item.key)).toEqual([
      "anthropic",
    ]);
    expect(filterProtocolOptions(options, "MESSAGES").map((item) => item.key)).toEqual([
      "anthropic",
    ]);
    expect(filterProtocolOptions(options, "兼容").map((item) => item.key)).toEqual(["openai"]);
    expect(filterProtocolOptions(options, "post /v1/chat").map((item) => item.key)).toEqual([
      "openai",
    ]);
    expect(filterProtocolOptions(options, "不存在")).toEqual([]);
  });
});

describe("protocolVersions", () => {
  test("只列支持该能力的版本，新到旧；当前固定的版本即使不支持也保留", () => {
    const openai = plugins[0];
    expect(protocolVersions(openai, "text")).toEqual(["1.1.0", "1.0.0"]);
    expect(protocolVersions(openai, "image")).toEqual(["1.1.0"]);
    expect(protocolVersions(openai, "image", "1.0.0")).toEqual(["1.1.0", "1.0.0"]);
    expect(protocolVersions(undefined, "text")).toEqual([]);
  });
});

describe("normalizeProtocolSettings", () => {
  const fields = [
    { type: "enum" as const, label: "接口", options: ["a", "b"], default: "a" },
    { type: "boolean" as const, label: "开关" },
    { type: "string" as const, label: "区域" },
  ];
  const schema = { api: fields[0], flag: fields[1], region: fields[2] };

  test("只保留声明过的项，缺的补默认值，没有默认值的可选项不出现", () => {
    expect(normalizeProtocolSettings(schema, { api: "b", extra: 1 })).toEqual({
      api: "b",
      flag: false,
    });
    expect(normalizeProtocolSettings(schema, undefined)).toEqual({ api: "a", flag: false });
    expect(normalizeProtocolSettings(null, { a: 1 })).toEqual({});
  });

  test("取值放不进声明的类型时回到默认值", () => {
    expect(normalizeProtocolSettings(schema, { api: "zzz" }).api).toBe("a");
  });
});

describe("selectProtocol：选中协议", () => {
  test("固定最新版本，设置项填默认值", () => {
    expect(selectProtocol(plugins, "openai", "text")).toEqual({
      plugin: "openai",
      version: "1.1.0",
      settings: { api: "responses" },
    });
  });

  test("插件不存在、已停用、最新版本不支持该能力时返回 null", () => {
    expect(selectProtocol(plugins, "ghost", "text")).toBeNull();
    expect(selectProtocol(plugins, "vidu", "video")).toBeNull();
    expect(selectProtocol(plugins, "anthropic", "video")).toBeNull();
  });
});

describe("changeProtocolVersion：切换版本", () => {
  test("按新版本的声明重新整理设置项：沿用同名项，丢掉不再声明的，补新项默认值", () => {
    const old = {
      plugin: "openai",
      version: "1.0.0",
      settings: { api: "chat_completions", legacy: true },
    };
    const result = changeProtocolVersion(plugins, old, "1.1.0");
    expect(result.protocol).toEqual({
      plugin: "openai",
      version: "1.1.0",
      settings: { api: "chat_completions" },
    });
    expect(result.dropped).toEqual(["legacy"]);
  });

  test("没有丢弃项时 dropped 为空；版本不存在时原样返回", () => {
    const old = { plugin: "anthropic", version: "1.0.0", settings: {} };
    expect(changeProtocolVersion(plugins, old, "1.0.0")).toEqual({ protocol: old, dropped: [] });
    expect(changeProtocolVersion(plugins, old, "9.9.9")).toEqual({ protocol: old, dropped: [] });
  });
});

describe("switchModelKind：切换能力", () => {
  const openai = { plugin: "openai", version: "1.1.0", settings: { api: "responses" } };

  test("已选协议支持新能力就保留", () => {
    expect(switchModelKind(plugins, openai, "image")).toEqual({ protocol: openai, notice: null });
  });

  test("已选协议不支持新能力：清空并提示", () => {
    const result = switchModelKind(plugins, { ...openai, version: "1.0.0" }, "image");
    expect(result.protocol).toBeNull();
    expect(result.notice).toBe("已选协议不支持图片，请重新选择");
    expect(switchModelKind(plugins, openai, "video").notice).toBe("已选协议不支持视频，请重新选择");
  });

  test("插件已不存在也算不支持", () => {
    const ghost = { plugin: "ghost", version: "1.0.0", settings: {} };
    expect(switchModelKind(plugins, ghost, "text").protocol).toBeNull();
  });

  test("切到 Agent：协议必须清空，有协议时给提示", () => {
    const result = switchModelKind(plugins, openai, "agent");
    expect(result.protocol).toBeNull();
    expect(result.notice).toContain("Agent");
    expect(switchModelKind(plugins, null, "agent")).toEqual({ protocol: null, notice: null });
  });

  test("没有已选协议时不提示", () => {
    expect(switchModelKind(plugins, null, "video")).toEqual({ protocol: null, notice: null });
  });
});

describe("withModelKind：改正文里的能力", () => {
  const body = {
    key: "m",
    kind: "text",
    label: "L",
    channels: [
      {
        channel: "a",
        upstream_model: "u",
        protocol: { plugin: "openai", version: "1.0.0", settings: { api: "chat_completions" } },
      },
    ],
    capabilities: { custom: true },
    pricing: { billing: "per_call", unit: 99 },
  };

  test("能力与定价重置成新种类的默认值，补上默认时限", () => {
    const result = withModelKind(body, "video", plugins);
    expect(result?.body.kind).toBe("video");
    expect(capsOps(result?.body)).toContain("t2v");
    expect(billingOf(result?.body)).toBe("per_second");
    expect(result?.body.deadline).toBe("30m");
  });

  test("已有时限不覆盖；协议不支持时清空并带提示", () => {
    const result = withModelKind({ ...body, deadline: "10m" }, "video", plugins);
    expect(result?.body.deadline).toBe("10m");
    expect(readModelProtocol(result?.body)).toBeNull();
    expect(result?.notice).toBe("已选协议不支持视频，请重新选择");
  });

  test("支持时保留协议", () => {
    const result = withModelKind(
      {
        ...body,
        channels: [{ ...body.channels[0], protocol: { plugin: "openai", version: "1.1.0" } }],
      },
      "image",
      plugins,
    );
    expect(readModelProtocol(result?.body)?.plugin).toBe("openai");
    expect(result?.notice).toBeNull();
  });

  test("正文不是对象返回 null", () => {
    expect(withModelKind(null, "text", plugins)).toBeNull();
  });
});

describe("withBatchProtocol：批量设置能力与协议", () => {
  const protocol = { plugin: "openai", version: "1.1.0", settings: { api: "responses" } };
  const priced = {
    key: "m",
    kind: "text",
    label: "L",
    channels: [{ channel: "a", upstream_model: "u" }],
    capabilities: { custom: true },
    pricing: { billing: "per_call", unit: 99 },
    deadline: "10m",
  };

  test("能力没变：只换协议，能力与价格原样保留", () => {
    const next = withBatchProtocol(priced, "text", protocol);
    expect(readModelProtocol(next)).toEqual(protocol);
    expect(next?.capabilities).toEqual({ custom: true });
    expect(next?.pricing).toEqual({ billing: "per_call", unit: 99 });
  });

  test("待配置的模型第一次选能力：补能力、价格与时限的默认值", () => {
    const pending = {
      key: "m",
      kind: "",
      label: "L",
      channels: [{ channel: "a", upstream_model: "u" }],
    };
    const next = withBatchProtocol(pending, "image", protocol);
    expect(next?.kind).toBe("image");
    expect(capsOps(next)).toContain("t2i");
    expect(billingOf(next)).toBe("per_call");
    expect(next?.deadline).toBe("10m");
    expect(readModelProtocol(next)).toEqual(protocol);
  });

  test("能力变了但已有价格：重置能力，价格保留", () => {
    const next = withBatchProtocol(priced, "image", protocol);
    expect(capsOps(next)).toContain("t2i");
    expect(next?.pricing).toEqual({ billing: "per_call", unit: 99 });
    expect(next?.deadline).toBe("10m");
  });

  test("Agent 不带协议，已有的会被清掉；正文不是对象返回 null", () => {
    const withProto = withModelProtocol(priced, protocol);
    expect(readModelProtocol(withBatchProtocol(withProto, "agent", protocol))).toBeNull();
    expect(withBatchProtocol(null, "text", protocol)).toBeNull();
  });
});

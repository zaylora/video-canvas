import { describe, expect, test } from "bun:test";

import type {
  ConfigListItem,
  PluginMeta,
  PluginVersionView,
  PluginView,
} from "@/api/admin/ai/type";
import {
  availableUpgrade,
  checkPluginFile,
  compareSemver,
  describeAuth,
  describeVersionAdjusted,
  endpointLabel,
  findPluginVersion,
  isSameCodeRejection,
  latestVersion,
  metaSummary,
  metaSupportsKind,
  normalizeIssues,
  outdatedModelCount,
  outdatedModels,
  pluginModelCount,
  settingRows,
  shortSha,
  summarizeUpgrade,
  summarizeUpload,
  vendorOf,
  versionDeleteBlock,
} from "@/utils/admin/plugin";

const version = (
  id: number,
  ver: string,
  meta: PluginMeta | null,
  modelCount = 0,
): PluginVersionView => ({
  id,
  plugin_key: "p",
  version: ver,
  sha256: "abcdef1234567890",
  created_at: "",
  created_by: 0,
  model_count: modelCount,
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

  test("鉴权说明覆盖各种方式；custom 说明读取渠道的 API Key 与 Secret Key 自行签名", () => {
    expect(describeAuth(null)).toBe("无需鉴权");
    expect(describeAuth({ type: "bearer" })).toContain("Bearer");
    expect(describeAuth({ type: "header", name: "X-Key" })).toContain("X-Key");
    const custom = describeAuth({ type: "custom" });
    expect(custom).toContain("自行签名");
    expect(custom).toContain("Secret Key");
    expect(custom).not.toContain("允许插件读取");
    expect(describeAuth({ type: "weird" })).toBe("weird");
  });

  test("meta 缺失或字段为 null 时摘要不崩", () => {
    expect(metaSummary(null).endpoints).toEqual([]);
    expect(metaSummary(null).vendor).toBe("");
    const summary = metaSummary({
      vendor: "kling",
      auth: { type: "custom" },
      endpoints: {
        video: { mode: "async", method: "POST", path: "/v1/videos" },
        text: {},
      },
      channelSettings: { region: { type: "enum", label: "区域" } },
    });
    expect(summary.authType).toBe("custom");
    expect(summary.vendor).toBe("kling");
    expect(summary.endpoints).toEqual([
      { kind: "video", mode: "async", request: "POST /v1/videos" },
      { kind: "text", mode: "?", request: "—" },
    ]);
    expect(summary.settingNames).toEqual(["region"]);
  });

  test("摘要不再暴露已弃用的导入能力，也没有下载域名白名单", () => {
    const legacyMeta = {
      auth: { type: "bearer" },
      allowedHosts: ["cdn.example.com"],
      import: { args: {} },
    };
    expect(Object.keys(metaSummary(legacyMeta))).not.toContain("allowedHosts");
    expect(Object.keys(metaSummary(legacyMeta))).not.toContain("importable");
  });
});

describe("endpointLabel / vendorOf：协议列表里的展示字段", () => {
  test("有路径：方法加路径，方法缺省按 POST", () => {
    const meta: PluginMeta = {
      endpoints: {
        text: { mode: "sync", method: "GET", path: "/v1/models" },
        video: { mode: "async", path: "/v1/videos" },
      },
    };
    expect(endpointLabel(meta, "text")).toBe("GET /v1/models");
    expect(endpointLabel(meta, "video")).toBe("POST /v1/videos");
  });

  test("没有声明路径、没有这个能力或 meta 缺失：显示 —", () => {
    expect(endpointLabel({ endpoints: { text: { mode: "sync" } } }, "text")).toBe("—");
    expect(endpointLabel({ endpoints: {} }, "image")).toBe("—");
    expect(endpointLabel(null, "text")).toBe("—");
    expect(endpointLabel(undefined, "text")).toBe("—");
  });

  test("厂商：没有声明返回空串", () => {
    expect(vendorOf({ vendor: "openai" })).toBe("openai");
    expect(vendorOf({})).toBe("");
    expect(vendorOf(null)).toBe("");
  });
});

describe("settingRows：协议设置项声明的展示行", () => {
  test("按声明顺序给出名字、标题、类型、必填、默认值与可选值", () => {
    const rows = settingRows({
      channelSettings: {
        region: {
          type: "enum",
          label: "区域",
          required: true,
          options: ["cn", "us"],
          default: "cn",
        },
        retries: { type: "number", label: "重试次数", default: 3, description: "失败后重试" },
        sandbox: { type: "boolean", label: "沙箱", default: false },
        tag: { type: "string", label: "标签" },
        odd: { type: "weird", label: "怪" },
      },
    });
    expect(rows).toEqual([
      {
        name: "region",
        label: "区域",
        typeLabel: "选项",
        required: true,
        defaultText: "cn",
        options: ["cn", "us"],
        description: "",
      },
      {
        name: "retries",
        label: "重试次数",
        typeLabel: "数字",
        required: false,
        defaultText: "3",
        options: [],
        description: "失败后重试",
      },
      {
        name: "sandbox",
        label: "沙箱",
        typeLabel: "开关",
        required: false,
        defaultText: "false",
        options: [],
        description: "",
      },
      {
        name: "tag",
        label: "标签",
        typeLabel: "文本",
        required: false,
        defaultText: "",
        options: [],
        description: "",
      },
      {
        name: "odd",
        label: "怪",
        typeLabel: "weird",
        required: false,
        defaultText: "",
        options: [],
        description: "",
      },
    ]);
  });

  test("没有声明或 meta 缺失：空数组；缺标题时退回名字", () => {
    expect(settingRows(null)).toEqual([]);
    expect(settingRows({ channelSettings: null })).toEqual([]);
    expect(settingRows({ channelSettings: { k: { type: "string" } } } as PluginMeta)[0].label).toBe(
      "k",
    );
  });
});

describe("findPluginVersion", () => {
  const plugins = [plugin("p", [version(2, "2.0.0", textMeta), version(1, "1.0.0", videoMeta)])];

  test("先按版本 ID 找，找不到再按版本号", () => {
    expect(findPluginVersion(plugins, "p", { id: 1 })?.version).toBe("1.0.0");
    expect(findPluginVersion(plugins, "p", { id: 99, version: "2.0.0" })?.id).toBe(2);
    expect(findPluginVersion(plugins, "nope", { id: 1 })).toBeUndefined();
  });
});

describe("metaSupportsKind：Agent 不走插件，其余看 endpoints", () => {
  const bearer: PluginMeta = { auth: { type: "bearer" }, endpoints: { text: { mode: "sync" } } };

  test("agent 恒为 false，哪怕插件是 Bearer 鉴权", () => {
    expect(metaSupportsKind(bearer, "agent")).toBe(false);
    expect(metaSupportsKind({ endpoints: { agent: { mode: "sync" } } }, "agent")).toBe(false);
  });

  test("其它种类看 endpoints 里有没有声明", () => {
    expect(metaSupportsKind(bearer, "text")).toBe(true);
    expect(metaSupportsKind(bearer, "video")).toBe(false);
    expect(metaSupportsKind({ endpoints: null }, "text")).toBe(false);
    expect(metaSupportsKind({}, "text")).toBe(false);
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
    expect(availableUpgrade(plugins, { plugin_key: "p", plugin_version: "1.0.0" })?.version).toBe(
      "1.1.0",
    );
    expect(availableUpgrade(plugins, { plugin_key: "p", plugin_version: "1.1.0" })).toBeNull();
    expect(availableUpgrade(plugins, { plugin_key: "ghost", plugin_version: "1.0.0" })).toBeNull();
  });

  test("latestVersion 取 semver 最高的，不依赖数组顺序", () => {
    const shuffled = plugin("p", [
      version(1, "1.0.0", null),
      version(3, "2.0.0", null),
      version(2, "1.5.0", null),
    ]);
    expect(latestVersion(shuffled)?.version).toBe("2.0.0");
    expect(latestVersion(undefined)).toBeUndefined();
  });
});

describe("versionDeleteBlock", () => {
  test("内置版本与有模型固定的版本不能删，并写明原因", () => {
    expect(versionDeleteBlock({ source: "builtin" }, { model_count: 0 })).toContain("内置");
    const block = versionDeleteBlock({ source: "uploaded" }, { model_count: 2 });
    expect(block).toContain("2 个模型");
    expect(block).not.toContain("渠道");
    expect(versionDeleteBlock({ source: "uploaded" }, { model_count: 0 })).toBeNull();
  });
});

describe("pluginModelCount / outdatedModelCount", () => {
  test("在用模型数是各版本之和，没有版本是 0", () => {
    const versions = [version(1, "1.0.0", null, 2), version(2, "2.0.0", null, 1)];
    expect(pluginModelCount({ versions })).toBe(3);
    expect(pluginModelCount({ versions: [] })).toBe(0);
  });

  test("旧版模型数：固定在比最新版本低的版本上的模型", () => {
    const versions = [
      version(1, "1.0.0", null, 2),
      version(3, "1.10.0", null, 4),
      version(2, "1.9.0", null, 1),
    ];
    expect(outdatedModelCount({ versions })).toBe(3);
    expect(outdatedModelCount({ versions: [version(1, "1.0.0", null, 5)] })).toBe(0);
    expect(outdatedModelCount({ versions: [] })).toBe(0);
  });
});

describe("outdatedModels：升级模型的清单", () => {
  const target = plugin("p", [version(2, "1.1.0", videoMeta), version(1, "1.0.0", videoMeta)]);
  const model = (key: string, extra: Partial<ConfigListItem> = {}): ConfigListItem => ({
    key,
    plugin: "p",
    plugin_version: "1.0.0",
    updated_at: "",
    ...extra,
  });

  test("只取协议用了这个插件、且版本低于最新版本的模型", () => {
    const list = [
      model("old"),
      model("latest", { plugin_version: "1.1.0" }),
      model("other-plugin", { plugin: "q" }),
      model("pending", { plugin: "", plugin_version: "" }),
      model("agent", { plugin: undefined, plugin_version: undefined }),
    ];
    expect(outdatedModels(target, list).map((item) => item.key)).toEqual(["old"]);
  });

  test("插件没有版本或不存在：空清单", () => {
    expect(outdatedModels(plugin("p", []), [model("a")])).toEqual([]);
    expect(outdatedModels(undefined, [model("a")])).toEqual([]);
  });
});

describe("summarizeUpgrade：升级结果区分成功数与失败原因", () => {
  const names: Record<string, string> = { a: "模型 A", b: "模型 B" };
  const nameOf = (key: string) => names[key] ?? key;

  test("全部成功：没有失败项", () => {
    const summary = summarizeUpgrade(
      { to_version: "1.1.0", upgraded: ["a", "b"], failed: [] },
      nameOf,
    );
    expect(summary).toEqual({ toVersion: "1.1.0", upgraded: 2, failed: [] });
  });

  test("部分失败：失败项带展示名与原因，找不到名字退回 key", () => {
    const summary = summarizeUpgrade(
      {
        to_version: "1.1.0",
        upgraded: ["a"],
        failed: [
          { key: "b", reason: "必填项 region 缺失" },
          { key: "zzz", reason: "" },
        ],
      },
      nameOf,
    );
    expect(summary.upgraded).toBe(1);
    expect(summary.failed).toEqual([
      { key: "b", name: "模型 B", reason: "必填项 region 缺失" },
      { key: "zzz", name: "zzz", reason: "未知原因" },
    ]);
  });
});

describe("describeVersionAdjusted：上传时版本号被自动调整的提示", () => {
  test("代码里写了版本号：说明写的是哪个、登记成了哪个，并提醒模型不会自动切换", () => {
    const notice = describeVersionAdjusted({ from: "1.3.1", to: "1.3.2" });
    expect(notice?.title).toBe("代码里写的是 v1.3.1，已自动登记为 v1.3.2");
    expect(notice?.hint).toBe("模型不会自动切换版本，请到插件页“升级模型”里切过去。");
  });

  test("代码里没写或写得不合法（from 为空串）：提示没有合法的版本号", () => {
    const notice = describeVersionAdjusted({ from: "", to: "1.0.0" });
    expect(notice?.title).toBe("代码里没有写合法的版本号，已自动登记为 v1.0.0");
    expect(notice?.hint).toBe("模型不会自动切换版本，请到插件页“升级模型”里切过去。");
  });

  test("没有调整（字段缺失、null）或登记版本为空：不提示", () => {
    expect(describeVersionAdjusted(undefined)).toBeNull();
    expect(describeVersionAdjusted(null)).toBeNull();
    expect(describeVersionAdjusted({ from: "1.0.0", to: "" })).toBeNull();
  });
});

describe("isSameCodeRejection：上传的代码与已有版本完全相同", () => {
  const same = { path: "meta", message: "代码与 v1.3.1 完全相同，没有产生新版本" };

  test("只有这一条问题：是（这时发给 AI 没有意义，不该给“复制修复信息”）", () => {
    expect(isSameCodeRejection([same])).toBe(true);
  });

  test("路径也是 meta 的真问题（比如没导出 meta）：不是，仍然要能发给 AI", () => {
    expect(isSameCodeRejection([{ path: "meta", message: "插件必须导出 meta 对象" }])).toBe(false);
  });

  test("夹着别的问题、没有问题、路径不是 meta：都不是", () => {
    expect(isSameCodeRejection([same, { path: "meta.key", message: "缺少 key" }])).toBe(false);
    expect(isSameCodeRejection([])).toBe(false);
    expect(isSameCodeRejection([{ path: "meta.version", message: "完全相同" }])).toBe(false);
    expect(isSameCodeRejection(null)).toBe(false);
    expect(isSameCodeRejection(undefined)).toBe(false);
  });
});

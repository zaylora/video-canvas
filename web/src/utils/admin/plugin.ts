import type {
  ConfigIssue,
  PluginMeta,
  PluginUploadResult,
  PluginVersionAdjusted,
  PluginVersionView,
  PluginView,
  UpgradeModelsResult,
} from "@/api/admin/ai/type";

/** 插件文件大小上限（与后端 pluginmeta.MaxPluginBytes 一致） */
export const PLUGIN_MAX_BYTES = 512 * 1024;

/** 上传前的本地检查：扩展名 .js、非空、≤512KB；通过返回 null */
export function checkPluginFile(file: { name: string; size: number }): string | null {
  if (!/\.js$/i.test(file.name)) return "插件必须是 .js 文件";
  if (file.size <= 0) return "文件是空的";
  if (file.size > PLUGIN_MAX_BYTES) {
    return `文件 ${(file.size / 1024).toFixed(1)}KB，超过 512KB 上限`;
  }
  return null;
}

/**
 * 整理预检问题：丢掉坏条目、去重，按 path 排序让同一处的问题挨在一起，便于定位。
 * path 为空表示整个文件（如语法错误）。
 */
export function normalizeIssues(raw: unknown): ConfigIssue[] {
  if (!Array.isArray(raw)) return [];
  const seen = new Set<string>();
  const issues: ConfigIssue[] = [];
  for (const item of raw) {
    if (typeof item !== "object" || item === null) continue;
    const { path, message } = item as { path?: unknown; message?: unknown };
    const issue = {
      path: typeof path === "string" ? path.trim() : "",
      message: typeof message === "string" && message.trim() ? message.trim() : "未知问题",
    };
    const id = `${issue.path}\u0000${issue.message}`;
    if (seen.has(id)) continue;
    seen.add(id);
    issues.push(issue);
  }
  // 稳定排序：同一 path 保持后端给的先后
  return issues
    .map((issue, index) => ({ issue, index }))
    .sort((a, b) =>
      a.issue.path === b.issue.path ? a.index - b.index : a.issue.path < b.issue.path ? -1 : 1,
    )
    .map(({ issue }) => issue);
}

export type UploadSummary = {
  tone: "success" | "error";
  title: string;
  issues: ConfigIssue[];
};

/** 上传结果 → 给人看的一句话 + 问题清单 */
export function summarizeUpload(result: PluginUploadResult | null | undefined): UploadSummary {
  const issues = normalizeIssues(result?.issues);
  if (result?.accepted) {
    const version = result.version;
    const name = version ? `${version.plugin_key}@${version.version}` : "新版本";
    return { tone: "success", title: `预检通过，已登记 ${name}`, issues };
  }
  return {
    tone: "error",
    title: issues.length > 0 ? `预检未通过：${issues.length} 个问题` : "预检未通过",
    issues,
  };
}

/**
 * 这次预检未通过，是不是只因为“代码与已有版本完全相同”（后端把问题挂在 meta 上）。
 * 这时没有哪里要改，发给 AI 没有意义，不给“复制修复信息”。
 * 宁可漏判也不误判：每个问题都必须是 meta 路径且文案含“完全相同”；后端改了措辞这里返回 false，
 * 只是按钮重新出现，无害；误判则会把“没导出 meta”之类真问题的按钮藏掉。
 */
export function isSameCodeRejection(issues: ConfigIssue[] | null | undefined): boolean {
  return (
    !!issues?.length &&
    issues.every((issue) => issue.path === "meta" && issue.message.includes("完全相同"))
  );
}

/** 版本号被自动调整后给人看的提示：一句结果加一句后续操作 */
export type VersionAdjustedNotice = {
  /** 代码里写的是什么、实际登记成了什么 */
  title: string;
  /** 提醒模型要自己升级到新版本 */
  hint: string;
};

/**
 * 上传时版本号被自动调整（见 PluginUploadResult.version_adjusted）→ 提示文案。
 * from 为空串说明代码里没写或写得不合法。没有调整（字段缺失或为 null）、登记版本号为空时返回 null。
 * 模型的协议固定在旧版本上，登记新版本不会让模型自动切换，所以总要带上“去升级模型”的提醒。
 */
export function describeVersionAdjusted(
  adjusted: PluginVersionAdjusted | null | undefined,
): VersionAdjustedNotice | null {
  if (!adjusted?.to) return null;
  const title = adjusted.from
    ? `代码里写的是 v${adjusted.from}，已自动登记为 v${adjusted.to}`
    : `代码里没有写合法的版本号，已自动登记为 v${adjusted.to}`;
  return { title, hint: "模型不会自动切换版本，请到插件页“升级模型”里切过去。" };
}

/** sha256 前 8 位 */
export const shortSha = (sha: string | null | undefined) => (sha ?? "").slice(0, 8);

/** 鉴权方式的中文说明 */
export function describeAuth(auth: PluginMeta["auth"]): string {
  const type = auth?.type ?? "none";
  const name = auth?.name ? `（${auth.name}）` : "";
  switch (type) {
    case "bearer":
      return "Bearer（宿主注入 Authorization 头）";
    case "header":
      return `请求头${name}`;
    case "query":
      return `查询参数${name}`;
    case "custom":
      return "插件自行签名（该协议会读取渠道的 API Key 与 Secret Key 自行签名）";
    case "none":
      return "无需鉴权";
    default:
      return String(type);
  }
}

export type MetaSummary = {
  authType: string;
  auth: string;
  /** 厂商 slug；插件没声明为空串 */
  vendor: string;
  /** 每个能力：同步 / 异步，以及展示用的请求（“POST /v1/messages”，没声明为 —） */
  endpoints: Array<{ kind: string; mode: string; request: string }>;
  /** 协议设置项的名字（meta.channelSettings 的键，在模型里填） */
  settingNames: string[];
  description?: string;
};

/** 版本列表里展示的 meta 摘要；meta 缺失或字段为 null 都不崩 */
export function metaSummary(meta: PluginMeta | null | undefined): MetaSummary {
  const endpoints = meta?.endpoints && typeof meta.endpoints === "object" ? meta.endpoints : {};
  return {
    authType: meta?.auth?.type ?? "none",
    auth: describeAuth(meta?.auth),
    vendor: vendorOf(meta),
    endpoints: Object.entries(endpoints).map(([kind, value]) => ({
      kind,
      mode: value?.mode ?? "?",
      request: endpointLabel(meta, kind),
    })),
    settingNames:
      meta?.channelSettings && typeof meta.channelSettings === "object"
        ? Object.keys(meta.channelSettings)
        : [],
    description: meta?.description || undefined,
  };
}

/** 协议设置项声明的一行展示数据 */
export type SettingRow = {
  /** 设置项的名字（meta.channelSettings 的键） */
  name: string;
  /** 表单标题，缺失时退回名字 */
  label: string;
  /** 取值类型的中文名；未知类型原样 */
  typeLabel: string;
  /** 是否必填 */
  required: boolean;
  /** 默认值的文本；没有默认值为空串 */
  defaultText: string;
  /** enum 的可选值，其它类型为空数组 */
  options: string[];
  /** 补充说明，没有为空串 */
  description: string;
};

const SETTING_TYPE_LABEL: Record<string, string> = {
  string: "文本",
  number: "数字",
  boolean: "开关",
  enum: "选项",
};

/**
 * 插件声明的协议设置项（meta.channelSettings，在模型的协议设置里填）→ 展示行，保持声明的书写顺序。
 * meta 缺失或没声明时为空数组。
 */
export function settingRows(meta: PluginMeta | null | undefined): SettingRow[] {
  const schema = meta?.channelSettings;
  if (!schema || typeof schema !== "object") return [];
  return Object.entries(schema).map(([name, spec]) => ({
    name,
    label: spec?.label || name,
    typeLabel: SETTING_TYPE_LABEL[spec?.type] ?? String(spec?.type ?? ""),
    required: !!spec?.required,
    defaultText:
      spec?.default === undefined || spec.default === null
        ? ""
        : typeof spec.default === "object"
          ? JSON.stringify(spec.default)
          : String(spec.default),
    options: Array.isArray(spec?.options) ? spec.options : [],
    description: spec?.description ?? "",
  }));
}

/**
 * 某个能力的请求，仅用于展示（宿主不据此发请求）：“POST /v1/messages”。
 * 方法缺省按 POST；没有声明路径、没有这个能力、meta 缺失都显示 —。
 */
export function endpointLabel(meta: PluginMeta | null | undefined, kind: string): string {
  const endpoint =
    meta?.endpoints && typeof meta.endpoints === "object" ? meta.endpoints[kind] : undefined;
  const path = endpoint?.path?.trim();
  if (!path) return "—";
  return `${endpoint?.method?.trim().toUpperCase() || "POST"} ${path}`;
}

/** 插件声明的厂商 slug；没声明为空串 */
export const vendorOf = (meta: PluginMeta | null | undefined): string => meta?.vendor?.trim() ?? "";

/** 按插件 key + 版本 ID（或版本号）找插件版本 */
export function findPluginVersion(
  plugins: readonly PluginView[],
  pluginKey: string,
  version: { id?: number | null; version?: string | null },
): PluginVersionView | undefined {
  const plugin = plugins.find((item) => item.key === pluginKey);
  const versions = plugin?.versions ?? [];
  return (
    (version.id != null ? versions.find((item) => item.id === version.id) : undefined) ??
    (version.version ? versions.find((item) => item.version === version.version) : undefined)
  );
}

/**
 * 插件版本能不能承接某种模型：普通种类要在 endpoints 里声明。
 * Agent 不走插件（由宿主 LLM 网关直接用 Bearer 请求渠道的 OpenAI 兼容接口），恒为 false。
 */
export function metaSupportsKind(meta: { endpoints?: unknown }, kind: string): boolean {
  if (kind === "agent") return false;
  return !!meta.endpoints && typeof meta.endpoints === "object" && kind in meta.endpoints;
}

/** 比较两个 semver 版本号（只比主.次.修订，预发布后缀忽略）；a 大返回正数 */
export function compareSemver(a: string, b: string): number {
  const parse = (text: string) =>
    text
      .split("-")[0]
      .split(".")
      .map((part) => Number.parseInt(part, 10) || 0);
  const left = parse(a);
  const right = parse(b);
  for (let index = 0; index < Math.max(left.length, right.length); index++) {
    const diff = (left[index] ?? 0) - (right[index] ?? 0);
    if (diff !== 0) return diff;
  }
  return 0;
}

/** 插件里比当前固定版本更新的最高版本（模型协议的 plugin / version 传进来）；没有更新（或找不到插件）返回 null */
export function availableUpgrade(
  plugins: readonly PluginView[],
  pinned: { plugin_key: string; plugin_version: string },
): PluginVersionView | null {
  const plugin = plugins.find((item) => item.key === pinned.plugin_key);
  let best: PluginVersionView | null = null;
  for (const version of plugin?.versions ?? []) {
    if (compareSemver(version.version, pinned.plugin_version) <= 0) continue;
    if (!best || compareSemver(version.version, best.version) > 0) best = version;
  }
  return best;
}

/** 插件当前最新的版本（按 semver 最高）；没有版本返回 undefined */
export function latestVersion(
  plugin: Pick<PluginView, "versions"> | undefined,
): PluginVersionView | undefined {
  let best: PluginVersionView | undefined;
  for (const version of plugin?.versions ?? []) {
    if (!best || compareSemver(version.version, best.version) > 0) best = version;
  }
  return best;
}

/**
 * 版本不能删除的原因；可以删除返回 null。
 * 内置插件的版本、还有模型的协议固定在该版本的版本不能删（后端还会检查非终态任务，返回 409）。
 */
export function versionDeleteBlock(
  plugin: Pick<PluginView, "source">,
  version: Pick<PluginVersionView, "model_count">,
): string | null {
  if (plugin.source === "builtin") return "内置版本不能删除";
  if (version.model_count > 0)
    return `${version.model_count} 个模型固定在此版本，先把它们升级或改用别的协议`;
  return null;
}

/** 插件当前有多少个模型在用（各版本 model_count 之和） */
export const pluginModelCount = (plugin: Pick<PluginView, "versions">) =>
  (plugin.versions ?? []).reduce((sum, version) => sum + (version.model_count ?? 0), 0);

/** 其中固定在旧版本（低于最新版本）上的模型数，即“升级模型”会处理的数量 */
export function outdatedModelCount(plugin: Pick<PluginView, "versions">): number {
  const latest = latestVersion(plugin);
  if (!latest) return 0;
  return (plugin.versions ?? []).reduce(
    (sum, version) =>
      compareSemver(version.version, latest.version) < 0 ? sum + (version.model_count ?? 0) : sum,
    0,
  );
}

/**
 * 协议固定在该插件旧版本上的模型（升级模型确认框的清单）。
 * 协议选的插件和版本来自模型列表项的 plugin / plugin_version；没选协议（待配置、Agent）的不算。
 * @param plugin 插件（要带全部版本）
 * @param models 全部模型列表项（listModelOptions）
 */
export function outdatedModels<T extends { plugin?: string; plugin_version?: string }>(
  plugin: PluginView | undefined,
  models: readonly T[],
): T[] {
  const latest = latestVersion(plugin);
  if (!plugin || !latest) return [];
  return models.filter(
    (model) =>
      model.plugin === plugin.key &&
      !!model.plugin_version &&
      compareSemver(model.plugin_version, latest.version) < 0,
  );
}

/** 升级结果：成功数与失败清单（失败的模型保持原样，原因留给人看） */
export type UpgradeSummary = {
  /** 实际升到的版本 */
  toVersion: string;
  /** 升级成功的模型数 */
  upgraded: number;
  /** 失败的模型：展示名与原因 */
  failed: Array<{ key: string; name: string; reason: string }>;
};

/**
 * 升级模型接口的结果 → 给人看的汇总。
 * @param result upgradePluginModels 的返回
 * @param nameOf 模型 key → 展示名；拿不到名字时退回 key
 */
export function summarizeUpgrade(
  result: UpgradeModelsResult,
  nameOf: (key: string) => string,
): UpgradeSummary {
  return {
    toVersion: result.to_version,
    upgraded: result.upgraded.length,
    failed: result.failed.map((item) => ({
      key: item.key,
      name: nameOf(item.key) || item.key,
      reason: item.reason?.trim() || "未知原因",
    })),
  };
}

/** 插件版本 meta 里声明的四种 kind，按固定顺序，用于“支持的生成方式”四格 */
export const PLUGIN_KINDS = ["text", "video", "image", "audio"] as const;

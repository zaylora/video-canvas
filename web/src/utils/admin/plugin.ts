import type {
  ChannelView,
  ConfigIssue,
  PluginMeta,
  PluginUploadResult,
  PluginVersionView,
  PluginView,
} from "@/api/admin-ai/type";

/** 插件文件大小上限（与后端 pluginmeta.MaxPluginBytes 一致） */
export const PLUGIN_MAX_BYTES = 512 * 1024;

/** 上传前的本地检查：扩展名 .js、非空、≤512KB；通过返回 null */
export function checkPluginFile(file: {
  name: string;
  size: number;
}): string | null {
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
      message:
        typeof message === "string" && message.trim()
          ? message.trim()
          : "未知问题",
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
      a.issue.path === b.issue.path
        ? a.index - b.index
        : a.issue.path < b.issue.path
          ? -1
          : 1,
    )
    .map(({ issue }) => issue);
}

export type UploadSummary = {
  tone: "success" | "error";
  title: string;
  issues: ConfigIssue[];
};

/** 上传结果 → 给人看的一句话 + 问题清单 */
export function summarizeUpload(
  result: PluginUploadResult | null | undefined,
): UploadSummary {
  const issues = normalizeIssues(result?.issues);
  if (result?.accepted) {
    const version = result.version;
    const name = version
      ? `${version.plugin_key}@${version.version}`
      : "新版本";
    return { tone: "success", title: `预检通过，已登记 ${name}`, issues };
  }
  return {
    tone: "error",
    title:
      issues.length > 0 ? `预检未通过：${issues.length} 个问题` : "预检未通过",
    issues,
  };
}

/** sha256 前 8 位 */
export const shortSha = (sha: string | null | undefined) =>
  (sha ?? "").slice(0, 8);

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
      return "插件自行签名（需渠道开启“允许插件读取 Key”）";
    case "none":
      return "无需鉴权";
    default:
      return String(type);
  }
}

export type MetaSummary = {
  authType: string;
  auth: string;
  endpoints: Array<{ kind: string; mode: string }>;
  allowedHosts: string[];
  settingNames: string[];
  importable: boolean;
  description?: string;
};

/** 版本列表里展示的 meta 摘要；meta 缺失或字段为 null 都不崩 */
export function metaSummary(meta: PluginMeta | null | undefined): MetaSummary {
  const endpoints =
    meta?.endpoints && typeof meta.endpoints === "object" ? meta.endpoints : {};
  return {
    authType: meta?.auth?.type ?? "none",
    auth: describeAuth(meta?.auth),
    endpoints: Object.entries(endpoints).map(([kind, value]) => ({
      kind,
      mode: value?.mode ?? "?",
    })),
    allowedHosts: Array.isArray(meta?.allowedHosts) ? meta.allowedHosts : [],
    settingNames:
      meta?.channelSettings && typeof meta.channelSettings === "object"
        ? Object.keys(meta.channelSettings)
        : [],
    importable: !!meta?.import,
    description: meta?.description || undefined,
  };
}

/** 按插件 key + 版本 ID（或版本号）找插件版本 */
export function findPluginVersion(
  plugins: readonly PluginView[],
  pluginKey: string,
  version: { id?: number | null; version?: string | null },
): PluginVersionView | undefined {
  const plugin = plugins.find((item) => item.key === pluginKey);
  const versions = plugin?.versions ?? [];
  return (
    (version.id != null
      ? versions.find((item) => item.id === version.id)
      : undefined) ??
    (version.version
      ? versions.find((item) => item.version === version.version)
      : undefined)
  );
}

/** 渠道所用插件版本的 meta；插件列表里找不到时为 undefined */
export const channelMeta = (
  plugins: readonly PluginView[],
  channel: ChannelView,
) =>
  findPluginVersion(plugins, channel.plugin_key, {
    id: channel.plugin_version_id,
    version: channel.plugin_version,
  })?.meta ?? undefined;

/**
 * 渠道能不能跑这种 kind 的模型：插件版本的 endpoints 里有这个 kind。
 * 找不到插件版本（列表没加载好）时返回 null，调用方别当成“不支持”。
 */
export function channelSupportsKind(
  plugins: readonly PluginView[],
  channel: ChannelView,
  kind: string,
): boolean | null {
  const meta = channelMeta(plugins, channel);
  if (!meta) return null;
  return (
    !!meta.endpoints &&
    typeof meta.endpoints === "object" &&
    kind in meta.endpoints
  );
}

/** 模型编辑器“选择渠道”下拉：只列出支持该 kind 的渠道（插件信息未知的也保留） */
export const channelsForKind = (
  channels: readonly ChannelView[],
  plugins: readonly PluginView[],
  kind: string,
) =>
  channels.filter(
    (channel) => channelSupportsKind(plugins, channel, kind) !== false,
  );

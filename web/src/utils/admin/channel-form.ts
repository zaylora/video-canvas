import type {
  ChannelCreateRequest,
  ChannelRateLimit,
  ChannelUpdateRequest,
  ChannelView,
  PluginView,
} from "@/api/admin-ai/type";
import { findPluginVersion } from "./plugin";
import {
  initialSettingValues,
  settingFields,
  validateSettingValues,
  type SettingField,
  type SettingFormValues,
} from "./settings-form";

/** 渠道 key 格式（与后端一致） */
export const CHANNEL_KEY_PATTERN = /^[a-z0-9][a-z0-9-]{0,63}$/;

/** 渠道编辑表单；数字输入框存原始文本 */
export type ChannelFormState = {
  key: string;
  name: string;
  pluginKey: string;
  pluginVersion: string;
  baseUrl: string;
  trustedInternal: boolean;
  allowCredentials: boolean;
  enabled: boolean;
  rps: string;
  maxConcurrency: string;
  settings: SettingFormValues;
};

/** 新建渠道的空表单：默认选第一个启用的插件的最新版本 */
export function emptyChannelForm(plugins: readonly PluginView[]): ChannelFormState {
  const plugin = plugins.find((item) => item.enabled && (item.versions ?? []).length > 0);
  return {
    key: "",
    name: "",
    pluginKey: plugin?.key ?? "",
    pluginVersion: plugin?.versions?.[0]?.version ?? "",
    baseUrl: "https://",
    trustedInternal: false,
    allowCredentials: false,
    enabled: true,
    rps: "",
    maxConcurrency: "",
    settings: {},
  };
}

const numText = (value: unknown) =>
  typeof value === "number" && Number.isFinite(value) && value > 0 ? String(value) : "";

/** 渠道视图 → 编辑表单 */
export function channelFormFromView(view: ChannelView, fields: SettingField[]): ChannelFormState {
  return {
    key: view.key,
    name: view.name ?? "",
    pluginKey: view.plugin_key ?? "",
    pluginVersion: view.plugin_version ?? "",
    baseUrl: view.base_url ?? "",
    trustedInternal: !!view.trusted_internal,
    allowCredentials: !!view.allow_credentials,
    enabled: !!view.enabled,
    rps: numText(view.rate_limit?.rps),
    maxConcurrency: numText(view.rate_limit?.max_concurrency),
    settings: initialSettingValues(fields, view.settings),
  };
}

/** 换了插件版本、设置项声明变了：同名字段的取值尽量带过去，其余用新声明的默认值 */
export function rebaseSettings(
  oldFields: SettingField[],
  oldValues: SettingFormValues,
  newFields: SettingField[],
): SettingFormValues {
  return initialSettingValues(newFields, validateSettingValues(oldFields, oldValues).values);
}

/** base_url：http/https、有主机、不带用户名密码；通过返回 null */
export function checkBaseUrl(raw: string): string | null {
  const text = raw.trim();
  if (!text) return "请填写 base_url";
  let url: URL;
  try {
    url = new URL(text);
  } catch {
    return "base_url 不是合法的地址";
  }
  if (url.protocol !== "http:" && url.protocol !== "https:") return "base_url 只能是 http 或 https";
  if (!url.hostname) return "base_url 缺少主机名";
  if (url.username || url.password) return "base_url 不能包含用户名或密码";
  return null;
}

function parseLimit(text: string, label: string): { value: number } | { error: string } {
  const trimmed = text.trim();
  if (!trimmed) return { value: 0 };
  const value = Number(trimmed);
  if (!Number.isFinite(value) || value < 0) return { error: `${label}必须是非负数字` };
  return { value };
}

export type ChannelRequestResult =
  | { ok: false; errors: Record<string, string> }
  | {
      ok: true;
      create: ChannelCreateRequest;
      update: ChannelUpdateRequest;
      changed: boolean;
    };

const sameJson = (a: unknown, b: unknown) => JSON.stringify(a ?? {}) === JSON.stringify(b ?? {});

/**
 * 表单 → 请求体。新建用 create；编辑用 update（只含改过的字段，没改动时 changed=false）。
 * 错误键：key / name / plugin / baseUrl / rps / maxConcurrency / settings.<字段名>
 */
export function buildChannelRequest(
  form: ChannelFormState,
  fields: SettingField[],
  original?: ChannelView | null,
): ChannelRequestResult {
  const errors: Record<string, string> = {};
  const key = form.key.trim();
  if (!original && !CHANNEL_KEY_PATTERN.test(key)) {
    errors.key = "key 只能是小写字母、数字、连字符，以字母或数字开头，最长 64 位";
  }
  const name = form.name.trim();
  if (!name) errors.name = "请填写名称";
  if (!form.pluginKey || !form.pluginVersion) errors.plugin = "请选择插件与版本";
  const baseUrlError = checkBaseUrl(form.baseUrl);
  if (baseUrlError) errors.baseUrl = baseUrlError;
  const rps = parseLimit(form.rps, "rps");
  if ("error" in rps) errors.rps = rps.error;
  const concurrency = parseLimit(form.maxConcurrency, "最大并发");
  if ("error" in concurrency) errors.maxConcurrency = concurrency.error;
  else if (!Number.isInteger(concurrency.value)) errors.maxConcurrency = "最大并发必须是整数";
  const settings = validateSettingValues(fields, form.settings);
  for (const [field, message] of Object.entries(settings.errors))
    errors[`settings.${field}`] = message;

  if (Object.keys(errors).length > 0 || "error" in rps || "error" in concurrency) {
    return { ok: false, errors };
  }

  const rateLimit: ChannelRateLimit = {
    rps: rps.value,
    max_concurrency: concurrency.value,
  };
  const create: ChannelCreateRequest = {
    key: original ? original.key : key,
    name,
    plugin_key: form.pluginKey,
    plugin_version: form.pluginVersion,
    base_url: form.baseUrl.trim(),
    trusted_internal: form.trustedInternal,
    allow_credentials: form.allowCredentials,
    settings: settings.values,
    rate_limit: rateLimit,
    enabled: form.enabled,
  };
  if (!original) return { ok: true, create, update: {}, changed: true };

  const update: ChannelUpdateRequest = {};
  if (create.name !== original.name) update.name = create.name;
  if (
    create.plugin_key !== original.plugin_key ||
    create.plugin_version !== original.plugin_version
  ) {
    update.plugin_key = create.plugin_key;
    update.plugin_version = create.plugin_version;
  }
  if (create.base_url !== original.base_url) update.base_url = create.base_url;
  if (create.trusted_internal !== !!original.trusted_internal)
    update.trusted_internal = create.trusted_internal;
  if (create.allow_credentials !== !!original.allow_credentials)
    update.allow_credentials = create.allow_credentials;
  if (!sameJson(create.settings, original.settings)) update.settings = create.settings;
  const originalLimit = {
    rps: original.rate_limit?.rps ?? 0,
    max_concurrency: original.rate_limit?.max_concurrency ?? 0,
  };
  if (!sameJson(rateLimit, originalLimit)) update.rate_limit = rateLimit;
  if (create.enabled !== !!original.enabled) update.enabled = create.enabled;
  return { ok: true, create, update, changed: Object.keys(update).length > 0 };
}

/** 名称里的英文、数字转成 key 片段：小写、非字母数字变连字符、去掉首尾连字符 */
const slugify = (text: string) =>
  text
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-+|-+$/g, "");

/**
 * 新建渠道时按名称自动生成 key（用户没手动改过 key 时用）：
 * 名称里有英文 / 数字就用它；没有（纯中文名）就用“插件 key + 地址主机名第一段”；
 * 和已有渠道重名时依次加 -2、-3……；结果满足 CHANNEL_KEY_PATTERN。
 * @param existing 已有渠道的 key
 */
export function suggestChannelKey(
  name: string,
  pluginKey: string,
  baseUrl: string,
  existing: readonly string[],
): string {
  let host = "";
  try {
    host = new URL(baseUrl.trim()).hostname.split(".").find((part) => part !== "www") ?? "";
  } catch {
    host = "";
  }
  const base = slugify(name) || slugify([pluginKey, host].filter(Boolean).join("-")) || "channel";
  const trimmed = base.slice(0, 60).replace(/-+$/, "");
  const taken = new Set(existing);
  if (!taken.has(trimmed)) return trimmed;
  for (let n = 2; ; n += 1) {
    const candidate = `${trimmed}-${n}`;
    if (!taken.has(candidate)) return candidate;
  }
}

export type ChannelUpgrade = {
  /** 提交给 PUT /channels/:key 的请求体 */
  update: ChannelUpdateRequest;
  /** 新版本不再声明、已丢弃的设置项（显示名），有值的才算 */
  dropped: string[];
  /** 新版本不是 auth: custom，“允许插件读取 Key”被自动关闭 */
  credsReset: boolean;
};

/**
 * 把渠道切到插件的另一个版本（批量升级用），与渠道抽屉里换版本的规则一致：
 * 同名设置项的取值带过去，其余用新声明的默认值；新版本不需要读 Key 时关闭 allow_credentials。
 * 找不到目标版本返回 null。
 */
export function upgradeChannelRequest(
  channel: ChannelView,
  plugins: readonly PluginView[],
  targetVersion: string,
): ChannelUpgrade | null {
  const next = findPluginVersion(plugins, channel.plugin_key, { version: targetVersion });
  if (!next) return null;
  const current = findPluginVersion(plugins, channel.plugin_key, {
    id: channel.plugin_version_id,
    version: channel.plugin_version,
  });
  const oldFields = settingFields(current?.meta?.channelSettings);
  const newFields = settingFields(next.meta?.channelSettings);
  const oldValues = initialSettingValues(oldFields, channel.settings);
  const dropped = oldFields
    .filter((field) => {
      const value = oldValues[field.name];
      const had =
        typeof value === "boolean" ? value : typeof value === "string" && value.trim() !== "";
      return had && !newFields.some((item) => item.name === field.name);
    })
    .map((field) => field.label);
  const settings = validateSettingValues(
    newFields,
    rebaseSettings(oldFields, oldValues, newFields),
  ).values;
  const credsReset = !!channel.allow_credentials && next.meta?.auth?.type !== "custom";
  const update: ChannelUpdateRequest = {
    plugin_key: channel.plugin_key,
    plugin_version: next.version,
    settings,
  };
  if (credsReset) update.allow_credentials = false;
  return { update, dropped, credsReset };
}

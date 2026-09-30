import type { ModelDraft } from "@/api/admin-ai/type";

/**
 * 模型配置正文（admin-ai-api.md「模型配置正文」）的小工具。
 * 编辑器是 JSON 文本、键顺序是刻意保留的（input_schema 的书写顺序就是前端渲染顺序），
 * 这里的函数只读或只改个别字段，JSON.parse / stringify 本身保留对象键顺序。
 */

/** 模型 kind */
export const MODEL_KINDS = ["text", "video", "image", "audio"] as const;

const isRecord = (value: unknown): value is Record<string, unknown> =>
  typeof value === "object" && value !== null && !Array.isArray(value);

/** 正文里的 kind；没有或不是字符串返回空串 */
export const readModelKind = (body: unknown) =>
  isRecord(body) && typeof body.kind === "string" ? body.kind : "";

/** 正文里 channels[0]；缺失返回空串 */
export function readModelChannel(body: unknown): {
  channel: string;
  upstreamModel: string;
} {
  const first =
    isRecord(body) && Array.isArray(body.channels)
      ? body.channels[0]
      : undefined;
  return {
    channel:
      isRecord(first) && typeof first.channel === "string" ? first.channel : "",
    upstreamModel:
      isRecord(first) && typeof first.upstream_model === "string"
        ? first.upstream_model
        : "",
  };
}

/**
 * 把 channels[0].channel 改成指定渠道，其余字段和键顺序不动。
 * 正文没有 channels 时追加一项（upstream_model 留空待填）。正文不是对象时返回 null。
 */
export function withModelChannel(
  body: unknown,
  channelKey: string,
): Record<string, unknown> | null {
  if (!isRecord(body)) return null;
  const channels = Array.isArray(body.channels) ? [...body.channels] : [];
  const first = isRecord(channels[0]) ? channels[0] : {};
  channels[0] = {
    ...first,
    channel: channelKey,
    upstream_model: first.upstream_model ?? "",
  };
  if ("channels" in body)
    return Object.fromEntries(
      Object.entries(body).map(([key, value]) => [
        key,
        key === "channels" ? channels : value,
      ]),
    );
  return { ...body, channels };
}

/** 上游模型名 → 建议的模型 key：小写，只留字母数字与连字符 */
export function suggestModelKey(upstreamModel: string): string {
  return upstreamModel
    .toLowerCase()
    .replace(/[^a-z0-9-]+/g, "-")
    .replace(/-{2,}/g, "-")
    .replace(/^-+|-+$/g, "")
    .slice(0, 64);
}

/** 后端草稿可能是 snake_case（契约）也可能透传插件的 camelCase，两种都认 */
function pick<T>(
  draft: Record<string, unknown>,
  snake: string,
  camel: string,
): T | undefined {
  return (draft[snake] ?? draft[camel]) as T | undefined;
}

/** 规整一份导入草稿；缺上游模型名的丢掉 */
export function normalizeDraft(raw: unknown): ModelDraft | null {
  if (!isRecord(raw)) return null;
  const upstream = pick<unknown>(raw, "upstream_model", "upstreamModel");
  if (typeof upstream !== "string" || !upstream) return null;
  const params = pick<unknown>(raw, "params", "params");
  const schema = pick<unknown>(raw, "input_schema", "inputSchema");
  return {
    upstream_model: upstream,
    kind: typeof raw.kind === "string" ? raw.kind : "",
    label: typeof raw.label === "string" ? raw.label : "",
    params: isRecord(params) ? params : null,
    input_schema: isRecord(schema) ? schema : null,
  };
}

/**
 * 导入草稿 → 新建模型编辑器的预填正文。
 * 渠道、上游模型名、kind、label、params、input_schema 取自草稿；积分等留默认值，运营再改。
 */
export function draftToModelBody(
  draft: ModelDraft,
  channelKey: string,
): Record<string, unknown> {
  const kind = draft.kind || "video";
  return {
    key: suggestModelKey(draft.upstream_model),
    kind,
    label: draft.label || draft.upstream_model,
    hint: "",
    credits: 1,
    deadline: kind === "text" ? "5m" : "30m",
    enabled: false,
    sort: 100,
    channels: [{ channel: channelKey, upstream_model: draft.upstream_model }],
    params: draft.params ?? {},
    input_schema: draft.input_schema ?? {},
  };
}

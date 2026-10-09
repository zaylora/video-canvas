import type { ModelDraft, PluginMeta } from "@/api/admin/ai/type";

import {
  draftToModelBody,
  MODEL_KINDS,
  suggestModelKey,
  withDefaultPrice,
  withModelField,
} from "./model-body";

/**
 * 「从渠道导入模型」批量处理的纯逻辑：kind 推断、key 生成与检查、草稿正文补齐、逐个保存 / 上线。
 * 不碰 React 和请求实现（请求函数由调用方注入），方便单测。
 */

/** 模型 key 的字符集与长度（与后端 aiConfigKeyRe / aiModelKeyMaxLen 一致） */
const MODEL_KEY_RE = /^[A-Za-z0-9][A-Za-z0-9_.-]*$/;
export const MODEL_KEY_MAX_LENGTH = 128;

/**
 * 渠道插件 endpoints 支持的 kind，按 text / video / image / audio 的固定顺序。
 * 插件信息未知（meta 缺失）时返回全部 kind，交给后端兜底。
 */
export function supportedKinds(meta: PluginMeta | null | undefined): string[] {
  if (!meta) return [...MODEL_KINDS];
  const endpoints = meta.endpoints && typeof meta.endpoints === "object" ? meta.endpoints : {};
  return MODEL_KINDS.filter((kind) => kind in endpoints);
}

/** 按上游模型名猜 kind 的关键词（只在草稿没给 kind 时用，猜中也要运营确认） */
const KIND_HINTS: Array<{ kind: string; pattern: RegExp }> = [
  { kind: "video", pattern: /video|seedance|kling|veo|sora|hailuo|vidu|wan[-_.]?\d|i2v|t2v/i },
  { kind: "image", pattern: /image|img|dall-?e|flux|seedream|imagen|midjourney|mj|sdxl|t2i/i },
  { kind: "audio", pattern: /audio|tts|speech|voice|music|suno|whisper/i },
  { kind: "text", pattern: /gpt|chat|claude|llama|qwen|deepseek|gemini|glm|moonshot|kimi/i },
];

/** kind 推断结果；certain=false 时界面要给下拉让运营确认 */
export type KindGuess = { kind: string; certain: boolean };

/**
 * 推断导入草稿的 kind：
 * 1. 草稿给了 kind 且渠道支持 → 确定；
 * 2. 渠道只支持一种 kind → 就是它，确定；
 * 3. 否则按上游模型名关键词猜（猜的结果必须在渠道支持范围内），不确定；
 * 4. 都不行取渠道支持的第一个，不确定。
 * @param draft 导入草稿
 * @param kinds 渠道支持的 kind（supportedKinds 的结果）；为空时视为全部
 */
export function inferDraftKind(
  draft: Pick<ModelDraft, "kind" | "upstream_model">,
  kinds: readonly string[],
): KindGuess {
  const allowed = kinds.length > 0 ? kinds : MODEL_KINDS;
  if (draft.kind && allowed.includes(draft.kind)) return { kind: draft.kind, certain: true };
  if (allowed.length === 1) return { kind: allowed[0], certain: true };
  const hinted = KIND_HINTS.find(
    (hint) => allowed.includes(hint.kind) && hint.pattern.test(draft.upstream_model),
  );
  return { kind: hinted?.kind ?? allowed[0], certain: false };
}

/**
 * 上游模型名 → 模型 key：在 suggestModelKey 的基础上兜底（全是非 ASCII 字符时生成空串，返回 "model"）。
 */
export function keyFromUpstream(upstreamModel: string): string {
  return suggestModelKey(upstreamModel) || "model";
}

/**
 * 检查一个待导入的 key；通过返回 null。
 * @param key 运营填的 key
 * @param existing 已存在的模型 key
 * @param others 同一批里其他行的 key（用于发现批内重复）
 */
export function checkImportKey(
  key: string,
  existing: ReadonlySet<string>,
  others: readonly string[] = [],
): string | null {
  const text = key.trim();
  if (!text) return "请填写产品标识";
  if (text.length > MODEL_KEY_MAX_LENGTH) return `不能超过 ${MODEL_KEY_MAX_LENGTH} 个字符`;
  if (!MODEL_KEY_RE.test(text)) return "只能用字母、数字、_ . -，且以字母或数字开头";
  if (existing.has(text)) return "已存在同名模型";
  if (others.includes(text)) return "和本批其他行重复";
  return null;
}

/** 表格一行的可编辑字段 */
export type ImportRowFields = {
  /** 产品标识 */
  key: string;
  /** 展示名 */
  label: string;
  /** 能力类型 */
  kind: string;
};

/** 草稿 → 表格行的初始值 */
export function initialImportRow(
  draft: ModelDraft,
  kinds: readonly string[],
): ImportRowFields & KindGuess {
  const guess = inferDraftKind(draft, kinds);
  return {
    key: keyFromUpstream(draft.upstream_model),
    label: draft.label || draft.upstream_model,
    kind: guess.kind,
    certain: guess.certain,
  };
}

/** 补齐后的正文；priceSkipped 表示设了统一价格但该模型按 Token 计费，价格没改 */
export type ImportBody = { body: Record<string, unknown>; priceSkipped: boolean };

/**
 * 补齐一份要保存的模型正文：按选定的 kind 预填能力与定价（含插件参数建议），
 * 再写入运营改过的 key、展示名；设了统一默认价格就改价格（Token 计费的不改）。
 * @param draft 导入草稿
 * @param channelKey 来源渠道
 * @param fields 表格里的 key / 展示名 / kind
 * @param price 统一默认价格（积分）；undefined 表示不改
 */
export function buildImportBody(
  draft: ModelDraft,
  channelKey: string,
  fields: ImportRowFields,
  price?: number,
): ImportBody {
  const label = fields.label.trim() || draft.upstream_model;
  let body = draftToModelBody({ ...draft, kind: fields.kind, label }, channelKey);
  body = withModelField(body, "key", fields.key.trim()) ?? body;
  if (price === undefined) return { body, priceSkipped: false };
  const priced = withDefaultPrice(body, price);
  return priced ? { body: priced, priceSkipped: false } : { body, priceSkipped: true };
}

/**
 * 解析统一默认价格输入：留空 → undefined（不改）；非负整数 → 数字；其他 → 错误文案。
 */
export function parseImportPrice(
  text: string,
): { ok: true; value?: number } | { ok: false; message: string } {
  const value = text.trim();
  if (!value) return { ok: true };
  if (!/^\d+$/.test(value)) return { ok: false, message: "请填写不小于 0 的整数" };
  return { ok: true, value: Number(value) };
}

/** 批量方式：只保存（默认不上线）/ 保存后直接上线 */
export type ImportMode = "draft" | "online";

/** 一行的处理结果 */
export type ImportOutcome = {
  key: string;
  label: string;
  status: "done" | "skipped" | "failed";
  /** 跳过 / 失败的原因，或成功时的补充说明 */
  reason?: string;
  /** 模型是否已落库（上线失败但已保存也算），用于更新“已存在”列表，避免重复创建 */
  saved: boolean;
};

/** 批量处理用到的接口（调用方注入真实请求，测试注入假的） */
export type ImportApi = {
  /** 新建模型（保存，默认不上线），返回校验问题 */
  create: (body: Record<string, unknown>) => Promise<{ issues: readonly unknown[] }>;
  /** 上线 / 下线 */
  setEnabled: (key: string, enabled: boolean) => Promise<unknown>;
};

/** 一项待处理的任务 */
export type ImportJob = { key: string; label: string } & ImportBody;

const TOKEN_NOTE = "按 Token 计费，没有统一价格，价格未改";

/**
 * 逐个（串行）保存导入的模型：不因某一个失败而中断，每个都给出结果。
 * - draft：只保存，不上线；
 * - online：保存后有校验问题的只保存不上线（记为跳过），没问题的接着上线。
 * @param jobs 待处理的正文
 * @param mode 处理方式
 * @param api 请求函数
 * @param onProgress 每处理完一个回调一次（已完成数，总数）
 * @param describeError 把异常翻译成给人看的原因
 */
export async function runImportJobs(
  jobs: readonly ImportJob[],
  mode: ImportMode,
  api: ImportApi,
  onProgress: (done: number, total: number) => void = () => {},
  describeError: (error: unknown) => string = () => "失败",
): Promise<ImportOutcome[]> {
  const out: ImportOutcome[] = [];
  for (const [index, job] of jobs.entries()) {
    const base = { key: job.key, label: job.label, saved: false };
    const note = job.priceSkipped ? TOKEN_NOTE : undefined;
    try {
      const result = await api.create(job.body);
      base.saved = true;
      if (mode === "online" && result.issues.length > 0) {
        out.push({
          ...base,
          status: "skipped",
          reason: [`有 ${result.issues.length} 个问题，已保存但没上线`, note]
            .filter(Boolean)
            .join("；"),
        });
      } else {
        if (mode === "online") {
          await api.setEnabled(job.key, true);
        }
        out.push({ ...base, status: "done", reason: note });
      }
    } catch (error) {
      const reason = describeError(error);
      out.push({
        ...base,
        status: "failed",
        reason: base.saved ? `已保存，上线失败：${reason}` : reason,
      });
    }
    onProgress(index + 1, jobs.length);
  }
  return out;
}

/** 已经落库的 key，用于更新“已存在”列表 */
export const savedKeys = (outcomes: readonly ImportOutcome[]) =>
  outcomes.filter((item) => item.saved).map((item) => item.key);

/** 结果汇总：「成功 3 个，跳过 1 个，失败 0 个」 */
export function summarizeImport(outcomes: readonly ImportOutcome[]) {
  const count = (status: ImportOutcome["status"]) =>
    outcomes.filter((item) => item.status === status).length;
  return { done: count("done"), skipped: count("skipped"), failed: count("failed") };
}

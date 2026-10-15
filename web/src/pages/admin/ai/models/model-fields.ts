import type { ConfigIssue } from "@/api/admin/ai/type.d";
import type { Billing, Capabilities, PriceTier } from "@/api/model/type.d";
import {
  checkDeadline,
  readModelChannel,
  readModelPricing,
  readModelString,
} from "@/utils/admin/model-body";
import type { ModelChannelInfo } from "@/utils/admin/model-channel";

import { LIMITS } from "./capability-editors";
import { billingBlocked, staleCondition, tierDimensions } from "./price-editors";

/** 编辑弹窗的三个页签 */
export type ModelTabId = "basic" | "params" | "price";

/** 正文顶层字段 → 所在页签与表单控件 id（点击待办项 / 校验问题时定位用） */
const FIELD_OF: Record<string, { tab: ModelTabId; id: string }> = {
  key: { tab: "basic", id: "model-key" },
  label: { tab: "basic", id: "model-label" },
  kind: { tab: "basic", id: "model-kind" },
  hint: { tab: "basic", id: "model-hint" },
  vendor: { tab: "basic", id: "model-vendor" },
  tags: { tab: "basic", id: "model-tags" },
  sort: { tab: "basic", id: "model-sort" },
  channels: { tab: "basic", id: "model-channel" },
  deadline: { tab: "params", id: "model-deadline" },
  capabilities: { tab: "params", id: "model-capabilities-params" },
  pricing: { tab: "price", id: "model-pricing" },
};

/**
 * 校验问题的路径 → 页签与控件。channels[0] 下有三种问题：
 * upstream_model 是上游模型标识，protocol（含 protocol.plugin / version / settings）落在“能力与协议”区块，
 * channel 是所属渠道；协议问题不能落到渠道的控件上。
 */
export function fieldOfPath(path: string): { tab: ModelTabId; id: string } | null {
  if (path.startsWith("channels[0].upstream_model")) return { tab: "basic", id: "model-upstream" };
  if (path.startsWith("channels[0].protocol")) return { tab: "basic", id: "model-protocol" };
  if (path.startsWith("channels[0].channel")) return { tab: "basic", id: "model-channel" };
  if (path.startsWith("capabilities.prompt")) return { tab: "params", id: "model-prompt-length" };
  return FIELD_OF[path.split(/[.[]/)[0]] ?? null;
}

/** 后端校验问题里落在某个字段上的第一条说明 */
export const issueFor = (issues: ConfigIssue[], path: string) =>
  issues.find(
    (issue) =>
      issue.path === path || issue.path.startsWith(`${path}.`) || issue.path.startsWith(`${path}[`),
  )?.message;

/**
 * 待办的严重程度：error 是会影响使用的问题（红点，阻止保存）；
 * warning 是建议项或上线前还要处理的事（黄点，不影响保存）。
 */
export type CheckLevel = "error" | "warning";

/** 页签 / 底栏提示的一项 */
export type ModelCheck = { tab: ModelTabId; text: string; path?: string; level: CheckLevel };

/** 一组待办里最严重的级别；没有待办返回 null */
export function worstLevel(checks: ModelCheck[]): CheckLevel | null {
  if (checks.some((check) => check.level === "error")) return "error";
  return checks.length > 0 ? "warning" : null;
}

const isRecord = (value: unknown): value is Record<string, unknown> =>
  typeof value === "object" && value !== null && !Array.isArray(value);

/**
 * 正文内部自相矛盾、保存下来就会让模型用不了的问题：计费方式与能力不匹配、
 * 规格价格引用了已删掉的规格、提示词字数上限与超时时限不合法。和后端校验一致的部分只是提前拦截。
 */
function blockingChecks(body: Record<string, unknown>, kind: string): ModelCheck[] {
  const out: ModelCheck[] = [];
  const error = (tab: ModelTabId, path: string, text: string) =>
    out.push({ tab, path, text, level: "error" });
  const caps = (isRecord(body.capabilities) ? body.capabilities : undefined) as
    | Capabilities
    | undefined;

  const deadline = readModelString(body, "deadline");
  const deadlineProblem = deadline ? checkDeadline(deadline) : null;
  if (deadlineProblem) error("params", "deadline", `任务超时：${deadlineProblem}`);

  if (caps) {
    const max = caps?.prompt?.max_length ?? 0;
    const [min, top] = LIMITS.promptLength;
    if (!Number.isInteger(max) || max < min || max > top)
      error("params", "capabilities.prompt", `提示词字数上限需要在 ${min} – ${top} 之间`);
  }

  const pricing = readModelPricing(body);
  if (!pricing || !kind) return out;
  const billingReason = billingBlocked(pricing.billing as Billing, kind, caps);
  if (billingReason) error("price", "pricing.billing", billingReason);
  if (pricing.billing !== "token") {
    const dims = tierDimensions(caps);
    (pricing.tiers ?? []).forEach((tier: PriceTier, index) => {
      const path = `pricing.tiers[${index}].when`;
      if (Object.keys(tier.when ?? {}).length === 0)
        error("price", path, `第 ${index + 1} 条规格价格没有条件，会盖掉默认价`);
      else {
        const stale = staleCondition(tier, dims);
        if (stale) error("price", path, `第 ${index + 1} 条规格价格：${stale}，请调整或删除`);
      }
    });
  }
  return out;
}

/** 两个路径是不是同一个字段，或一个包含另一个 */
export const samePlace = (a: string, b: string) =>
  a === b ||
  a.startsWith(`${b}.`) ||
  a.startsWith(`${b}[`) ||
  b.startsWith(`${a}.`) ||
  b.startsWith(`${a}[`);

/**
 * 编辑弹窗页签上的提示：
 * - 红点（error）：必填项（含调用协议）没填、正文内部矛盾，保存会被拦下；
 * - 黄点（warning）：渠道不可用 / 缺 Key、选填项用了默认值，以及最近一次保存 / 校验返回的问题。
 * 和后端启用检查一致的部分只是提前提示，最终以后端为准。
 * @param ready 渠道与插件清单是否已加载好；没加载好时不对渠道、协议下结论
 */
export function modelChecks(
  body: Record<string, unknown> | null,
  info: ModelChannelInfo,
  issues: ConfigIssue[],
  ready = true,
): ModelCheck[] {
  if (!body) return [];
  const out: ModelCheck[] = blockingChecks(body, info.kind);
  const warn = (tab: ModelTabId, text: string, path?: string) =>
    out.push({ tab, text, path, level: "warning" });
  const fail = (tab: ModelTabId, text: string, path: string) =>
    out.push({ tab, text, path, level: "error" });
  const { channel, upstreamModel } = readModelChannel(body);
  if (!readModelString(body, "label")) fail("basic", "填写展示名称", "label");
  if (!info.kind) fail("basic", "选择模型能力", "kind");
  if (!channel) fail("basic", "选择所属渠道", "channels[0].channel");
  else if (ready && info.channel && !info.channel.enabled)
    warn("basic", `渠道「${info.channel.name}」已停用`, "channels[0].channel");
  else if (ready && !info.channel) warn("basic", `渠道 ${channel} 不存在`, "channels[0].channel");
  if (ready && info.kind && info.protocolProblem)
    fail("basic", info.protocolProblem, "channels[0].protocol");
  if (ready && info.channel && info.keyMissing)
    warn("basic", `渠道「${info.channel.name}」还没有设置 Key`, "channels[0].channel");
  if (!upstreamModel) fail("basic", "填写上游模型 ID", "channels[0].upstream_model");
  if (info.kind !== "agent" && !readModelString(body, "deadline"))
    warn("params", "没有设置任务超时，将使用系统默认值", "deadline");
  const price = readModelPricing(body);
  const unpriced =
    !price ||
    (price.billing === "token"
      ? !price.token?.in && !price.token?.out
      : !(price.billing === "per_second" ? price.per_second : price.unit));
  if (unpriced) warn("price", "还没有设置默认积分价格", "pricing");
  const blocked = out.filter((check) => check.level === "error");
  for (const issue of issues) {
    if (blocked.some((check) => check.path && samePlace(check.path, issue.path))) continue;
    const field = fieldOfPath(issue.path);
    warn(field?.tab ?? "params", issue.message, issue.path);
  }
  return out;
}

import type { ConfigIssue } from "@/api/admin/ai/type.d";
import { readModelChannel, readModelString } from "@/utils/admin/model-body";
import type { ModelChannelInfo } from "@/utils/admin/model-channel";

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
  params: { tab: "params", id: "model-params" },
  capabilities: { tab: "params", id: "model-capabilities" },
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
  return FIELD_OF[path.split(/[.[]/)[0]] ?? null;
}

/** 后端校验问题里落在某个字段上的第一条说明 */
export const issueFor = (issues: ConfigIssue[], path: string) =>
  issues.find(
    (issue) =>
      issue.path === path || issue.path.startsWith(`${path}.`) || issue.path.startsWith(`${path}[`),
  )?.message;

/** 上线前待办的一项 */
export type ModelCheck = { tab: ModelTabId; text: string; path?: string };

/**
 * 启用前还需处理的事：必填项没填、能力与协议没选、渠道不可用 / 缺 Key，以及最近一次保存 / 校验返回的问题。
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
  const out: ModelCheck[] = [];
  const { channel, upstreamModel } = readModelChannel(body);
  if (!readModelString(body, "key"))
    out.push({ tab: "basic", text: "填写产品模型标识", path: "key" });
  if (!readModelString(body, "label"))
    out.push({ tab: "basic", text: "填写展示名称", path: "label" });
  if (!info.kind) out.push({ tab: "basic", text: "选择模型能力", path: "kind" });
  if (!channel) out.push({ tab: "basic", text: "选择所属渠道", path: "channels[0].channel" });
  else if (ready && info.channel && !info.channel.enabled)
    out.push({
      tab: "basic",
      text: `渠道「${info.channel.name}」已停用`,
      path: "channels[0].channel",
    });
  else if (ready && !info.channel)
    out.push({ tab: "basic", text: `渠道 ${channel} 不存在`, path: "channels[0].channel" });
  if (ready && info.kind && info.protocolProblem)
    out.push({ tab: "basic", text: info.protocolProblem, path: "channels[0].protocol" });
  if (ready && info.channel && info.keyMissing)
    out.push({
      tab: "basic",
      text: `渠道「${info.channel.name}」还没有设置 Key`,
      path: "channels[0].channel",
    });
  if (!upstreamModel)
    out.push({ tab: "basic", text: "填写上游模型 ID", path: "channels[0].upstream_model" });
  for (const issue of issues) {
    const field = fieldOfPath(issue.path);
    out.push({ tab: field?.tab ?? "params", text: issue.message, path: issue.path });
  }
  return out;
}

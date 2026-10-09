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

/** 校验问题的路径 → 页签与控件；channels[0].upstream_model 单独映射 */
export function fieldOfPath(path: string): { tab: ModelTabId; id: string } | null {
  if (path.startsWith("channels[0].upstream_model")) return { tab: "basic", id: "model-upstream" };
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
 * 上线前还需处理的事：必填项没填、渠道不可用 / 缺 Key，以及最近一次保存 / 校验返回的问题。
 * 和后端启用检查一致的部分只是提前提示，最终以后端为准。
 */
export function modelChecks(
  body: Record<string, unknown> | null,
  info: ModelChannelInfo,
  issues: ConfigIssue[],
): ModelCheck[] {
  if (!body) return [];
  const out: ModelCheck[] = [];
  const { channel, upstreamModel } = readModelChannel(body);
  if (!readModelString(body, "key"))
    out.push({ tab: "basic", text: "填写产品模型标识", path: "key" });
  if (!readModelString(body, "label"))
    out.push({ tab: "basic", text: "填写展示名称", path: "label" });
  if (!channel) out.push({ tab: "basic", text: "选择所属渠道", path: "channels" });
  else if (info.channel && !info.channel.enabled)
    out.push({ tab: "basic", text: `渠道「${info.channel.name}」已停用`, path: "channels" });
  else if (info.keyMissing)
    out.push({
      tab: "basic",
      text: `渠道「${info.channel?.name ?? channel}」还没有设置 Key`,
      path: "channels",
    });
  else if (info.supportsKind === false)
    out.push({ tab: "basic", text: "渠道的插件版本不支持这个能力", path: "channels" });
  if (!upstreamModel)
    out.push({ tab: "basic", text: "填写上游模型 ID", path: "channels[0].upstream_model" });
  for (const issue of issues) {
    const field = fieldOfPath(issue.path);
    out.push({ tab: field?.tab ?? "params", text: issue.message, path: issue.path });
  }
  return out;
}

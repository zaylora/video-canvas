import type { ListModelsParams } from "@/api/admin/ai/type";

/** 能力筛选里“待配置”的取值：对应后端的 status=pending（还没选能力或协议的模型），不是 kind */
export const PENDING_FILTER = "pending";

/**
 * 面板里的筛选参数 → 模型列表接口的查询参数：固定限定在当前渠道；
 * 能力筛选选了“待配置”时改成 status=pending（待配置的模型 kind 为空，按 kind 查不到）。
 * @param query useAdminList 给出的查询（q、page、page_size、kind、status）
 * @param channelKey 当前渠道 key
 */
export function modelListQuery(
  query: Record<string, string | number>,
  channelKey: string,
): ListModelsParams {
  const { kind, status, ...rest } = query;
  const pending = kind === PENDING_FILTER;
  return {
    ...(rest as ListModelsParams),
    ...(!pending && kind ? { kind: String(kind) } : {}),
    ...(pending ? { status: PENDING_FILTER } : status ? { status: String(status) } : {}),
    channel: channelKey,
  };
}

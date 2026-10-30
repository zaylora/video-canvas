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

/**
 * 拖动后的新顺序是否仍在各自的组内：列表按启用开关分两组（已启用在前），只能在组内拖动。
 * 逐位比较新旧顺序的启用标记，完全一致说明没有行跨过分组边界。
 * @param before 拖动前（服务端）的顺序
 * @param after 拖动后的本地顺序
 * @returns 两组各自内部调换返回 true；有行跨组或行数不一致返回 false
 */
export function reorderKeepsGroups(
  before: { enabled?: boolean }[],
  after: { enabled?: boolean }[],
): boolean {
  return (
    before.length === after.length &&
    after.every((item, index) => !!item.enabled === !!before[index]?.enabled)
  );
}

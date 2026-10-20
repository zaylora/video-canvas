import { ensureTraceZip } from "@/utils/admin/agent-runs";
import service from "@/utils/requests/service";
import type { AgentRunPage, ListAgentRunsParams } from "./type";

/**
 * 契约见设计文档 docs/design/Agent调优Harness/Agent调优Harness.md 8.4；
 * 前缀 /api/v1 由 axios baseURL 提供，仅管理员可访问
 */
const P = "/admin/agent/runs";

/** 去掉空值，免得把 `user=` 之类的空参数发给后端 */
const clean = (params: Record<string, unknown>) =>
  Object.fromEntries(
    Object.entries(params).filter(([, v]) => v !== undefined && v !== null && v !== ""),
  );

/**
 * Agent 运行列表，页码分页，按创建时间倒序；不传 from/to 时后端默认最近 7 天
 * @param params 用户、画布、run id、状态、来源、时间范围与 page / page_size
 * @returns 当页的运行与命中总数；list 缺失时补成空数组
 */
export const listAgentRuns = async (params: ListAgentRunsParams = {}): Promise<AgentRunPage> => {
  const page = await service.get<AgentRunPage | null>(P, clean({ ...params }));
  return {
    list: page?.list ?? [],
    total: page?.total ?? 0,
    page: page?.page ?? params.page ?? 1,
    page_size: page?.page_size ?? params.page_size ?? 20,
  };
};

/**
 * 下载某一轮的轨迹包（zip）。要带登录凭证，所以走请求实例取 blob，由调用方保存成文件。
 * 轨迹已过期 / 已关闭 / run 不存在时，后端返回 `{code,msg}` 信封而不是文件：
 * 这里让 4xx 也进入成功分支以读到信封（401 仍走拦截器清登录态），再由 ensureTraceZip 识别并抛 ApiError；
 * silent 让拦截器不弹全局 toast，提示交给页面（成功 / 失败文案不同）。每次调用服务端都会写审计 agent.trace.export。
 * @param rid run id
 * @returns zip 内容
 * @throws ApiError 业务错误（文案来自后端）、网络错误或无法识别的响应
 */
export const downloadAgentRunTrace = async (rid: string): Promise<Blob> =>
  ensureTraceZip(
    await service.request<Blob>({
      url: `${P}/${encodeURIComponent(rid)}/trace`,
      responseType: "blob",
      silent: true,
      validateStatus: (status) => status < 400 || (status < 500 && status !== 401),
    }),
  );

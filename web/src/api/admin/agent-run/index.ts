import service from "@/utils/requests/service";
import { adminAgentRunEndpoints as ep } from "./endpoints";
import type {
  AgentRunDetail,
  AgentRunExport,
  AgentRunListQuery,
  AgentRunPage,
  AgentStatsGroup,
  AgentStatsQuery,
  AgentStatsResult,
} from "./type";

/** 去掉空值，免得把 `status=` 之类的空参数发给后端 */
const clean = (params: object): Record<string, unknown> =>
  Object.fromEntries(
    Object.entries(params).filter(([, v]) => v !== undefined && v !== null && v !== ""),
  );

/**
 * 运行摘要列表（创建时间倒序，游标分页）
 * @param query 筛选条件与 cursor / limit
 * @returns 当页记录和下一页游标；items 缺失时补成空数组，没有下一页时 next_cursor 是空串
 */
export const listAgentRuns = async (query: AgentRunListQuery = {}): Promise<AgentRunPage> => {
  const page = await service.get<Partial<AgentRunPage> | null>(ep.list(), clean(query));
  return { items: page?.items ?? [], next_cursor: page?.next_cursor ?? "" };
};

/**
 * 一次运行的详情；运行不存在时后端返回 404 / 60012，由调用方据此显示「运行不存在」
 * @param rid 运行 ID（编码串）
 * @returns 详情；各数组缺失时补成空数组
 */
export const getAgentRun = async (rid: string): Promise<AgentRunDetail> => {
  const d = await service.get<Partial<AgentRunDetail>>(ep.detail(rid), undefined);
  return {
    digest: d.digest ?? null,
    run: d.run ?? null,
    timeline: d.timeline ?? [],
    events: d.events ?? [],
    tool_calls: d.tool_calls ?? [],
    model_calls: d.model_calls ?? [],
    mutations: d.mutations ?? [],
    approvals: d.approvals ?? [],
    feedback: d.feedback ?? null,
    context: d.context ?? [],
    runtime_tail: d.runtime_tail ?? "",
  };
};

/**
 * 导出一次运行的 Markdown：与详情里的时间线由后端同一份步骤生成
 * @param rid 运行 ID（编码串）
 * @returns Markdown 正文
 */
export const exportAgentRunMarkdown = async (rid: string): Promise<string> =>
  (await service.get<AgentRunExport>(ep.export(rid), { format: "md" })).content;

/**
 * 版本对比统计：按维度分组的聚合指标
 * @param query 分组维度和时间范围
 * @returns 各组指标，按最近一次运行时间倒序；groups 缺失时补成空数组
 */
export const getAgentStats = async (query: AgentStatsQuery): Promise<AgentStatsGroup[]> =>
  ((await service.get<Partial<AgentStatsResult> | null>(ep.stats(), clean(query))) ?? {}).groups ??
  [];

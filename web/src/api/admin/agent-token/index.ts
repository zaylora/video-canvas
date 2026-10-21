import service from "@/utils/requests/service";
import { adminAgentTokenEndpoints as ep } from "./endpoints";
import type { AgentTokenItem, AgentTokenList, IssueAgentTokenReq, IssuedAgentToken } from "./type";

/**
 * 分析令牌列表；items 缺失时补成空数组
 * @returns 全部令牌，新的在前
 */
export const listAgentTokens = async (): Promise<AgentTokenItem[]> =>
  ((await service.get<AgentTokenList | null>(ep.list(), undefined)) ?? { items: [] }).items ?? [];

/**
 * 签发分析令牌。明文只在这次响应里返回，调用方展示后不要再保存
 * @param data 用途（1–64 字）和有效天数（1–90）
 * @returns 新令牌的记录和明文
 */
export const issueAgentToken = (data: IssueAgentTokenReq) =>
  service.post<IssuedAgentToken>(ep.issue(), data);

/**
 * 注销令牌：立即失效，不可恢复，注销后列表里不再返回
 * @param id 令牌 ID
 */
export const revokeAgentToken = (id: number) => service.delete<void>(ep.revoke(id));

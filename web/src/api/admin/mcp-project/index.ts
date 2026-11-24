import service from "@/utils/requests/service";

import { adminMCPProjectEndpoints as ep } from "./endpoints";
import type {
  IssuedMCPToken,
  MCPProjectCreated,
  MCPProjectItem,
  MCPProjectList,
  MCPProjectReq,
} from "./type";

/**
 * 项目列表；items 缺失时补成空数组
 * @returns 全部项目，新的在前
 */
export const listMCPProjects = async (): Promise<MCPProjectItem[]> =>
  ((await service.get<MCPProjectList | null>(ep.list(), undefined)) ?? { items: [] }).items ?? [];

/**
 * 新建项目。服务端在建项目的同一次请求里就签好首枚令牌，
 * 返回的 issued 带明文与端点，直接可以拼成配置片段。
 * @param data 名称、归属用户、画布、权限档位
 */
export const createMCPProject = (data: MCPProjectReq) =>
  service.post<MCPProjectCreated>(ep.create(), data);

/**
 * 修改项目（含启停）
 * @param id 项目 ID
 * @param data 完整字段；enabled 不传表示保持原状
 */
export const updateMCPProject = (id: string, data: MCPProjectReq) =>
  service.put<MCPProjectItem>(ep.update(id), data);

/**
 * 删除项目：令牌一并删除，不可恢复
 * @param id 项目 ID
 */
export const removeMCPProject = (id: string) => service.delete<void>(ep.remove(id));

/**
 * 生成或轮换项目那枚令牌：一个项目只有一枚，旧的立刻失效。
 * 明文只在这条响应里返回，展示后不要再保存。
 * @param id 项目 ID
 */
export const issueMCPToken = (id: string) => service.post<IssuedMCPToken>(ep.issueToken(id));

/**
 * 取回项目那枚令牌的明文：「显示配置片段」用。
 * 只对未过期、当初存了密文的令牌有效。
 * @param id 项目 ID
 */
export const getMCPTokenSecret = async (id: string): Promise<string> =>
  (await service.get<{ token: string } | null>(ep.tokenSecret(id), undefined))?.token ?? "";

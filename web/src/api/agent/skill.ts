import service from "@/utils/requests/service";
import type { AgentSkillDto } from "./type";

/**
 * 获取 @ 弹层里可用的技能目录（已启用的内置技能与导入技能）；后端可能返回 null，统一补成空数组。
 * 单独成文件，不和会话接口混在 index 里
 * @returns 技能列表
 */
export const getAgentSkills = async (): Promise<AgentSkillDto[]> =>
  (await service.get<AgentSkillDto[] | null>("/agent/skills", undefined)) ?? [];

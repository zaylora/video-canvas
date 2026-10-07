import type { AgentSkillDto } from "@/api/agent/type.d";

/** @ 弹层里的一个技能选项：就是接口返回的目录项 */
export type AgentSkillOption = AgentSkillDto;

/**
 * 按显示名、技能名、说明过滤 @ 弹层里的技能；空关键词返回全部
 * @param skills 接口返回的技能目录
 * @param query 搜索关键词
 * @returns 命中的技能，保持原顺序
 */
export function filterSkills(skills: readonly AgentSkillOption[], query: string) {
  const q = query.trim().toLowerCase();
  if (!q) return [...skills];
  return skills.filter((s) => `${s.title} ${s.name} ${s.description}`.toLowerCase().includes(q));
}

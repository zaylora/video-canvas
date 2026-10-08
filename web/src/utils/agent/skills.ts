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

/** 技能目录的加载状态 */
export type SkillsStatus = "loading" | "ready" | "error";

/**
 * 输入 / 弹出的技能菜单没有可选项时，该显示的一句提示；有可选项时返回 null
 * @param status 目录的加载状态
 * @param catalogSize 目录里的技能总数
 * @param matched 当前关键词命中的数量
 * @returns 提示文字，或 null（直接显示列表）
 */
export function skillMenuNotice(status: SkillsStatus, catalogSize: number, matched: number) {
  if (status === "loading") return "正在加载技能…";
  if (status === "error") return "技能加载失败，请用下方的技能按钮重试";
  if (matched > 0) return null;
  return catalogSize === 0 ? "还没有启用的技能" : "没有找到匹配的技能";
}

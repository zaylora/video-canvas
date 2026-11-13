import type { Project, Round, Script } from "@/api/marketing/type";

/**
 * 把一份轮次快照并进项目：已有的原地替换，新轮次按轮次号倒序插入。不改原对象。
 * @param project 项目详情
 * @param round 最新轮次
 */
export function mergeRound(project: Project, round: Round): Project {
  const exists = project.rounds.some((item) => item.id === round.id);
  const rounds = exists
    ? project.rounds.map((item) => (item.id === round.id ? round : item))
    : [...project.rounds, round].sort((a, b) => b.no - a.no);
  return { ...project, rounds };
}

/**
 * 把一份脚本快照并进所在轮次：按 id 替换，没有就追加。
 * 找不到轮次说明本地缺这一轮，返回 null，由调用方重新拉详情。
 * @param project 项目详情
 * @param roundId 脚本所在轮次
 * @param script 最新脚本
 */
export function mergeScript(project: Project, roundId: string, script: Script): Project | null {
  if (!project.rounds.some((item) => item.id === roundId)) return null;
  return {
    ...project,
    rounds: project.rounds.map((round) => {
      if (round.id !== roundId) return round;
      const exists = round.scripts.some((item) => item.id === script.id);
      return {
        ...round,
        scripts: exists
          ? round.scripts.map((item) => (item.id === script.id ? script : item))
          : [...round.scripts, script],
      };
    }),
  };
}

/**
 * 轮次是否刚写完：scripting 变 ready / failed。没见过的轮次（对账拉回的旧数据）、被取消的都不算。
 * @param prev 之前本地的轮次
 * @param next 最新轮次
 */
export function roundTransition(prev: Round | undefined, next: Round): "ready" | "failed" | null {
  if (!prev || prev.status !== "scripting") return null;
  if (next.status === "ready" || next.status === "failed") return next.status;
  return null;
}

/**
 * 脚本是否刚出完片：rendering 变 done / failed。
 * @param prev 之前本地的脚本
 * @param next 最新脚本
 */
export function scriptTransition(prev: Script | undefined, next: Script): "done" | "failed" | null {
  if (!prev || prev.status !== "rendering") return null;
  if (next.status === "done" || next.status === "failed") return next.status;
  return null;
}

/**
 * 出片结束要不要弹提醒：只在用户不在该项目页面时提醒成片出好；出片失败不弹，失败原因在项目页的舞台上看。
 * @param prev 之前本地的脚本
 * @param next 最新脚本
 * @param viewing 用户是否正看着这个项目
 */
export function scriptNotice(
  prev: Script | undefined,
  next: Script,
  viewing: boolean,
): "done" | null {
  return scriptTransition(prev, next) === "done" && !viewing ? "done" : null;
}

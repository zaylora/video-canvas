import { useCallback, useEffect, useRef, useState } from "react";

import { getAgentSkills } from "@/api/agent/skill";
import type { AgentSkillDto } from "@/api/agent/type.d";
import type { SkillsStatus } from "@/utils/agent/skills";

/**
 * 弹层打开时读技能目录（管理员随时可能启停技能，所以每次打开都刷新）。
 * 失败时保留上一次成功的列表；请求错误的全局 toast 由拦截器弹，这里只记状态让弹层退化成可重试的提示。
 */
export function useSkillCatalog(open: boolean) {
  const [skills, setSkills] = useState<AgentSkillDto[]>([]);
  const [status, setStatus] = useState<SkillsStatus>("loading");
  /** 只认最近一次请求的结果，快速开合弹层时旧请求不会盖掉新的 */
  const latest = useRef(0);
  const load = useCallback(async () => {
    const ticket = ++latest.current;
    setStatus((prev) => (prev === "ready" ? prev : "loading"));
    try {
      const list = await getAgentSkills();
      if (ticket !== latest.current) return;
      setSkills(list);
      setStatus("ready");
    } catch {
      if (ticket === latest.current) setStatus((prev) => (prev === "ready" ? prev : "error"));
    }
  }, []);
  useEffect(() => {
    if (open) void load();
  }, [open, load]);
  return { skills, status, reload: load };
}

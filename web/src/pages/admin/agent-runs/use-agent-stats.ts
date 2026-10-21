import { useCallback, useEffect, useRef, useState } from "react";

import { getAgentStats } from "@/api/admin/agent-run";
import type { AgentStatsGroup } from "@/api/admin/agent-run/type.d";
import { statsSince } from "@/utils/admin/agent-run";

/** 版本对比的加载状态：loading 首次加载中，ready 已有数据，error 加载失败 */
export type StatsStatus = "loading" | "ready" | "error";

/**
 * 版本对比用的统计：最近 30 天按 harness_key 分组（版本卡片和 Harness 下拉共用）、按 model_key 分组（模型下拉）。
 * 两个请求互不牵连：卡片区失败只在卡片区提示和重试，不影响下面的列表；模型下拉拿不到时只是少了选项。
 * 请求错误的全局提示由拦截器弹，这里只管状态。
 * @returns 版本统计及其状态、模型统计、重试
 */
export function useAgentStats() {
  const [status, setStatus] = useState<StatsStatus>("loading");
  const [harness, setHarness] = useState<AgentStatsGroup[]>([]);
  const [models, setModels] = useState<AgentStatsGroup[]>([]);
  const [attempt, setAttempt] = useState(0);
  const alive = useRef(true);

  useEffect(() => {
    alive.current = true;
    return () => {
      alive.current = false;
    };
  }, []);

  useEffect(() => {
    const since = statsSince(Date.now());
    getAgentStats({ group_by: "harness_key", since })
      .then((groups) => {
        if (!alive.current) return;
        setHarness(groups);
        setStatus("ready");
      })
      .catch(() => alive.current && setStatus("error"));
    getAgentStats({ group_by: "model_key", since })
      .then((groups) => alive.current && setModels(groups))
      .catch(() => undefined);
  }, [attempt]);

  /** 重试：先回到加载中（卡片区显示骨架），再重新请求 */
  const retry = useCallback(() => {
    setStatus("loading");
    setAttempt((n) => n + 1);
  }, []);
  return { status, harness, models, retry };
}

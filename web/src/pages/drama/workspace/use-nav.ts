import { useCallback } from "react";
import { useSearchParams } from "react-router";

import { parseStage, type WorkspaceStage } from "@/utils/drama/stages";

/** 工作台的导航状态：当前工序和当前集，都写在地址栏（?area=assets&ep=3），刷新、分享链接都能回到原处 */
export type WorkspaceNav = {
  stage: WorkspaceStage;
  /** 当前集号；资产区的 0 表示「全集」 */
  ep: number;
  /** 切换工序；不传 ep 沿用当前集 */
  go: (stage: WorkspaceStage, ep?: number) => void;
  /** 只换集，工序不变 */
  setEp: (ep: number) => void;
};

/**
 * 读写地址栏里的工序与集号。
 * 集号不在分集里（被删、写错）时回落：资产区回到「全集」，其余回到第一集。
 */
export function useWorkspaceNav(episodes: readonly { no: number }[]): WorkspaceNav {
  const [params, setParams] = useSearchParams();
  const stage = parseStage(params.get("area"));
  const raw = params.get("ep");
  const parsed = raw === null ? NaN : Number(raw);
  const exists = Number.isInteger(parsed) && episodes.some((episode) => episode.no === parsed);
  let ep = exists ? parsed : (episodes[0]?.no ?? 1);
  if (stage === "assets" && (raw === null || parsed === 0 || !exists)) ep = 0;

  const write = useCallback(
    (nextStage: WorkspaceStage, nextEp: number) => {
      const next = new URLSearchParams();
      if (nextStage !== "overview") next.set("area", nextStage);
      // 总览也保留集号：从总览再点回别的工序时，还停在刚才那一集
      next.set("ep", String(nextEp));
      setParams(next, { replace: true });
    },
    [setParams],
  );

  const go = useCallback<WorkspaceNav["go"]>(
    (nextStage, nextEp) => {
      // 只有资产区认 0（全集）；从那里切到别的工序时落到第一集
      const fallback = ep === 0 && nextStage !== "assets" ? (episodes[0]?.no ?? 1) : ep;
      write(nextStage, nextEp ?? fallback);
    },
    [ep, episodes, write],
  );
  const setEp = useCallback((nextEp: number) => write(stage, nextEp), [stage, write]);

  return { stage, ep, go, setEp };
}

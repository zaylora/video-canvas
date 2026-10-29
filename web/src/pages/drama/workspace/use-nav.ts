import { useCallback, useEffect } from "react";
import { useSearchParams } from "react-router";

import type { EpisodeSummary } from "@/api/drama/type";
import {
  EPISODE_DOC,
  parseScriptDoc,
  resolveStage,
  scriptTabDoc,
  type ScriptDoc,
  type WorkspaceStage,
} from "@/utils/drama/stages";

/** 导航只需要每集的集号和剧本状态 */
type NavEpisode = Pick<EpisodeSummary, "no" | "scriptStatus">;

const NO_EPISODES: readonly NavEpisode[] = [];

/** 工作台的导航状态：当前工序、当前集和剧本页打开的项目文档，都写在地址栏（?area=script&ep=3&doc=outline），刷新、分享链接都能回到原处 */
export type WorkspaceNav = {
  stage: WorkspaceStage;
  /** 当前集号；资产区的 0 表示「全集」 */
  ep: number;
  /** 剧本页打开的大纲 / 人物传记；null 表示看当前集的剧本 */
  doc: ScriptDoc | null;
  /** 切换工序；不传 ep 沿用当前集（切到剧本页时：这一集已有剧本就看它，还没开始写就打开大纲），传了 ep 就是要看那一集 */
  go: (stage: WorkspaceStage, ep?: number) => void;
  /** 只换集，工序不变（同时离开大纲 / 人物传记） */
  setEp: (ep: number) => void;
  /** 在剧本页打开大纲或人物传记 */
  openDoc: (doc: ScriptDoc) => void;
};

/**
 * 读写地址栏里的工序与集号。
 * 集号不在分集里（被删、写错）时回落：资产区回到「全集」，其余回到第一集。
 * 没写工序时按短剧状态挑默认页（刚提交进剧本页看研读），挑完立即写回地址栏，
 * 免得大纲生成出来后默认页变了、页面自己跳走。
 */
export function useWorkspaceNav(
  drama: { outlineMd: string; episodes: readonly NavEpisode[] } | null,
): WorkspaceNav {
  const episodes = drama?.episodes ?? NO_EPISODES;
  const [params, setParams] = useSearchParams();
  const area = params.get("area");
  const stage = resolveStage(area, {
    outlineMd: drama?.outlineMd ?? "",
    episodeCount: episodes.length,
  });
  const raw = params.get("ep");
  const parsed = raw === null ? NaN : Number(raw);
  const exists = Number.isInteger(parsed) && episodes.some((episode) => episode.no === parsed);
  let ep = exists ? parsed : (episodes[0]?.no ?? 1);
  if (stage === "assets" && (raw === null || parsed === 0 || !exists)) ep = 0;
  const doc = stage === "script" ? parseScriptDoc(params.get("doc"), episodes.length) : null;

  const write = useCallback(
    (nextStage: WorkspaceStage, nextEp: number, nextDoc?: ScriptDoc | typeof EPISODE_DOC) => {
      const next = new URLSearchParams();
      next.set("area", nextStage);
      // 总览也保留集号：从总览再点回别的工序时，还停在刚才那一集
      next.set("ep", String(nextEp));
      if (nextDoc) next.set("doc", nextDoc);
      setParams(next, { replace: true });
    },
    [setParams],
  );

  const go = useCallback<WorkspaceNav["go"]>(
    (nextStage, nextEp) => {
      // 只有资产区认 0（全集）；从那里切到别的工序时落到第一集
      const fallback = ep === 0 && nextStage !== "assets" ? (episodes[0]?.no ?? 1) : ep;
      const target = nextEp ?? fallback;
      // 点名某一集就看它的剧本；单纯切过来时，这一集已经开始写剧本就停在这一集，还没开始写（研读、出大纲阶段）打开大纲
      const picked =
        nextStage !== "script"
          ? undefined
          : nextEp !== undefined
            ? EPISODE_DOC
            : scriptTabDoc(episodes.find((episode) => episode.no === target));
      write(nextStage, target, picked);
    },
    [ep, episodes, write],
  );
  const setEp = useCallback(
    (nextEp: number) => write(stage, nextEp, stage === "script" ? EPISODE_DOC : undefined),
    [stage, write],
  );
  const openDoc = useCallback(
    (nextDoc: ScriptDoc) => write("script", ep === 0 ? (episodes[0]?.no ?? 1) : ep, nextDoc),
    [ep, episodes, write],
  );

  const loaded = drama !== null;
  useEffect(() => {
    if (loaded && area === null) write(stage, ep);
  }, [loaded, area, stage, ep, write]);

  return { stage, ep, doc, go, setEp, openDoc };
}

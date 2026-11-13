import { create } from "zustand";

import type { Project, Round, Script } from "@/api/marketing/type";
import { mergeRound, mergeScript } from "@/utils/marketing/merge";

type MarketingState = {
  /** 打开过的项目详情，以项目 ID 为键；WebSocket 推送并进这里 */
  projects: Record<string, Project>;
  /** 项目标题（列表和详情都会记）：不在详情页时提醒用它写「某某的脚本写好了」 */
  titles: Record<string, string>;
  /** 正在看的项目详情页引用计数：断线重连后只对账这些 */
  watching: Record<string, number>;
  /** 每次有实时变化就加一：列表页据此静默刷新 */
  listVersion: number;
  /** 写入一份完整的项目详情 */
  setProject: (project: Project) => void;
  /** 并入一份轮次快照；本地没有这个项目返回 false（调用方去拉详情） */
  applyRound: (projectId: string, round: Round) => boolean;
  /** 并入一份脚本快照；本地缺项目或轮次返回 false（调用方去拉详情） */
  applyScript: (projectId: string, roundId: string, script: Script) => boolean;
  /** 记项目标题 */
  setTitles: (titles: Record<string, string>) => void;
  /** 开始看某个项目，返回取消函数 */
  watch: (projectId: string) => () => void;
  /** 删掉本地缓存的项目 */
  drop: (projectId: string) => void;
  /** 通知列表有变化 */
  bumpList: () => void;
};

/**
 * 营销片项目详情的本地缓存：详情页读它，HTTP 结果和 WebSocket 推送（marketing.round / marketing.script）
 * 都写进来，断线重连后对账（utils/marketing/events）重新拉一遍正在看的项目。
 */
export const useMarketingStore = create<MarketingState>((set, get) => ({
  projects: {},
  titles: {},
  watching: {},
  listVersion: 0,

  setProject: (project) =>
    set((state) => ({
      projects: { ...state.projects, [project.id]: project },
      titles: { ...state.titles, [project.id]: project.title },
      listVersion: state.listVersion + 1,
    })),

  applyRound: (projectId, round) => {
    const project = get().projects[projectId];
    if (!project) {
      set((state) => ({ listVersion: state.listVersion + 1 }));
      return false;
    }
    set((state) => ({
      projects: { ...state.projects, [projectId]: mergeRound(project, round) },
      listVersion: state.listVersion + 1,
    }));
    return true;
  },

  applyScript: (projectId, roundId, script) => {
    const project = get().projects[projectId];
    const next = project ? mergeScript(project, roundId, script) : null;
    if (!project || !next) {
      set((state) => ({ listVersion: state.listVersion + 1 }));
      return false;
    }
    set((state) => ({
      projects: { ...state.projects, [projectId]: next },
      listVersion: state.listVersion + 1,
    }));
    return true;
  },

  setTitles: (titles) => set((state) => ({ titles: { ...state.titles, ...titles } })),

  watch: (projectId) => {
    set((state) => ({
      watching: { ...state.watching, [projectId]: (state.watching[projectId] ?? 0) + 1 },
    }));
    return () =>
      set((state) => {
        const left = (state.watching[projectId] ?? 1) - 1;
        const watching = { ...state.watching };
        if (left <= 0) delete watching[projectId];
        else watching[projectId] = left;
        return { watching };
      });
  },

  drop: (projectId) =>
    set((state) => {
      const projects = { ...state.projects };
      delete projects[projectId];
      return { projects, listVersion: state.listVersion + 1 };
    }),

  bumpList: () => set((state) => ({ listVersion: state.listVersion + 1 })),
}));

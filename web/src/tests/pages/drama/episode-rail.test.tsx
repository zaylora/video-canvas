import { describe, expect, test } from "bun:test";
import { renderToStaticMarkup } from "react-dom/server";

import type { EpisodeSummary } from "@/api/drama/type";
import { DramaWorkspaceContext } from "@/pages/drama/workspace/context";
import { EpisodeRail } from "@/pages/drama/workspace/episode-rail";
import type { DramaWorkspace } from "@/pages/drama/workspace/use-workspace";
import type { WorkspaceStage } from "@/utils/drama/stages";

const episode = (patch: Partial<EpisodeSummary> = {}) =>
  ({ no: 1, title: "初见", locked: false, ...patch }) as EpisodeSummary;

const rail = (episodes: EpisodeSummary[], stage: WorkspaceStage = "assets", ep = 0) =>
  renderToStaticMarkup(
    <DramaWorkspaceContext.Provider
      value={
        {
          drama: { id: "d1", episodes },
          jobs: [],
          refresh: async () => {},
        } as unknown as DramaWorkspace
      }
    >
      <EpisodeRail stage={stage} ep={ep} onPick={() => {}} onDoc={() => {}} />
    </DramaWorkspaceContext.Provider>,
  );

describe("EpisodeRail：分集栏", () => {
  test("锁定集的小锁挂在按钮角上，不和集号挤在同一层里（否则会压住「01」）", () => {
    const html = rail([episode({ locked: true })]);
    const button = html.slice(html.indexOf('aria-label="第 1 集 · 初见"'));
    // 内容层（相对它定位的东西会叠在集号上）：集号 + 进度条，以进度条和内容层的两个闭合 span 结尾
    const contentEnd = button.indexOf("</span></span>");
    const lock = button.indexOf("lucide-lock");
    expect(lock).toBeGreaterThan(-1);
    expect(lock).toBeGreaterThan(contentEnd);
  });

  test("没锁定的集没有小锁", () => {
    expect(rail([episode()])).not.toContain("lucide-lock");
  });
});

describe("EpisodeRail：各工序共用同一条栏，切工序时不跳", () => {
  const episodes = [episode(), episode({ no: 2, title: "重逢" })];
  /** 栏里每一项按钮的读屏名，按出现顺序 */
  const labels = (html: string) =>
    [...html.matchAll(/<button[^>]*aria-label="([^"]+)"/g)].map((m) => m[1]);
  const stages: WorkspaceStage[] = ["script", "assets", "studio", "final"];

  test("每个工序顶部都是 大纲、人物、全集，再接各集，位置完全一致", () => {
    for (const stage of stages) {
      expect(labels(rail(episodes, stage, 1)).slice(0, 5)).toEqual([
        "故事大纲",
        "人物传记",
        "全集",
        "第 1 集 · 初见",
        "第 2 集 · 重逢",
      ]);
    }
  });

  test("「全集」只有资产管理能点，其他工序置灰禁用并说明原因", () => {
    const all = (html: string) =>
      html.slice(html.lastIndexOf("<button", html.indexOf('aria-label="全集"')));
    expect(all(rail(episodes, "assets", 0))).not.toMatch(/^<button[^>]* disabled=""/);
    for (const stage of ["script", "studio", "final"] as const) {
      const button = all(rail(episodes, stage, 1));
      expect(button).toMatch(/^<button[^>]*disabled=""/);
      expect(button).toMatch(/^<button[^>]*title="仅资产管理可按全集筛选"/);
    }
  });

  test("栏头统一叫「分集」", () => {
    for (const stage of stages) {
      const html = rail(episodes, stage, 1);
      expect(html).toContain(">分集<");
      expect(html).not.toContain(">筛选<");
    }
  });
});

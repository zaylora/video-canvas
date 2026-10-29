import { describe, expect, test } from "bun:test";
import { renderToStaticMarkup } from "react-dom/server";

import type { EpisodeSummary } from "@/api/drama/type";
import { DramaWorkspaceContext } from "@/pages/drama/workspace/context";
import { EpisodeRail } from "@/pages/drama/workspace/episode-rail";
import type { DramaWorkspace } from "@/pages/drama/workspace/use-workspace";

const episode = (patch: Partial<EpisodeSummary> = {}) =>
  ({ no: 1, title: "初见", locked: false, ...patch }) as EpisodeSummary;

const rail = (episodes: EpisodeSummary[]) =>
  renderToStaticMarkup(
    <DramaWorkspaceContext.Provider
      value={
        { drama: { id: "d1", episodes }, refresh: async () => {} } as unknown as DramaWorkspace
      }
    >
      <EpisodeRail stage="assets" ep={0} onPick={() => {}} />
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

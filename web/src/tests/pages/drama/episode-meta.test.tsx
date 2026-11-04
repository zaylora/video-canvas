import { describe, expect, test } from "bun:test";
import { renderToStaticMarkup } from "react-dom/server";

import type { Drama } from "@/api/drama/type";
import { AnalyzingView, useAnalyzing } from "@/pages/drama/workspace/analyzing-view";
import { DramaWorkspaceContext } from "@/pages/drama/workspace/context";
import { EmptyScript, EpisodeMeta } from "@/pages/drama/workspace/episode-meta";

describe("没有剧本的集：空状态", () => {
  const html = renderToStaticMarkup(
    <EmptyScript
      episode={{
        synopsis: "沈砚上山拜师",
        hook: "雪地里的断剑在发光",
        cliffhanger: "师兄认出了剑",
      }}
      onGenerate={() => {}}
    />,
  );

  test("图标 + 「这一集还没有剧本」+「生成剧本」按钮", () => {
    expect(html).toContain("这一集还没有剧本");
    expect(html).toContain("生成剧本");
    expect(html).toContain("lucide-file-pen-line");
  });

  test("下面列本集梗概、开场钩子、结尾悬念", () => {
    for (const text of [
      "梗概",
      "沈砚上山拜师",
      "开场钩子",
      "雪地里的断剑在发光",
      "结尾悬念",
      "师兄认出了剑",
    ]) {
      expect(html).toContain(text);
    }
  });

  test("没有钩子 / 悬念的老集只列有值的", () => {
    const old = renderToStaticMarkup(
      <EmptyScript episode={{ synopsis: "s", hook: "", cliffhanger: "" }} onGenerate={() => {}} />,
    );
    expect(old).toContain("梗概");
    expect(old).not.toContain("开场钩子");
    expect(old).not.toContain("结尾悬念");
  });
});

describe("页边：对应原文", () => {
  test("单个单元「第 3 章」、跨单元「第 3–5 章」，单位跟着单元种类", () => {
    const one = renderToStaticMarkup(
      <EpisodeMeta
        episode={{ synopsis: "", hook: "", cliffhanger: "", unitFrom: 3, unitTo: 3 }}
        unitKind="chapter"
      />,
    );
    expect(one).toContain("对应原文");
    expect(one).toContain("第 3 章");
    const range = renderToStaticMarkup(
      <EpisodeMeta
        episode={{ synopsis: "", hook: "", cliffhanger: "", unitFrom: 3, unitTo: 5 }}
        unitKind="scene"
      />,
    );
    expect(range).toContain("第 3–5 场");
  });

  test("没有关联单元的老集不显示对应原文；什么都没有时整块不渲染", () => {
    const old = renderToStaticMarkup(
      <EpisodeMeta
        episode={{ synopsis: "梗概", hook: "", cliffhanger: "", unitFrom: null, unitTo: null }}
        unitKind="chapter"
      />,
    );
    expect(old).not.toContain("对应原文");
    expect(
      renderToStaticMarkup(
        <EpisodeMeta
          episode={{ synopsis: "", hook: "", cliffhanger: "", unitFrom: null, unitTo: null }}
          unitKind="chapter"
        />,
      ),
    ).toBe("");
  });

  test("有剧本时页边也带梗概、钩子、悬念", () => {
    const out = renderToStaticMarkup(
      <EpisodeMeta
        episode={{ synopsis: "梗", hook: "钩", cliffhanger: "悬", unitFrom: 1, unitTo: 1 }}
        unitKind="chapter"
      />,
    );
    for (const text of ["梗", "钩", "悬", "第 1 章"]) expect(out).toContain(text);
  });
});

describe("研读视图：粗读全书 / 分集规划 两步，第一集出现后退出", () => {
  const wrap = (drama: Partial<Drama>, node: React.ReactElement) =>
    renderToStaticMarkup(
      <DramaWorkspaceContext.Provider
        value={
          {
            drama: {
              phase: "running",
              status: "analyzing",
              episodes: [],
              outlineMd: "",
              planCursor: 0,
              ...drama,
            },
            jobs: [],
          } as never
        }
      >
        {node}
      </DramaWorkspaceContext.Provider>,
    );
  /** 把 useAnalyzing 的结果写进标记里读出来 */
  const probe = (drama: Partial<Drama>) => {
    function Probe() {
      const { showAnalyzing } = useAnalyzing();
      return <i data-show={String(showAnalyzing)} />;
    }
    return { showAnalyzing: wrap(drama, <Probe />).includes('data-show="true"') };
  };

  test("步骤文案", () => {
    const out = wrap({}, <AnalyzingView />);
    expect(out).toContain("粗读全书");
    expect(out).toContain("抽样读开篇与后文，整理人物与梗概");
    expect(out).toContain("分集规划");
    expect(out).toContain("按批读原文，生成分集目录");
    expect(out).not.toContain("按集拆分");
    expect(out).not.toContain("故事大纲与人物传记");
  });

  test("规划中没有任何集：显示研读视图；第一集出来就退出", () => {
    expect(probe({}).showAnalyzing).toBe(true);
    expect(probe({ episodes: [{ no: 1 } as never] }).showAnalyzing).toBe(false);
  });

  test("暂停时还没有集：视图仍在，标题写「分集规划已暂停」；规划完成（done）不显示", () => {
    const paused = { phase: "paused" as const, status: "outlined" as const };
    expect(probe(paused).showAnalyzing).toBe(true);
    expect(wrap(paused, <AnalyzingView />)).toContain("分集规划已暂停");
    expect(probe({ phase: "done", status: "ready" }).showAnalyzing).toBe(false);
  });

  test("粗读完成（已有大纲）后高亮第二步", () => {
    const out = wrap({ outlineMd: "大纲" }, <AnalyzingView />);
    // 第一步打勾，第二步在转
    expect(out).toContain("lucide-check");
    expect(out).toContain("animate-spin");
  });
});

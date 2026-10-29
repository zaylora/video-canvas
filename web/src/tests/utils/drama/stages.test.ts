import { describe, expect, test } from "bun:test";

import type { DramaJob, EpisodeSummary } from "@/api/drama/type";
import {
  EPISODE_DOC,
  episodeMatrix,
  episodePips,
  jumpStageFor,
  overallProgress,
  parseScriptDoc,
  parseStage,
  resolveStage,
  scriptTabDoc,
  stageHints,
  resumeTarget,
  STAGES,
} from "@/utils/drama/stages";

const ep = (patch: Partial<EpisodeSummary> = {}): EpisodeSummary => ({
  no: 1,
  title: "雾港来信",
  synopsis: "",
  scriptStatus: "empty",
  locked: false,
  revision: 1,
  updatedAt: "x",
  storyboardStatus: "empty",
  segmentCount: 0,
  segmentSeconds: 0,
  doneSegmentCount: 0,
  final: null,
  finalStale: false,
  ...patch,
});

const FINAL = { id: "a1" } as unknown as NonNullable<EpisodeSummary["final"]>;

describe("工序标签：地址栏 ?area= 的解析", () => {
  test("五道工序按总览、剧本、资产管理、片场、成片排列", () => {
    expect(STAGES.map((s) => s.key)).toEqual(["overview", "script", "assets", "studio", "final"]);
  });

  test("合法值原样返回，没有或写错回到总览", () => {
    expect(parseStage("assets")).toBe("assets");
    expect(parseStage("final")).toBe("final");
    expect(parseStage(null)).toBe("overview");
    expect(parseStage("nope")).toBe("overview");
  });

  test("旧地址 ?area=episodes（分集视频）落到片场", () => {
    expect(parseStage("episodes")).toBe("studio");
  });
});

describe("地址栏没写工序时落在哪", () => {
  test("刚提交、大纲和分集都还没有：进剧本页（大纲里看研读进度）", () => {
    expect(resolveStage(null, { outlineMd: "", episodeCount: 0 })).toBe("script");
  });

  test("已经有大纲或分集：回到总览", () => {
    expect(resolveStage(null, { outlineMd: "# 大纲", episodeCount: 0 })).toBe("overview");
    expect(resolveStage(null, { outlineMd: "", episodeCount: 3 })).toBe("overview");
  });

  test("写了工序就照写的来", () => {
    expect(resolveStage("overview", { outlineMd: "", episodeCount: 0 })).toBe("overview");
    expect(resolveStage("assets", { outlineMd: "", episodeCount: 0 })).toBe("assets");
  });
});

describe("剧本页打开哪份项目文档（?doc=）", () => {
  test("地址栏写了大纲或人物传记就打开它", () => {
    expect(parseScriptDoc("outline", 16)).toBe("outline");
    expect(parseScriptDoc("characters", 16)).toBe("characters");
  });

  test("切到剧本页、研读完成后没写 doc：默认打开大纲，哪怕已经有分集", () => {
    expect(parseScriptDoc(null, 16)).toBe("outline");
    expect(parseScriptDoc(null, 0)).toBe("outline");
    expect(parseScriptDoc("nope", 16)).toBe("outline");
  });

  test("明确选了某一集（doc=episode）：看分集剧本", () => {
    expect(parseScriptDoc("episode", 16)).toBeNull();
  });

  test("还没拆出分集时 doc=episode 无集可看：仍打开大纲", () => {
    expect(parseScriptDoc("episode", 0)).toBe("outline");
  });
});

describe("分集栏：四格小进度条（剧本 / 拆解 / 分镜 / 成片）", () => {
  test("什么都没做：四格都是空", () => {
    expect(episodePips(ep())).toEqual(["off", "off", "off", "off"]);
  });

  test("剧本生成中闪烁，写好点亮；锁定即已拆解", () => {
    expect(episodePips(ep({ scriptStatus: "generating" }))[0]).toBe("run");
    expect(episodePips(ep({ scriptStatus: "ready" }))[0]).toBe("on");
    expect(episodePips(ep({ scriptStatus: "ready", locked: true }))[1]).toBe("on");
  });

  test("分镜：生成中闪烁；有分镜但片段没出齐是半亮，出齐点亮", () => {
    expect(episodePips(ep({ storyboardStatus: "generating" }))[2]).toBe("run");
    expect(
      episodePips(ep({ storyboardStatus: "ready", segmentCount: 6, doneSegmentCount: 2 }))[2],
    ).toBe("part");
    expect(
      episodePips(ep({ storyboardStatus: "ready", segmentCount: 6, doneSegmentCount: 6 }))[2],
    ).toBe("on");
  });

  test("成片：有成片点亮，成片已过期只算半亮", () => {
    expect(episodePips(ep({ final: FINAL }))[3]).toBe("on");
    expect(episodePips(ep({ final: FINAL, finalStale: true }))[3]).toBe("part");
  });
});

describe("总览矩阵：一集在五道工序上的状态", () => {
  test("没有剧本：剧本待做，后面各格不可点", () => {
    const row = episodeMatrix(ep());
    expect(row.script).toEqual({ state: "todo" });
    expect(row.breakdown).toEqual({ state: "none" });
    expect(row.storyboard).toEqual({ state: "none" });
    expect(row.segments).toEqual({ state: "none" });
    expect(row.final).toEqual({ state: "none" });
  });

  test("剧本写好还没拆解：拆解待做，分镜仍不可点", () => {
    const row = episodeMatrix(ep({ scriptStatus: "ready" }));
    expect(row.script.state).toBe("done");
    expect(row.breakdown.state).toBe("todo");
    expect(row.storyboard.state).toBe("none");
  });

  test("已拆解没有分镜：分镜待做", () => {
    const row = episodeMatrix(ep({ scriptStatus: "ready", locked: true }));
    expect(row.breakdown.state).toBe("done");
    expect(row.storyboard.state).toBe("todo");
  });

  test("片段进度显示已出视频 / 总数，出齐才算完成", () => {
    const base = ep({ scriptStatus: "ready", locked: true, storyboardStatus: "ready" });
    expect(episodeMatrix({ ...base, segmentCount: 6, doneSegmentCount: 2 }).segments).toEqual({
      state: "part",
      done: 2,
      total: 6,
    });
    expect(episodeMatrix({ ...base, segmentCount: 6, doneSegmentCount: 6 }).segments).toEqual({
      state: "done",
      done: 6,
      total: 6,
    });
  });

  test("成片：有片段没成片是待合成；已合成但过期仍可点重新合成", () => {
    const base = ep({
      scriptStatus: "ready",
      locked: true,
      storyboardStatus: "ready",
      segmentCount: 3,
      doneSegmentCount: 3,
    });
    expect(episodeMatrix(base).final.state).toBe("todo");
    expect(episodeMatrix({ ...base, final: FINAL }).final.state).toBe("done");
    expect(episodeMatrix({ ...base, final: FINAL, finalStale: true }).final.state).toBe("part");
  });
});

describe("总览：整体完成度", () => {
  test("没有分集：0%", () => {
    expect(overallProgress([])).toMatchObject({ percent: 0, scripts: 0, broken: 0, finals: 0 });
  });

  test("五道工序各占 1/5，片段按出视频比例折算", () => {
    const eps = [
      ep({
        no: 1,
        scriptStatus: "ready",
        locked: true,
        storyboardStatus: "ready",
        segmentCount: 4,
        doneSegmentCount: 4,
        final: FINAL,
      }),
      ep({ no: 2 }),
    ];
    // 第 1 集 5 项全满，第 2 集 0 项：5 / 10
    expect(overallProgress(eps)).toMatchObject({ percent: 50, scripts: 1, broken: 1, finals: 1 });
    const half = [
      ep({
        scriptStatus: "ready",
        locked: true,
        storyboardStatus: "ready",
        segmentCount: 4,
        doneSegmentCount: 2,
      }),
    ];
    // 剧本 1 + 拆解 1 + 分镜 1 + 片段 0.5 + 成片 0 = 3.5 / 5
    expect(overallProgress(half).percent).toBe(70);
  });
});

describe("总览：继续制作该去哪", () => {
  const done = (no: number) =>
    ep({
      no,
      scriptStatus: "ready",
      locked: true,
      storyboardStatus: "ready",
      segmentCount: 2,
      doneSegmentCount: 2,
      final: FINAL,
    });

  test("没有分集：没有可继续的", () => {
    expect(resumeTarget([])).toBeNull();
  });

  test("全部做完：没有可继续的", () => {
    expect(resumeTarget([done(1), done(2)])).toBeNull();
  });

  test("按集号从前往后，找第一个没做完的集，并指向它缺的那道工序", () => {
    expect(resumeTarget([done(1), ep({ no: 2 })])).toEqual({ stage: "script", ep: 2 });
    expect(resumeTarget([done(1), ep({ no: 2, scriptStatus: "ready" })])).toEqual({
      stage: "script",
      ep: 2,
    });
    expect(resumeTarget([done(1), ep({ no: 2, scriptStatus: "ready", locked: true })])).toEqual({
      stage: "studio",
      ep: 2,
    });
    expect(
      resumeTarget([
        ep({
          no: 1,
          scriptStatus: "ready",
          locked: true,
          storyboardStatus: "ready",
          segmentCount: 4,
          doneSegmentCount: 1,
        }),
      ]),
    ).toEqual({ stage: "studio", ep: 1 });
  });

  test("片段出齐但没有成片，或成片已过期：去成片页", () => {
    const base = {
      scriptStatus: "ready" as const,
      locked: true,
      storyboardStatus: "ready" as const,
      segmentCount: 2,
      doneSegmentCount: 2,
    };
    expect(resumeTarget([ep({ no: 3, ...base })])).toEqual({ stage: "final", ep: 3 });
    expect(resumeTarget([ep({ no: 3, ...base, final: FINAL, finalStale: true })])).toEqual({
      stage: "final",
      ep: 3,
    });
  });
});

describe("命令面板：选中一集跳到哪道工序", () => {
  test("分镜已生成：直接进片场；否则回剧本页（那里有拆解入口）", () => {
    expect(jumpStageFor(ep({ storyboardStatus: "ready" }))).toBe("studio");
    expect(jumpStageFor(ep({ storyboardStatus: "generating" }))).toBe("studio");
    expect(jumpStageFor(ep({ scriptStatus: "ready", locked: true }))).toBe("script");
    expect(jumpStageFor(ep())).toBe("script");
  });
});

describe("工序标签上的提醒点", () => {
  const job = (step: DramaJob["step"], status: DramaJob["status"] = "running"): DramaJob =>
    ({ id: step, step, status, episodes: [], progress: 0 }) as unknown as DramaJob;

  test("没有待办：不亮", () => {
    expect(stageHints([], [])).toEqual({});
  });

  test("有集还没剧本：剧本是待办；剧本在写（集在生成或任务在跑）是进行中，优先于待办", () => {
    expect(stageHints([ep()], [])).toEqual({ script: "todo" });
    expect(stageHints([ep({ scriptStatus: "generating" })], [])).toEqual({ script: "run" });
    expect(stageHints([ep()], [job("scripts")])).toEqual({ script: "run" });
    expect(stageHints([ep()], [job("analyze", "queued")])).toEqual({ script: "run" });
  });

  test("剧本写好还没拆解：角色与场景是待办；拆解任务在跑是进行中", () => {
    const ready = ep({ scriptStatus: "ready" });
    expect(stageHints([ready], [])).toEqual({ assets: "todo" });
    expect(stageHints([ready], [job("breakdown")])).toEqual({ assets: "run" });
    expect(stageHints([ready], [job("asset_prompts")])).toEqual({ assets: "run" });
  });

  test("分镜在生成：片场进行中；合成在跑：成片进行中", () => {
    const done = ep({ scriptStatus: "ready", locked: true });
    expect(stageHints([{ ...done, storyboardStatus: "generating" }], [])).toEqual({
      studio: "run",
    });
    expect(stageHints([done], [job("storyboard")])).toEqual({ studio: "run" });
    expect(stageHints([done], [job("compose")])).toEqual({ final: "run" });
  });

  test("已经结束的任务不算", () => {
    expect(stageHints([], [job("scripts", "succeeded"), job("compose", "failed")])).toEqual({});
  });
});

describe("scriptTabDoc：点顶部「剧本」切过来时打开什么", () => {
  test("当前集已经有剧本（写好、在写、写失败）：停在这一集", () => {
    expect(scriptTabDoc({ scriptStatus: "ready" })).toBe(EPISODE_DOC);
    expect(scriptTabDoc({ scriptStatus: "generating" })).toBe(EPISODE_DOC);
    expect(scriptTabDoc({ scriptStatus: "failed" })).toBe(EPISODE_DOC);
  });

  test("还没开始写剧本（研读、出大纲阶段）：打开大纲", () => {
    expect(scriptTabDoc({ scriptStatus: "empty" })).toBeUndefined();
  });

  test("还没有分集：打开大纲", () => {
    expect(scriptTabDoc(undefined)).toBeUndefined();
  });
});

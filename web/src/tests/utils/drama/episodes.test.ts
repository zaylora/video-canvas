import { describe, expect, test } from "bun:test";

import type { DramaJob, EpisodeSummary } from "@/api/drama/type";
import {
  breakdownOptions,
  breakdownSubmit,
  defaultBreakdownSelection,
  deleteEpisodeConfirm,
  defaultScriptSelection,
  scriptOptions,
  scriptSubmit,
  episodeAfterDelete,
  episodeEditState,
  episodePage,
  nextStep,
  pageOfEpisode,
  gapBefore,
  MAX_JOB_EPISODES,
  sourceRangeLabel,
  plannedEpisodes,
  selectByRange,
  editBlockReason,
  tocState,
} from "@/utils/drama/episodes";

const ep = (no: number, patch: Partial<EpisodeSummary> = {}): EpisodeSummary => ({
  no,
  title: `第${no}集`,
  synopsis: "",
  scriptStatus: "ready",
  locked: false,
  revision: 1,
  updatedAt: "x",
  ...patch,
});

const job = (patch: Partial<DramaJob> = {}): DramaJob => ({
  id: "j",
  step: "scripts",
  episodes: [2],
  status: "running",
  progress: 10,
  credits: 0,
  errorCode: 0,
  errorMessage: "",
  createdAt: "x",
  finishedAt: null,
  ...patch,
});

describe("锁定集只读判定", () => {
  test("锁定集不可编辑，原因是 locked", () => {
    expect(episodeEditState(ep(1, { locked: true }))).toEqual({
      editable: false,
      reason: "locked",
    });
  });

  test("生成中的集暂不可编辑，避免和模型输出互相覆盖", () => {
    expect(episodeEditState(ep(1, { scriptStatus: "generating" }))).toEqual({
      editable: false,
      reason: "generating",
    });
  });

  test("未生成 / 失败 / 已有剧本的集可以手写", () => {
    for (const scriptStatus of ["empty", "failed", "ready"] as const) {
      expect(episodeEditState(ep(1, { scriptStatus })).editable).toBe(true);
    }
  });

  test("目录状态图标：锁定 > 生成中 > 待生成 > 正常", () => {
    expect(tocState(ep(1, { locked: true, scriptStatus: "ready" }))).toBe("locked");
    expect(tocState(ep(1, { scriptStatus: "generating" }))).toBe("generating");
    expect(tocState(ep(1, { scriptStatus: "empty" }))).toBe("pending");
    expect(tocState(ep(1, { scriptStatus: "failed" }))).toBe("pending");
    expect(tocState(ep(1))).toBe("ready");
  });
});

describe("下一步浮条状态", () => {
  const base = { status: "ready" as const };

  test("研读中还没有集：不显示", () => {
    expect(nextStep({ ...base, status: "analyzing", episodes: [] }, [])).toEqual({ kind: "none" });
  });

  test("有集没剧本：生成分集剧本，范围是没剧本的集", () => {
    const step = nextStep(
      {
        ...base,
        episodes: [ep(1), ep(2, { scriptStatus: "empty" }), ep(3, { scriptStatus: "failed" })],
      },
      [],
    );
    expect(step).toEqual({ kind: "scripts", episodes: [2, 3], disabledReason: null });
  });

  test("剧本都齐了、还有没拆解的：资产拆解", () => {
    const step = nextStep({ ...base, episodes: [ep(1, { locked: true }), ep(2), ep(3)] }, []);
    expect(step).toEqual({ kind: "breakdown", episodes: [2, 3], disabledReason: null });
  });

  test("全部已拆解：不显示", () => {
    expect(
      nextStep({ ...base, episodes: [ep(1, { locked: true }), ep(2, { locked: true })] }, []),
    ).toEqual({ kind: "none" });
  });

  test("有进行中任务：禁用并写明原因", () => {
    const step = nextStep({ ...base, episodes: [ep(1), ep(2, { scriptStatus: "empty" })] }, [
      job({ step: "breakdown", episodes: [1] }),
    ]);
    expect(step.kind).toBe("scripts");
    expect(step.kind === "scripts" && step.disabledReason).toContain("资产拆解");
  });

  test("已结束的任务不禁用", () => {
    const step = nextStep({ ...base, episodes: [ep(1)] }, [job({ status: "succeeded" })]);
    expect(step).toEqual({ kind: "breakdown", episodes: [1], disabledReason: null });
  });

  test("有集正在生成剧本（任务列表还没同步）也禁用", () => {
    const step = nextStep(
      {
        ...base,
        episodes: [ep(1), ep(2, { scriptStatus: "generating" }), ep(3, { scriptStatus: "empty" })],
      },
      [],
    );
    expect(step.kind === "scripts" && step.disabledReason).toBeTruthy();
  });
});

describe("资产拆解对话框", () => {
  const episodes = [
    ep(1, { locked: true }),
    ep(2),
    ep(3, { scriptStatus: "empty" }),
    ep(4),
    ep(5, { scriptStatus: "generating" }),
  ];

  test("已锁定、没有剧本、剧本生成中的集都禁用并写明原因", () => {
    const options = breakdownOptions(episodes);
    expect(options.map((o) => [o.no, o.disabled, o.reason])).toEqual([
      [1, true, "已拆解"],
      [2, false, null],
      [3, true, "还没有剧本"],
      [4, false, null],
      [5, true, "还没有剧本"],
    ]);
  });

  test("默认选第一个可用的集", () => {
    expect(defaultBreakdownSelection(episodes)).toEqual([2]);
    expect(defaultBreakdownSelection([ep(1, { locked: true })])).toEqual([]);
  });

  test("提交条件：至少选一集；比例以项目为准，不再要求选比例", () => {
    expect(breakdownSubmit([])).toEqual({ ok: false, label: "至少选一集" });
    expect(breakdownSubmit([2, 4])).toEqual({ ok: true, label: "开始拆解" });
  });
});

describe("删除一集", () => {
  test("删的是当前看的这集：落到补位上来的下一集，删的是最后一集就落到前一集", () => {
    expect(episodeAfterDelete(2, 2, 3)).toBe(2);
    expect(episodeAfterDelete(3, 3, 2)).toBe(2);
  });

  test("删的在当前集前面：当前集号减 1，看的还是同一集", () => {
    expect(episodeAfterDelete(4, 2, 4)).toBe(3);
  });

  test("删的在当前集后面：不变", () => {
    expect(episodeAfterDelete(1, 3, 4)).toBe(1);
  });

  test("删完一集都不剩：回到 1（页面按「还没有分集」显示）", () => {
    expect(episodeAfterDelete(1, 1, 0)).toBe(1);
  });

  test("确认框：写明集号和标题；有片段视频的提醒会一起删，后面的集号往前补", () => {
    const plain = deleteEpisodeConfirm(ep(2, { title: "初见" }), 5);
    expect(plain.title).toBe("删除第 2 集「初见」？");
    expect(plain.description).toContain("第 3–5 集会依次变成第 2–4 集");
    expect(plain.description).not.toContain("片段");
    const withClips = deleteEpisodeConfirm(ep(5, { segmentCount: 3 }), 5);
    expect(withClips.description).toContain("3 个片段");
    expect(withClips.description).not.toContain("依次变成");
  });
});

describe("生成分集剧本对话框", () => {
  const eps = [
    ep(1, { scriptStatus: "ready", locked: true }),
    ep(2, { scriptStatus: "ready" }),
    ep(3, { scriptStatus: "empty" }),
    ep(4, { scriptStatus: "failed" }),
    ep(5, { scriptStatus: "generating" }),
  ];

  test("已拆解、生成中的集不能选；已有剧本的能选（重新生成会覆盖）", () => {
    expect(scriptOptions(eps)).toEqual([
      { no: 1, title: "第1集", disabled: true, reason: "已拆解", hasScript: true },
      { no: 2, title: "第2集", disabled: false, reason: null, hasScript: true },
      { no: 3, title: "第3集", disabled: false, reason: null, hasScript: false },
      { no: 4, title: "第4集", disabled: false, reason: null, hasScript: false },
      { no: 5, title: "第5集", disabled: true, reason: "生成中", hasScript: false },
    ]);
  });

  test("默认勾选还没有剧本的集（没有或失败），和原来一键生成的范围一样", () => {
    expect(defaultScriptSelection(eps)).toEqual([3, 4]);
  });

  test("提交按钮写明集数；没选不能提交；选了已有剧本的给出覆盖提醒", () => {
    expect(scriptSubmit([], eps)).toEqual({ ok: false, label: "至少选一集", overwrite: 0 });
    expect(scriptSubmit([3, 4], eps)).toEqual({ ok: true, label: "生成 2 集剧本", overwrite: 0 });
    expect(scriptSubmit([2, 3], eps)).toEqual({ ok: true, label: "生成 2 集剧本", overwrite: 1 });
  });
});

describe("总览进度表分页", () => {
  const eps = Array.from({ length: 25 }, (_, i) => ep(i + 1));

  test("每页 10 集，给出当前页的集和「第 a–b 集」范围", () => {
    const first = episodePage(eps, 1);
    expect(first.items.map((e) => e.no)).toEqual([1, 2, 3, 4, 5, 6, 7, 8, 9, 10]);
    expect(first).toMatchObject({ page: 1, pages: 3, from: 1, to: 10 });
    const last = episodePage(eps, 3);
    expect(last.items.map((e) => e.no)).toEqual([21, 22, 23, 24, 25]);
    expect(last).toMatchObject({ page: 3, pages: 3, from: 21, to: 25 });
  });

  test("页码越界收回到有效范围；没有集时只有一页", () => {
    expect(episodePage(eps, 9).page).toBe(3);
    expect(episodePage(eps, 0).page).toBe(1);
    expect(episodePage([], 1)).toMatchObject({ items: [], page: 1, pages: 1 });
  });

  test("某一集在第几页：按在列表里的位置算，找不到回第 1 页", () => {
    expect(pageOfEpisode(eps, 1)).toBe(1);
    expect(pageOfEpisode(eps, 10)).toBe(1);
    expect(pageOfEpisode(eps, 11)).toBe(2);
    expect(pageOfEpisode(eps, 99)).toBe(1);
  });
});

describe("只列已规划的集", () => {
  test("标题为空的集还没规划，不进选集列表", () => {
    const episodes = [ep(1), ep(2, { title: "" }), ep(3)];
    expect(plannedEpisodes(episodes).map((e) => e.no)).toEqual([1, 3]);
    expect(scriptOptions(episodes).map((o) => o.no)).toEqual([1, 3]);
    expect(breakdownOptions(episodes).map((o) => o.no)).toEqual([1, 3]);
  });
});

describe("按范围选：第 a–b 集", () => {
  const episodes = [
    ep(1),
    ep(2, { locked: true }),
    ep(3, { scriptStatus: "generating" }),
    ep(4, { scriptStatus: "empty" }),
    ep(5),
  ];

  test("写剧本：范围内除已拆解、生成中的集都选上", () => {
    expect(selectByRange(scriptOptions(episodes), 1, 5)).toEqual([1, 4, 5]);
    expect(selectByRange(scriptOptions(episodes), 4, 5)).toEqual([4, 5]);
  });

  test("拆解：只选有剧本、未锁定的；a > b 时自动对调；越界被截断", () => {
    expect(selectByRange(breakdownOptions(episodes), 5, 1)).toEqual([1, 5]);
    expect(selectByRange(breakdownOptions(episodes), 0, 999)).toEqual([1, 5]);
    expect(selectByRange(breakdownOptions(episodes), 7, 9)).toEqual([]);
  });
});

describe("写剧本的衔接提示", () => {
  const episodes = [
    ep(1),
    ep(2, { scriptStatus: "empty" }),
    ep(3, { scriptStatus: "empty" }),
    ep(4, { scriptStatus: "empty" }),
  ];

  test("选中集的上一集没剧本、也不在选择里，就提示（返回选中的集号）", () => {
    expect(gapBefore([3], episodes)).toEqual([3]);
    expect(gapBefore([4], episodes)).toEqual([4]);
  });

  test("上一集有剧本、或本次一起写、或是第 1 集，不提示", () => {
    expect(gapBefore([2], episodes)).toEqual([]);
    expect(gapBefore([2, 3, 4], episodes)).toEqual([]);
    expect(gapBefore([1], episodes)).toEqual([]);
  });

  test("上一集已拆解（锁定）也算有剧本", () => {
    expect(gapBefore([2], [ep(1, { locked: true, scriptStatus: "ready" }), ep(2)])).toEqual([]);
  });
});

describe("规划中禁止增删集", () => {
  test("规划中写明原因；暂停 / 完成后恢复", () => {
    expect(editBlockReason("running", [])).toBe("正在规划，完成或暂停后再操作");
    expect(editBlockReason("paused", [])).toBeNull();
    expect(editBlockReason("done", [])).toBeNull();
  });
});

describe("对应原文", () => {
  test("单个单元「第 3 章」，跨单元「第 3–5 章」，单位跟着单元种类，没关联为 null", () => {
    expect(sourceRangeLabel(3, 3, "chapter")).toBe("第 3 章");
    expect(sourceRangeLabel(3, 5, "chapter")).toBe("第 3–5 章");
    expect(sourceRangeLabel(8, null, "scene")).toBe("第 8 场");
    expect(sourceRangeLabel(2, 2, "episode")).toBe("第 2 集");
    expect(sourceRangeLabel(null, null, "chapter")).toBeNull();
  });
});

describe("一次最多 200 集", () => {
  const many = Array.from({ length: 450 }, (_, i) => ep(i + 1, { scriptStatus: "empty" }));

  test("默认选择最多取前 200 个没有剧本的集", () => {
    expect(MAX_JOB_EPISODES).toBe(200);
    expect(defaultScriptSelection(many)).toHaveLength(200);
    expect(defaultScriptSelection(many)[199]).toBe(200);
  });

  test("超过 200 集不能提交，按钮写明上限", () => {
    const over = many.slice(0, 201).map((e) => e.no);
    expect(scriptSubmit(over, many)).toMatchObject({ ok: false, label: "一次最多 200 集" });
    expect(scriptSubmit(over.slice(0, 200), many).ok).toBe(true);
    expect(breakdownSubmit(over)).toEqual({ ok: false, label: "一次最多 200 集" });
  });
});

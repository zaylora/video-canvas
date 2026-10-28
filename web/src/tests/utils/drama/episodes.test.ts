import { describe, expect, test } from "bun:test";

import type { DramaJob, EpisodeSummary } from "@/api/drama/type";
import {
  breakdownOptions,
  breakdownSubmit,
  defaultBreakdownSelection,
  episodeEditState,
  nextStep,
  ratioFixed,
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

  test("已有锁定集时比例固定不可改", () => {
    expect(ratioFixed(episodes)).toBe(true);
    expect(ratioFixed([ep(1), ep(2)])).toBe(false);
  });

  test("提交条件：比例必选、至少一集", () => {
    expect(breakdownSubmit("", [2])).toEqual({ ok: false, label: "先选比例" });
    expect(breakdownSubmit("9:16", [])).toEqual({ ok: false, label: "至少选一集" });
    expect(breakdownSubmit("9:16", [2, 4])).toEqual({ ok: true, label: "开始拆解" });
  });
});

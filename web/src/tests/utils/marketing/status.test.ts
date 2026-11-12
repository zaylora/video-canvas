import { describe, expect, test } from "bun:test";

import type { ProjectSummary, StepRun } from "@/api/marketing/type";
import { projectStatusLine, splitStepLevels } from "@/utils/marketing/status";

const item = (patch: Partial<ProjectSummary> = {}): ProjectSummary => ({
  id: "a",
  title: "t",
  coverUrl: "",
  scripting: false,
  rendering: 0,
  lastRoundFailed: false,
  scriptCounts: { draft: 0, rendering: 0, done: 0, failed: 0 },
  bars: [],
  updatedAt: "2026-11-11T10:00:00Z",
  ...patch,
});

describe("列表卡状态行", () => {
  test("写脚本中最优先，其次出片中、写脚本失败", () => {
    expect(projectStatusLine(item({ scripting: true, rendering: 2 }))).toEqual({
      text: "写脚本中",
      tone: "busy",
    });
    expect(projectStatusLine(item({ rendering: 1, lastRoundFailed: true })).text).toBe("出片中");
    expect(projectStatusLine(item({ lastRoundFailed: true }))).toEqual({
      text: "写脚本失败",
      tone: "bad",
    });
  });

  test("有成片显示条数，否则显示待出片的脚本份数", () => {
    expect(
      projectStatusLine(item({ scriptCounts: { draft: 1, rendering: 0, done: 2, failed: 0 } }))
        .text,
    ).toBe("2 条成片");
    expect(
      projectStatusLine(item({ scriptCounts: { draft: 2, rendering: 0, done: 0, failed: 1 } }))
        .text,
    ).toBe("3 份脚本待出片");
  });
});

describe("环节进度条分层", () => {
  const run = (step: StepRun["step"]) => ({ step }) as StepRun;
  test("事实卡与拆解一层，跟拍与创意一层，没跑的环节不出现", () => {
    const [first, second] = splitStepLevels([run("create"), run("facts"), run("follow")]);
    expect(first.map((s) => s.step)).toEqual(["facts"]);
    expect(second.map((s) => s.step)).toEqual(["follow", "create"]);
  });
});

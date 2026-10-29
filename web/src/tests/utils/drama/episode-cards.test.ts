import { describe, expect, test } from "bun:test";

import type { EpisodeSummary } from "@/api/drama/type";
import {
  episodeCardView,
  storyboardStart,
  type EpisodeCardContext,
} from "@/utils/drama/episode-cards";

const ep = (patch: Partial<EpisodeSummary> = {}): EpisodeSummary => ({
  no: 1,
  title: "村里的女人",
  synopsis: "",
  scriptStatus: "ready",
  locked: true,
  revision: 1,
  updatedAt: "x",
  storyboardStatus: "empty",
  segmentCount: 0,
  segmentSeconds: 0,
  doneSegmentCount: 0,
  ...patch,
});

const idle: EpisodeCardContext = { busyReason: null };

describe("分集卡片：状态与主按钮判定（原型 epCard）", () => {
  test("没拆解资产：去拆解，提示需先资产拆解", () => {
    const view = episodeCardView(ep({ locked: false }), idle);
    expect(view.badge).toEqual({ tone: "warning", text: "需先资产拆解" });
    expect(view.action).toMatchObject({ kind: "breakdown", label: "去拆解", disabledReason: null });
  });

  test("已拆解、没有分镜：生成脚本（带积分），有进行中的流水线任务时禁用并写明原因", () => {
    const view = episodeCardView(ep(), idle);
    expect(view.badge.text).toBe("待生成脚本");
    expect(view.action).toMatchObject({ kind: "storyboard", label: "生成脚本", showCredits: true });
    const busy = episodeCardView(ep(), { busyReason: "有进行中的任务：分集剧本 · 3 集" });
    expect(busy.action.disabledReason).toBe("有进行中的任务：分集剧本 · 3 集");
  });

  test("分镜生成中：按钮禁用显示生成中", () => {
    const view = episodeCardView(ep({ storyboardStatus: "generating" }), idle);
    expect(view.badge).toMatchObject({ tone: "running", text: "生成脚本中" });
    expect(view.action).toMatchObject({ kind: "generating", label: "生成中…" });
    expect(view.action.disabledReason).not.toBeNull();
  });

  test("分镜失败：可以重新生成", () => {
    const view = episodeCardView(ep({ storyboardStatus: "failed" }), idle);
    expect(view.badge.tone).toBe("destructive");
    expect(view.action).toMatchObject({ kind: "storyboard", label: "重新生成脚本" });
  });

  test("分镜就绪：进入制作，徽标显示已出视频的片段数，全部出完变成功色", () => {
    const partial = episodeCardView(
      ep({ storyboardStatus: "ready", segmentCount: 4, segmentSeconds: 50, doneSegmentCount: 1 }),
      idle,
    );
    expect(partial.badge).toEqual({ tone: "running", text: "1/4 片段已出视频" });
    expect(partial.action).toMatchObject({
      kind: "enter",
      label: "进入制作",
      disabledReason: null,
    });
    const all = episodeCardView(
      ep({ storyboardStatus: "ready", segmentCount: 4, doneSegmentCount: 4 }),
      idle,
    );
    expect(all.badge.tone).toBe("success");
  });

  test("分镜就绪但流水线忙时仍可进入制作（制作页不走文本流水线）", () => {
    const view = episodeCardView(ep({ storyboardStatus: "ready", segmentCount: 2 }), {
      busyReason: "有进行中的任务：资产拆解 · 第 2 集",
    });
    expect(view.action.disabledReason).toBeNull();
  });
});

describe("重新生成分镜前是否要先确认（67013）", () => {
  test("没有片段：直接生成", () => {
    expect(storyboardStart(ep())).toEqual({ confirm: false });
  });

  test("有片段但没有任何采用视频：后端会直接替换，不用确认", () => {
    expect(storyboardStart(ep({ storyboardStatus: "ready", segmentCount: 4 }))).toEqual({
      confirm: false,
    });
  });

  test("已有出视频的片段：要先确认，会清空已有片段", () => {
    const result = storyboardStart(
      ep({ storyboardStatus: "ready", segmentCount: 4, doneSegmentCount: 2 }),
    );
    expect(result.confirm).toBe(true);
  });
});

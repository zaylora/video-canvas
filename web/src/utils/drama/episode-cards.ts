import type { EpisodeSummary } from "@/api/drama/type";

/** 卡片状态徽标的色调 */
export type BadgeTone = "success" | "running" | "warning" | "destructive" | "muted";

export type EpisodeCardAction = {
  /** breakdown 去拆解（切到剧本区）；storyboard 生成脚本；enter 进入片段工作区；generating 生成中 */
  kind: "breakdown" | "storyboard" | "enter" | "generating";
  label: string;
  /** 带积分估价的主按钮 */
  showCredits: boolean;
  /** 不能点的原因；可点为 null */
  disabledReason: string | null;
};

export type EpisodeCardView = {
  badge: { tone: BadgeTone; text: string };
  action: EpisodeCardAction;
};

export type EpisodeCardContext = {
  /** 同一部短剧同时只能跑一个流水线任务：有进行中的就是原因，否则 null */
  busyReason: string | null;
};

/**
 * 分集卡片的状态徽标与主按钮（设计 §6.3、原型「分集」）：
 * 没拆解资产 → 去拆解；已拆解没分镜 → 生成脚本（带积分）；生成中 → 禁用；失败 → 重新生成；就绪 → 进入制作。
 * 生成脚本走文本流水线，有进行中的任务时禁用；进入制作不受影响。
 */
export function episodeCardView(
  episode: EpisodeSummary,
  context: EpisodeCardContext,
): EpisodeCardView {
  switch (episode.storyboardStatus) {
    case "ready": {
      const all = episode.segmentCount > 0 && episode.doneSegmentCount >= episode.segmentCount;
      return {
        badge: {
          tone: all ? "success" : "running",
          text: `${episode.doneSegmentCount}/${episode.segmentCount} 片段已出视频`,
        },
        action: { kind: "enter", label: "进入制作", showCredits: false, disabledReason: null },
      };
    }
    case "generating":
      return {
        badge: { tone: "running", text: "生成脚本中" },
        action: {
          kind: "generating",
          label: "生成中…",
          showCredits: false,
          disabledReason: "分镜脚本生成中",
        },
      };
    default:
      break;
  }
  if (!episode.locked) {
    return {
      badge: { tone: "warning", text: "需先资产拆解" },
      action: { kind: "breakdown", label: "去拆解", showCredits: false, disabledReason: null },
    };
  }
  const failed = episode.storyboardStatus === "failed";
  return {
    badge: failed
      ? { tone: "destructive", text: "脚本生成失败" }
      : { tone: "muted", text: "待生成脚本" },
    action: {
      kind: "storyboard",
      label: failed ? "重新生成脚本" : "生成脚本",
      showCredits: true,
      disabledReason: context.busyReason,
    },
  };
}

/**
 * 重新生成分镜前要不要先确认：已有出视频的片段 → 要（会清空已有片段，后端对应 67013）；
 * 没有视频的片段后端直接替换。仍可能撞上 67013（失败的 take 也算），提交时再兜一次。
 */
export const storyboardStart = (episode: EpisodeSummary): { confirm: boolean } => ({
  confirm: episode.doneSegmentCount > 0,
});

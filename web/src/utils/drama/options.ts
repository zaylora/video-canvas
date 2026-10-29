import type { DramaRatio, DramaStatus } from "@/api/drama/type";

/** 成片比例（宽、高用来画小方块示意） */
export const RATIOS: { key: Exclude<DramaRatio, "">; w: number; h: number }[] = [
  { key: "9:16", w: 9, h: 16 },
  { key: "16:9", w: 16, h: 9 },
  { key: "21:9", w: 21, h: 9 },
  { key: "3:4", w: 3, h: 4 },
  { key: "4:3", w: 4, h: 3 },
  { key: "1:1", w: 1, h: 1 },
];

/** 目标集数档位；0 = 自动（按剧本已有分集） */
export const TARGET_EPISODE_OPTIONS = [0, 10, 16, 30, 60] as const;

/** 单集时长档位（秒） */
export const EPISODE_SECOND_OPTIONS = [60, 90, 120, 180] as const;

export const DEFAULT_EPISODE_SECONDS = 60;

/** 自定义画风的 style_key */
export const CUSTOM_STYLE_KEY = "custom";

/** 新建时默认选中的画风；清单里没有就取第一个 */
export const DEFAULT_STYLE_KEY = "real_city_film";

/** 原文字数上限（契约 §3.1） */
export const SOURCE_MAX_CHARS = 300_000;

/** 上传支持的扩展名 */
export const SOURCE_EXTENSIONS = ["txt", "md", "docx"] as const;

/** 文件名的扩展名，小写，没有返回空串 */
export const extOf = (name: string): string => {
  const i = name.lastIndexOf(".");
  return i < 0 ? "" : name.slice(i + 1).toLowerCase();
};

/**
 * 上传文件的本地预检：只拦显然不对的格式（给 PDF 一句明确的话），
 * 内容为空、超长等以后端 67002 / 67003 为准。通过返回 null。
 */
export function checkSourceFile(file: Pick<File, "name">): string | null {
  const ext = extOf(file.name);
  if (ext === "pdf") return "PDF 暂不支持，请转成 txt / md / docx 后再上传";
  if (!(SOURCE_EXTENSIONS as readonly string[]).includes(ext)) {
    return "仅支持 txt / md / docx 文件";
  }
  return null;
}

/** 粘贴文本的本地预检：空白 / 超长 */
export function checkSourceText(text: string): string | null {
  if (text.trim().length === 0) return "内容为空，先粘贴剧本或小说正文";
  if (text.length > SOURCE_MAX_CHARS) return "内容超过 30 万字，请删减后再试";
  return null;
}

/** 目标集数的展示 */
export const targetEpisodesLabel = (n: number): string => (n > 0 ? `${n} 集` : "自动集数");

/** 短剧状态的展示文案与是否进行中 */
export function dramaStatusLabel(status: DramaStatus): { text: string; busy: boolean } {
  switch (status) {
    case "analyzing":
      return { text: "研读中", busy: true };
    case "outlined":
      return { text: "大纲已出", busy: false };
    case "scripting":
      return { text: "写剧本中", busy: true };
    case "ready":
      return { text: "剧本已就绪", busy: false };
    case "failed":
      return { text: "失败待重试", busy: false };
  }
}

/** 列表卡下的三步进度点（剧本 · 资产 · 视频）：0 未开始 / 0.5 进行中 / 1 完成 */
export function dramaProgress(summary: {
  status: DramaStatus;
  episodeCount: number;
  lockedCount: number;
}): [number, number, number] {
  const script = summary.status === "ready" ? 1 : summary.episodeCount > 0 ? 0.5 : 0;
  const assets =
    summary.lockedCount <= 0
      ? 0
      : summary.episodeCount > 0 && summary.lockedCount >= summary.episodeCount
        ? 1
        : 0.5;
  return [script, assets, 0];
}

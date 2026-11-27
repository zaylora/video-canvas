import type { BoardLayout } from "@/api/drama/type";
import type { CanvasNodeData, NodeOutput } from "@/types";
import { activeOutputIdOf, readOutputs } from "@/utils/canvas/outputs";

import { BOARD_SHOTS_PLACEHOLDER } from "./placeholders";

/**
 * 故事板：每个片段一张宫格分镜图（docs/design/故事板/故事板.md）。
 * 格子按原稿【时间轴】的镜头一一对应，生视频时采用版作为第一张参考图。
 */

/** 故事板固定横版 16:9，不跟短剧画幅 */
export const BOARD_RATIO = "16:9";

/** 一个宫格最多几格；镜头再多就合并进最后一格 */
const MAX_CELLS = 9;

/** 提示词正文的语言：说明行跟着它走，H3 转写稿是英文，原稿和通用转写稿是中文 */
export type PromptLang = "zh" | "en";

/**
 * 判断提示词正文是中文还是英文：先去掉对白（`<d>…</d>`）、资产引用（`@[名](id)`）和引号里的原文，
 * 这些在英文稿里也保留中文；剩下的拉丁字母比汉字多三倍以上算英文，其余（含空串）算中文。
 */
export function promptLang(prompt: string): PromptLang {
  const body = prompt
    .replace(/<d>[\s\S]*?<\/d>/g, "")
    .replace(/@\[[^\]]*\]\([^)]*\)/g, "")
    .replace(/"[^"\n]*"|“[^”\n]*”/g, "");
  const han = body.match(/\p{Script=Han}/gu)?.length ?? 0;
  const latin = body.match(/[A-Za-z]/g)?.length ?? 0;
  return latin > han * 3 ? "en" : "zh";
}

/**
 * 生视频时加在提示词最前面的一行（越要紧的素材说明越靠前），告诉模型图片1是什么、格子和镜头怎么对应、只借什么。
 * - 格子按从左到右、从上到下对应镜头；镜头比格子少时点明多出的格子不用，免得模型多演一镜；
 * - 宫格里的人物形象常常不一致，形象以角色 / 场景参考图和文字为准，宫格只借构图、站位、景别和衔接；
 * - 宫格图固定横版，成片画幅不同就要求重新构图，不把横版画面硬塞进竖屏；
 * - 故事板模板可能是手绘线稿也可能是写实（内置模板、老项目快照、「我的」模板），所以不写死画法，
 *   只说成片不带宫格线、编号、批注，也不沿用宫格图本身的笔触和色调，画风按提示词里写的；
 * - 语言跟提示词正文一致（英文稿配英文说明），中英两版意思相同。
 * @param shots 出图时数出的镜头数；0 = 没识别到时间轴
 * @param ratio 成片画幅（短剧比例）
 * @param lang 提示词正文的语言，默认中文
 */
export function boardLine(shots: number, ratio: string, lang: PromptLang = "zh"): string {
  const { cols, rows } = boardLayout(shots);
  const cells = cols * rows;
  const counted = shots > 0 && shots <= MAX_CELLS;
  const spare = counted && shots < cells;
  const reframe = ratio && ratio !== BOARD_RATIO ? ratio : "";
  if (lang === "en") {
    const order = !counted
      ? "the panels correspond to the shots below in order"
      : shots === 1
        ? "panel 1 corresponds to the shot below"
        : `panels 1-${shots} correspond to shots 1-${shots} below`;
    const rest = !spare
      ? ""
      : shots + 1 === cells
        ? `; ignore panel ${cells}`
        : `; ignore panels ${shots + 1}-${cells}`;
    return (
      `Image 1 is the storyboard grid for this segment. Read it left to right, top to bottom: ${order}${rest}. ` +
      "Use each panel only for composition, character blocking, shot size and continuity between shots; " +
      "character appearance, costumes and set details follow the other reference images and the text. " +
      (reframe ? `Reframe for ${reframe}. ` : "") +
      "The video is continuous footage in the style described below, with no grid lines, borders, panel numbers or annotation text, " +
      "and does not copy the storyboard's own linework or colors."
    );
  }
  const order = !counted
    ? "各格依次对应下面的各个镜头"
    : shots === 1
      ? "第1格对应下面的镜头"
      : `第1到第${shots}格依次对应下面的第1到第${shots}个镜头`;
  const rest = !spare
    ? ""
    : `，${shots + 1 === cells ? `第${cells}格` : `第${shots + 1}到第${cells}格`}不用`;
  return (
    `图片1是本段的分镜宫格图，按从左到右、从上到下的编号顺序，${order}${rest}；` +
    "只参考每格的构图、人物站位、景别和镜头衔接，人物形象、服装和场景细节以其他参考图和文字描述为准。" +
    `${reframe ? `成片按 ${reframe} 画幅重新构图，` : "成片"}是完整连续的画面，画风按下面写的生成，` +
    "不出现宫格线、边框、格子编号和批注文字，也不沿用宫格图本身的笔触和色调。"
  );
}

/** 【时间轴】里一个镜头行：「0-3秒：」「6-9.5秒：」开头 */
const SHOT_RE = /^\s*\d+(?:\.\d+)?\s*[-–~]\s*\d+(?:\.\d+)?\s*秒\s*[:：]\s*(.*)$/;
/** 不画进宫格的句子：台词、对白、旁白、声音 */
const SPOKEN_RE = /^(台词|对白|旁白|画外音|声音|音效|环境声)/;
/**
 * 一句台词：「说话人（语气）：『原文』」，说话人从上一个句读之后算起。
 * 整对引号一起匹配，引号里的句号不会把台词切成两半；语气括号（全角、半角）整个算进说话人，
 * 括号里的逗号（「压低声，迟疑」）不会被当成句读把说话人截断。
 */
const SPEECH_RE = /(?:[^。！？!?；;，,（(]|[（(][^）)]*[）)])*[:：]\s*『[^』]*』/g;

/** 取某个分栏（【名】开头那一行）冒号后的正文；没有为空串 */
function section(prompt: string, name: string): string {
  const line = prompt.split("\n").find((item) => item.trim().startsWith(`【${name}】`));
  return line
    ? line
        .trim()
        .slice(name.length + 2)
        .trim()
    : "";
}

/**
 * 去掉一镜里的台词和声音，只留看得见的画面。
 * 先整句去掉台词（换成句号，台词后面的终点画面另起一句），再按句号切开去掉声音句；
 * 仍带引号的句子（没有说话人的引语）整句不要。
 */
function visual(text: string): string {
  return text
    .replace(SPEECH_RE, "。")
    .split("。")
    .map((part) => part.trim().replace(/[，,、；;]+$/, ""))
    .filter((part) => part && !/[『』]/.test(part) && !SPOKEN_RE.test(part))
    .map((part) => `${part}。`)
    .join("");
}

/**
 * 原稿【时间轴】里的镜头画面，按先后排列；时间、台词和声音都去掉。
 * 没有时间轴分栏为空数组。
 */
export function shotLines(prompt: string): string[] {
  const lines = prompt.split("\n");
  const start = lines.findIndex((line) => line.trim().startsWith("【时间轴】"));
  if (start < 0) return [];
  const out: string[] = [];
  for (const line of lines.slice(start + 1)) {
    if (line.trim().startsWith("【")) break;
    const match = line.match(SHOT_RE);
    if (match) out.push(visual(match[1]));
  }
  return out;
}

/**
 * 按镜头数选宫格（整图 16:9 横版）：1–4 镜 2x2；5–6 镜 3 列 2 行；7 镜以上 3x3。
 * 识别不到镜头（0）也按 2x2。
 */
export function boardLayout(shots: number): { layout: BoardLayout; cols: number; rows: number } {
  if (shots <= 4) return { layout: "2x2", cols: 2, rows: 2 };
  if (shots <= 6) return { layout: "3x2", cols: 3, rows: 2 };
  return { layout: "3x3", cols: 3, rows: 3 };
}

/**
 * 拼宫格图的图片提示词（不调文本模型）：把原稿填进项目设置里选的故事板模板（内置或「我的」）。
 * 模板占位符：{{格数}} {{列数}} {{行数}} {{比例}}，以及 {{画面}} = 原稿的画面风格、场景与光、人物与道具、站位 + 逐镜画面。
 * 模板里没有 {{画面}}（后端保存时会拦，这里兜底）就把画面接在最后，镜头内容不丢。
 * 原稿里的 `@[名](节点id)` 原样保留，组装 input 时换成参考图编号。
 * @param prompt 片段原稿
 * @param template 故事板模板正文；空串只放画面
 * @returns text 提示词；layout 布局；shotCount 数出的镜头数；notice 给用户看的提醒（没有为空串）
 */
export function boardPrompt(
  prompt: string,
  template: string,
): {
  text: string;
  layout: BoardLayout;
  shotCount: number;
  notice: string;
} {
  const shots = shotLines(prompt);
  const { layout, cols, rows } = boardLayout(shots.length);
  const cells = cols * rows;
  let notice = "";
  let body: string;
  if (shots.length === 0) {
    notice = "没识别到时间轴，按 4 格生成";
    body = `按下面的片段内容拆成 ${cells} 个镜头：\n${prompt.trim()}`;
  } else {
    const shown =
      shots.length > MAX_CELLS
        ? [...shots.slice(0, MAX_CELLS - 1), shots.slice(MAX_CELLS - 1).join("；")]
        : shots;
    if (shots.length > MAX_CELLS) {
      notice = `镜头过多（${shots.length} 个），第 9 格合并了剩下的镜头，建议拆段`;
    }
    body = [
      ...[
        ["画面风格", section(prompt, "画幅风格")],
        ["场景与光", section(prompt, "场景与光")],
        ["人物与道具", section(prompt, "参考与人物")],
        ["站位", section(prompt, "站位")],
      ]
        .filter(([, value]) => value)
        .map(([name, value]) => `${name}：${value}`),
      "镜头：",
      ...shown.map((shot, index) => `${index + 1}. ${shot}`),
    ].join("\n");
  }
  const values: Record<string, string> = {
    格数: String(cells),
    列数: String(cols),
    行数: String(rows),
    比例: BOARD_RATIO,
  };
  const base = template.trim() || BOARD_SHOTS_PLACEHOLDER;
  const filled = base.split(BOARD_SHOTS_PLACEHOLDER).join("\u0000");
  const text = filled
    .replace(/\{\{(格数|列数|行数|比例)\}\}/g, (_, name: string) => values[name])
    .split("\u0000")
    .join(body);
  return {
    text: base.includes(BOARD_SHOTS_PLACEHOLDER) ? text : `${text}\n${body}`,
    layout,
    shotCount: shots.length,
    notice,
  };
}

/** 故事板节点的 drama.board：属于哪个片段、出图时的原稿哈希、宫格信息（前端写入，后端据此数故事板） */
export type NodeBoardMeta = {
  segmentId: string;
  /** 出图时原稿的 SHA-256；和当前原稿不同即已过期 */
  sourceHash: string;
  layout: BoardLayout;
  shotCount: number;
};

/** 画布上一个片段的故事板节点（读自画布，不另存） */
export type BoardNode = NodeBoardMeta & {
  nodeId: string;
  /** 当前那一版图片；还没出图为 null。重新生成时仍是上一版 */
  src: string | null;
  assetId?: string;
  status: "idle" | "running" | "error" | "done";
  /** 失败原因 */
  error: string;
  /** 历次出图（旧的在前）；旧节点只有一张图时当唯一一版，空节点为空数组 */
  versions: NodeOutput[];
  /** 当前版本的 id（版本里选中的那一版）；没有版本为 null */
  activeId: string | null;
  /** 节点上绑着的生成任务 id；生成中的故事板据此订阅实时进度，没有为 null */
  taskId: string | null;
};

/** 读故事板只用到的节点字段：编辑器里的节点和接口读到的画布节点都满足 */
type BoardNodeData = {
  drama?: CanvasNodeData["drama"] | null;
  src?: string | null;
  assetId?: string | null;
  status?: string | null;
  uploadProgress?: number | null;
  error?: string | null;
  outputs?: NodeOutput[] | null;
  activeOutputId?: string | null;
  mediaType?: CanvasNodeData["mediaType"];
  taskId?: string | null;
  model?: string | null;
};

/** 节点数据里的 drama.board；不是故事板节点为 null */
export function boardMetaOf(data: BoardNodeData): NodeBoardMeta | null {
  const board = data.drama?.board as Partial<NodeBoardMeta> | undefined;
  if (!board || typeof board.segmentId !== "string" || !board.segmentId) return null;
  return {
    segmentId: board.segmentId,
    sourceHash: typeof board.sourceHash === "string" ? board.sourceHash : "",
    layout: board.layout ?? "2x2",
    shotCount: typeof board.shotCount === "number" ? board.shotCount : 0,
  };
}

/** 是不是故事板节点：资产列表、@ 候选都不收它 */
export const isBoardNode = (data: BoardNodeData): boolean => boardMetaOf(data) !== null;

/** 画布节点里的故事板，按片段 id 取；组节点略过，同一片段有多个时取后面的 */
export function boardsFromGraph(
  nodes: readonly { id: string; type?: string; data: object }[],
): Map<string, BoardNode> {
  const out = new Map<string, BoardNode>();
  for (const node of nodes) {
    if (node.type === "group") continue;
    const data = node.data as BoardNodeData;
    const meta = boardMetaOf(data);
    if (!meta) continue;
    const src = data.src && !data.src.startsWith("blob:") ? data.src : null;
    const status =
      data.status === "running" ||
      (data.uploadProgress !== undefined && data.uploadProgress !== null)
        ? "running"
        : data.status === "error"
          ? "error"
          : src
            ? "done"
            : "idle";
    const nodeData = data as CanvasNodeData;
    const versions = readOutputs(nodeData);
    out.set(meta.segmentId, {
      ...meta,
      versions,
      activeId: activeOutputIdOf(nodeData) ?? null,
      taskId: data.taskId || null,
      nodeId: node.id,
      src,
      assetId: src ? (data.assetId ?? undefined) : undefined,
      status,
      error: data.error ?? "",
    });
  }
  return out;
}

/**
 * 当前版本往前 / 往后一版的 id（左右滑动换版用）；到头或只有一版返回 null。
 * 当前版本不在历史里（节点数据异常）按最后一版算。
 * @param versions 历次出图，旧的在前
 * @param activeId 当前版本的 id
 * @param delta 1 往后（更新的一版），-1 往前
 */
export function neighborVersion(
  versions: readonly { id: string }[],
  activeId: string | null,
  delta: 1 | -1,
): string | null {
  const at = versions.findIndex((item) => item.id === activeId);
  const next = (at < 0 ? versions.length - 1 : at) + delta;
  return versions[next]?.id ?? null;
}

/** 一个片段的故事板状态：empty 没有 / running 在出图 / failed 失败 / stale 原稿改过 / ready 可用 */
export type BoardStatus = "empty" | "running" | "failed" | "stale" | "ready";

/**
 * @param board 片段的故事板节点；没有为 undefined
 * @param hash 片段当前原稿的哈希；还没算出来为 undefined（先按没过期）
 */
export function boardStatus(board: BoardNode | undefined, hash: string | undefined): BoardStatus {
  if (!board) return "empty";
  if (board.status === "running") return "running";
  if (board.status === "error") return "failed";
  if (!board.src) return "empty";
  return hash !== undefined && hash !== board.sourceHash ? "stale" : "ready";
}

/** 生视频时能带入的故事板：节点有图（生成中沿用上一版）；没有为 null。shotCount 给说明行写格子对应 */
export type BoardRef = { assetId: string; src: string; stale: boolean; shotCount: number };

export function boardRef(board: BoardNode | undefined, hash: string | undefined): BoardRef | null {
  if (!board?.src || !board.assetId) return null;
  return {
    assetId: board.assetId,
    src: board.src,
    stale: hash !== undefined && hash !== board.sourceHash,
    shotCount: board.shotCount,
  };
}

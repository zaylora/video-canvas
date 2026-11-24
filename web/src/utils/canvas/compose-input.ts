import type { CanvasNodeData } from "@/types";
import type { ComposeCandidate } from "@/utils/storyboard/compose";
import { activeOutputIdOf, readOutputs } from "./outputs";

/** 一条连进成片节点的上游视频：位置决定它在成片里的次序 */
export type ComposeSource = {
  /** 上游节点 id */
  id: string;
  /** 上游节点的数据；节点刚被删掉时可能取不到 */
  data: CanvasNodeData | undefined;
  /** 画布坐标，只用来排序 */
  y: number;
  x: number;
};

/** 成片段落：按成片顺序排好的一段 */
export type ComposeRow = {
  /** 上游节点 id */
  id: string;
  /** 段号，从 0 起，就是成片里的先后 */
  seq: number;
  /** 上游节点标题，界面上分行列出来用 */
  label: string;
  /** 这一段对应的素材 id；还没出视频时为 undefined */
  assetId?: string;
};

/** 成片节点的输入：分段、能合成的片段、还没出视频的段号 */
export type ComposeInput = {
  /** 按成片顺序排好的段，界面上照这个顺序列 */
  rows: ComposeRow[];
  candidates: ComposeCandidate[];
  /** 连进来了但还没出视频的段号（从 0 起），合成时会被跳过 */
  missing: number[];
  /** 连进来的视频节点总数（= 成片段数上限） */
  total: number;
};

/**
 * 把连进成片节点的上游整理成「有序素材」。
 *
 * 默认顺序只认画布位置：先上下、再左右（y 小在前，同高看 x）。这不是暗箱推断 ——
 * 节点正文会把序号和标题逐行列出来，拖动上游节点就换了顺序，看到的就是最终的。
 * 用户点了上下移之后，`order`（节点 data 里的 composeOrder）说了算：
 * 在数组里的按数组先后排，不在数组里的接在后面、仍按位置 —— 新连进来的视频不必重排。
 *
 * 取素材走 activeOutputIdOf + readOutputs，和分镜表那条路径同一套兜底：
 * 老节点只有 src + assetId、没有 outputs，裸读 activeOutputId 会静默漏段。
 */
export function composeInputOf(
  sources: readonly ComposeSource[],
  order: readonly string[] = [],
): ComposeInput {
  const rank = new Map(order.map((id, index) => [id, index]));
  const byPosition = (a: ComposeSource, b: ComposeSource) =>
    a.y - b.y || a.x - b.x || a.id.localeCompare(b.id);

  const videos = sources
    .filter((item) => item.data?.kind === "video")
    .sort((a, b) => {
      const ra = rank.get(a.id);
      const rb = rank.get(b.id);
      if (ra === undefined && rb === undefined) return byPosition(a, b);
      if (ra === undefined) return 1;
      if (rb === undefined) return -1;
      return ra - rb;
    });

  const rows: ComposeRow[] = [];
  const candidates: ComposeCandidate[] = [];
  const missing: number[] = [];

  videos.forEach((item, seq) => {
    const data = item.data;
    const activeId = data ? activeOutputIdOf(data) : undefined;
    const output =
      data && activeId ? readOutputs(data).find((one) => one.id === activeId) : undefined;
    rows.push({ id: item.id, seq, label: data?.label ?? "视频", assetId: output?.assetId });
    if (output?.assetId) candidates.push({ seq, assetId: output.assetId });
    else missing.push(seq);
  });

  return { rows, candidates, missing, total: videos.length };
}

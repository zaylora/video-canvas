import type { CanvasNodeData, NodeStoryboard } from "@/types";
import { activeOutputIdOf, readOutputs } from "@/utils/canvas/outputs";

/** 一段待合成素材：seq 是分镜表的行号（从 0 起），只用于界面上说「第几镜」 */
export type ComposeCandidate = { seq: number; assetId: string };

/**
 * 按分镜表行序收集「已出视频」的片段。行序就是成片顺序，不接受任何空间推断。
 *
 * 取素材走 activeOutputIdOf + readOutputs，不用裸的 data.activeOutputId：
 * 旧画布的节点只有 src + assetId、没有 outputs，readOutputs 会兜底造一版，但 activeOutputId 是 undefined，
 * 直接用它会静默漏掉这一镜，成片少一段而界面看不出来。
 */
export function composeCandidates(
  board: NodeStoryboard,
  dataOf: (nodeId: string) => CanvasNodeData | undefined,
): ComposeCandidate[] {
  return composeCandidatesFrom(
    board,
    board.shots.map((shot) => (shot.videoNodeId ? dataOf(shot.videoNodeId) : undefined)),
  );
}

/**
 * 同 composeCandidates，但直接接收「与 shots 一一对应的节点数据」。
 * 弹窗里已经按行订阅读好了节点数据，不需要再按 id 反查一遍。
 */
export function composeCandidatesFrom(
  board: NodeStoryboard,
  datas: readonly (CanvasNodeData | undefined)[],
): ComposeCandidate[] {
  const out: ComposeCandidate[] = [];
  board.shots.forEach((_, index) => {
    const data = datas[index];
    if (!data || data.kind !== "video") return;
    const activeId = activeOutputIdOf(data);
    if (!activeId) return;
    const output = readOutputs(data).find((item) => item.id === activeId);
    if (!output?.assetId) return;
    out.push({ seq: index, assetId: output.assetId });
  });
  return out;
}

/** 没有出视频的镜头行号，合成时会被跳过 */
export function missingVideoSeqs(
  board: NodeStoryboard,
  dataOf: (nodeId: string) => CanvasNodeData | undefined,
): number[] {
  return missingVideoSeqsFrom(
    board,
    board.shots.map((shot) => (shot.videoNodeId ? dataOf(shot.videoNodeId) : undefined)),
  );
}

/** 同 missingVideoSeqs，接收与 shots 一一对应的节点数据 */
export function missingVideoSeqsFrom(
  board: NodeStoryboard,
  datas: readonly (CanvasNodeData | undefined)[],
): number[] {
  const ready = new Set(composeCandidatesFrom(board, datas).map((item) => item.seq));
  return board.shots.map((_, index) => index).filter((index) => !ready.has(index));
}

/** 把行号列表写成界面上的镜号：`[1, 3]` → 「02、04」 */
export const seqText = (seqs: readonly number[]): string =>
  seqs.map((seq) => String(seq + 1).padStart(2, "0")).join("、");

/** 毫秒转 mm:ss，成片时长展示用 */
export const mmss = (ms: number): string => {
  const total = Math.floor(ms / 1000);
  return `${String(Math.floor(total / 60)).padStart(2, "0")}:${String(total % 60).padStart(2, "0")}`;
};

/** 成片状态：none 还没有；fresh 最新；stale 之后镜头变过，可重新合成 */
export type ComposeState = "none" | "fresh" | "stale";

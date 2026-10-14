import type { TaskView } from "@/api/generation-task/type";
import type { CanvasNode, CanvasNodeData, FlowNode, NodeOutput } from "@/types";

import { findSlotsBeside } from "./duplicate";
import { MEDIA_KIND_OF, activeOutputIdOf, isGenerating, readOutputs } from "./outputs";

/** 节点右上角多版本徽标要显示的内容；不该显示返回 null */
export type VersionBadge = {
  /** 历史版本总数（不是本次数量） */
  count: number;
  /** 还有任务在跑：徽标旁带转圈 */
  generating: boolean;
};

/**
 * 多版本徽标：数字是「预计总数」= 已有的历史版本 + 还在生成的任务数，所以一提交 N 个任务就显示 N，
 * 陆续出图时总数不变，不是只数已成功的。总数 ≥2 才显示（只有 1 版、也没有第二张在路上的节点没有历史入口）；
 * 文本节点没有。
 */
export function showVersionBadge(data: CanvasNodeData): VersionBadge | null {
  if (data.kind === "script") return null;
  const generating = isGenerating(data);
  const inFlight = data.pendingTaskIds?.length ? data.pendingTaskIds.length : generating ? 1 : 0;
  const count = readOutputs(data).length + inFlight;
  return count >= 2 ? { count, generating } : null;
}

/** 单击设为主图能不能做：生成中（占位里还没有第一张）不行；已经是主图不用做 */
export function canSetMain(
  data: CanvasNodeData,
  outputId: string,
): { ok: true } | { ok: false; reason: string | null } {
  if (data.status === "running") return { ok: false, reason: "生成中，出第一张后可切换" };
  if (!readOutputs(data).some((item) => item.id === outputId)) return { ok: false, reason: null };
  // 失败后点当前那一版，等于把它找回来，照样放行
  if (outputId === activeOutputIdOf(data) && data.status === "done") {
    return { ok: false, reason: null };
  }
  return { ok: true };
}

/**
 * 把历史版本拆成独立节点（「添加到画布」）：每一版一个 done 节点，素材和只含这一版的历史都带上，
 * 不带提示词、连线和任务，也没有来源线；摆在源节点右侧，避开已有节点，新节点处于选中状态。
 * 名字是「原名 · 版本k」，k 是这一版在源节点历史里的序号（从 1 起，旧的在前）。
 */
export function buildVersionNodes(
  source: CanvasNode,
  nodes: FlowNode[],
  versions: NodeOutput[],
  newId: () => string = () => crypto.randomUUID(),
): CanvasNode[] {
  const all = readOutputs(source.data);
  const slots = findSlotsBeside(source, nodes, versions.length);
  return versions.map((output, i) => {
    const index = all.findIndex((item) => item.id === output.id);
    return {
      id: newId(),
      type: "canvas" as const,
      position: slots[i],
      selected: true,
      data: {
        kind: MEDIA_KIND_OF[output.mediaType],
        label: `${source.data.label} · 版本${(index >= 0 ? index : i) + 1}`,
        status: "done" as const,
        src: output.src,
        assetId: output.assetId,
        mediaType: output.mediaType,
        outputs: [output],
        activeOutputId: output.id,
      },
    };
  });
}

/** 一批任务的创建时间相差不会超过这么久（毫秒），超过的算更早的批次 */
const BATCH_WINDOW_MS = 30_000;

/**
 * 弹窗里要摆的「生成失败」格：这个节点最近一批里失败 / 过期的任务，附上给用户看的原因。
 * 失败任务不进节点历史，所以从任务库里找；比该节点最新任务早 30 秒以上的算旧批次，不再显示。
 */
export function recentFailedTasks(
  tasks: Record<string, TaskView>,
  nodeId: string,
): Array<{ id: string; message: string }> {
  const mine = Object.values(tasks).filter((task) => task.node_id === nodeId);
  const newest = Math.max(0, ...mine.map((task) => Date.parse(task.created_at) || 0));
  return mine
    .filter(
      (task) =>
        (task.status === "failed" || task.status === "expired") &&
        newest - (Date.parse(task.created_at) || 0) <= BATCH_WINDOW_MS,
    )
    .map((task) => ({
      id: String(task.id),
      message: task.error_message || (task.status === "expired" ? "生成超时" : "生成失败"),
    }));
}

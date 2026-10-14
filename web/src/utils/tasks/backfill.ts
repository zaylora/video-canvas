import type { TaskView } from "@/api/generation-task/type";
import type { CanvasNodeData, NodeOutput } from "@/types";
import { activeOutputIdOf, appendOutputs, readOutputs, selectOutput } from "@/utils/canvas/outputs";
import { isTerminalStatus, outputText, taskKey } from "./status";
import type { TaskMap } from "./merge";

/** 节点回填只关心这几项 */
type BackfillNode = { id: string; data: CanvasNodeData };

export type BackfillEvent = {
  nodeId: string;
  view: TaskView;
  patch: Partial<CanvasNodeData>;
};

const mediaTypeOf = (mediaType: string | undefined): "image" | "video" | "audio" => {
  const type = mediaType?.toLowerCase() ?? "";
  if (type.startsWith("image")) return "image";
  if (type.startsWith("audio")) return "audio";
  return "video";
};

/** 任务快照里的任务编号，没有为 null */
const taskRefOf = (view: TaskView): string | null => view.task_ref || null;

const EMPTY_MESSAGE = "生成结果为空，请重试";

/**
 * 任务的全部可用产物（一个任务可以返回多张，如豆包组图）：要有地址和素材 id，缺的成员跳过。
 * 都写成同一个任务产出的版本。
 */
function taskOutputs(data: CanvasNodeData, view: TaskView): NodeOutput[] {
  const now = Date.now();
  return (view.outputs ?? []).flatMap((output) => {
    if (!output.url || output.asset_id === null || output.asset_id === undefined) return [];
    const assetId = String(output.asset_id);
    return [
      {
        id: assetId,
        src: output.url,
        mediaType: mediaTypeOf(output.media_type),
        assetId,
        taskId: taskKey(view.id),
        model: data.model,
        createdAt: now,
      },
    ];
  });
}

/** 文本节点（script）：同步出正文，一个节点一个任务，没有批次 */
function planTextBackfill(data: CanvasNodeData, view: TaskView): Partial<CanvasNodeData> | null {
  if (!data.taskId || data.taskId !== taskKey(view.id)) return null;
  switch (view.status) {
    case "succeeded": {
      const text = outputText(view);
      if (text === undefined) {
        return data.status === "error" &&
          data.error === EMPTY_MESSAGE &&
          (data.errorTaskRef ?? null) === taskRefOf(view)
          ? null
          : { status: "error", error: EMPTY_MESSAGE, errorTaskRef: taskRefOf(view), text: null };
      }
      if (data.status === "done" && data.text === text) return null;
      return { status: "done", text, error: null };
    }
    case "failed":
    case "expired": {
      const message = view.error_message || (view.status === "expired" ? "生成超时" : "生成失败");
      const taskRef = taskRefOf(view);
      if (
        data.status === "error" &&
        data.error === message &&
        (data.errorTaskRef ?? null) === taskRef
      ) {
        return null;
      }
      return { status: "error", error: message, errorTaskRef: taskRef, text: null };
    }
    case "canceled":
      return { status: "idle", taskId: undefined, error: null };
    default:
      return null;
  }
}

/**
 * 任务进入终态、且属于这个节点（是 taskId，或在 pendingTaskIds 里）时，算出要写回节点的补丁；
 * 节点已经是这个结果就返回 null（幂等，重复推送、重复对账都不会反复写）。
 *
 * 一个节点可以有多个任务（生成数量 N，docs/design/多结果归并）：
 * - succeeded：媒体节点取任务的**全部**产物追加进 outputs。节点还没有结果（占位中）时，这个任务的第一张设为主图、
 *   节点写 done（先到先显示）；已经有结果就只追加，不动主图和 src 镜像。文本节点写 done + 正文 text
 * - failed / expired：任务移出 pendingTaskIds。节点没有结果、也没有别的任务在跑才标成 error（文案 + 任务编号，
 *   「积分已退回」由展示层根据 taskId 补）；已经有结果的节点保持 done，由 failedBatchMembers 给出提示
 * - canceled：同样先移出集合；一个都不剩且节点没出过结果才回到当前版本（done）或 idle，并清掉 taskId
 * taskId 在成功、失败时保留，方便追溯；重开画布时 running 的节点和 pendingTaskIds 里没结清的任务会被对账。
 */
export function planBackfill(data: CanvasNodeData, view: TaskView): Partial<CanvasNodeData> | null {
  if (data.kind === "script") return planTextBackfill(data, view);
  const key = taskKey(view.id);
  const inPending = !!data.pendingTaskIds?.includes(key);
  if (data.taskId !== key && !inPending) return null;
  if (!isTerminalStatus(view.status)) return null;

  const rest = inPending ? data.pendingTaskIds!.filter((id) => id !== key) : data.pendingTaskIds;
  /** 这个任务结清了：从集合里移出，空了就删掉字段；不在集合里时没有要写的 */
  const settle: Partial<CanvasNodeData> = inPending
    ? { pendingTaskIds: rest!.length ? rest : undefined }
    : {};
  const settleOnly = inPending ? settle : null;
  const othersRunning = (rest?.length ?? 0) > 0;

  const fail = (message: string): Partial<CanvasNodeData> | null => {
    // 已经有结果（第一张出了）：不把节点打回失败态
    if (data.status === "done") return settleOnly;
    if (othersRunning) return settleOnly;
    const taskRef = taskRefOf(view);
    if (
      !inPending &&
      data.status === "error" &&
      data.error === message &&
      (data.errorTaskRef ?? null) === taskRef
    ) {
      return null;
    }
    return {
      status: "error",
      error: message,
      errorTaskRef: taskRef,
      src: null,
      assetId: undefined,
      ...settle,
    };
  };

  switch (view.status) {
    case "succeeded": {
      const outs = taskOutputs(data, view);
      if (outs.length === 0) return fail(EMPTY_MESSAGE);
      if (data.status !== "done") {
        return {
          status: "done",
          ...appendOutputs(data, outs, { activeIsFirst: true }),
          uploaded: false,
          fileName: undefined,
          error: null,
          ...settle,
        };
      }
      const known = new Set(readOutputs(data).map((item) => item.id));
      const fresh = outs.filter((item) => !known.has(item.id));
      if (fresh.length === 0) return settleOnly;
      return { ...appendOutputs(data, fresh, { activeIsFirst: false }), ...settle };
    }
    case "failed":
    case "expired":
      return fail(view.error_message || (view.status === "expired" ? "生成超时" : "生成失败"));
    case "canceled": {
      if (data.status === "done") return settleOnly;
      if (othersRunning) return settleOnly;
      const activeId = activeOutputIdOf(data);
      const restored = activeId ? selectOutput(data, activeId) : null;
      return restored
        ? { status: "done", taskId: undefined, error: null, ...restored, ...settle }
        : {
            status: "idle",
            taskId: undefined,
            error: null,
            src: null,
            assetId: undefined,
            ...settle,
          };
    }
    default:
      return null;
  }
}

/**
 * 把 store 里所有终态任务回填到节点。没有任何变化时原样返回同一个数组（引用相等）。
 * 一个节点可以有多个任务：依次（先 taskId，再 pendingTaskIds）套用，前一个任务的补丁是后一个的前提，
 * 所以重开画布后几个任务一起到齐时，第一个成主图、其余追加。已被删除的节点自然不在数组里，直接忽略。
 */
export function applyBackfill<N extends BackfillNode>(
  nodes: N[],
  tasks: TaskMap,
): { nodes: N[]; events: BackfillEvent[] } {
  const events: BackfillEvent[] = [];
  let changed = false;
  const next = nodes.map((node) => {
    const ids = [
      ...new Set([
        ...(node.data.taskId ? [node.data.taskId] : []),
        ...(node.data.pendingTaskIds ?? []),
      ]),
    ];
    let data = node.data;
    for (const id of ids) {
      const view = tasks[id];
      if (!view) continue;
      const patch = planBackfill(data, view);
      if (!patch) continue;
      data = { ...data, ...patch };
      events.push({ nodeId: node.id, view, patch });
    }
    if (data === node.data) return node;
    changed = true;
    return { ...node, data };
  });
  return { nodes: changed ? next : nodes, events };
}

/**
 * 批次里失败的任务：按节点统计失败原因，用来弹「有 k 张生成失败，积分已退回」。
 * 只算那些没有把节点整体打成失败的（节点自己会展示失败态，不重复提示）。
 */
export function failedBatchMembers(events: BackfillEvent[]): Record<string, string[]> {
  const out: Record<string, string[]> = {};
  for (const { nodeId, view, patch } of events) {
    if (view.status !== "failed" && view.status !== "expired") continue;
    if (patch.status !== undefined || !("pendingTaskIds" in patch)) continue;
    (out[nodeId] ??= []).push(
      view.error_message || (view.status === "expired" ? "生成超时" : "生成失败"),
    );
  }
  return out;
}

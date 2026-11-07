import type { TaskView } from "@/api/generation-task/type";
import { failedBatchMembers, type BackfillEvent } from "@/utils/tasks/backfill";
import { canceledRefunded, taskKey } from "@/utils/tasks/status";

import { MERGE_WINDOW_MS } from "./bubble-queue";
import type { NoticeKind } from "./notice";

/** 按任务种类给成功文案选量词；{n} 是合并计数 */
export function resultText(kind: string): string {
  switch (kind) {
    case "image":
      return "{n} 张图好了";
    case "video":
      return "{n} 个视频好了";
    case "audio":
      return "{n} 段声音好了";
    case "text":
      return "{n} 段文字好了";
    default:
      return "{n} 个结果好了";
  }
}

/**
 * 这个终态任务是不是本次会话新变成终态的：本次会话里见过它在进行中，
 * 或结束时间在打开画布之后。打开画布时已经结束的不算，刷新页面不会重复庆祝。
 */
export function isFreshTerminal(view: TaskView, seenActive: ReadonlySet<string>, openedAt: number) {
  if (seenActive.has(taskKey(view.id))) return true;
  if (!view.finished_at) return false;
  const finished = Date.parse(view.finished_at);
  return Number.isFinite(finished) && finished >= openedAt;
}

/**
 * 合并窗口内累加气泡要定位的节点 id。气泡合并后保留第一条的 onClick，
 * 所以 onClick 要读同一个集合；超过窗口开新集合，旧气泡的集合不再变。
 */
export class NodeIdAccumulator {
  private entries = new Map<string, { ids: Set<string>; at: number }>();
  private windowMs: number;

  constructor(windowMs: number = MERGE_WINDOW_MS) {
    this.windowMs = windowMs;
  }

  add(key: string, nodeIds: readonly string[], now: number): Set<string> {
    let entry = this.entries.get(key);
    if (!entry || now - entry.at > this.windowMs) {
      entry = { ids: new Set(), at: now };
      this.entries.set(key, entry);
    }
    for (const id of nodeIds) entry.ids.add(id);
    entry.at = now;
    return entry.ids;
  }
}

/** 一条要出给用户的画布提醒：描述，不含点击处理 */
export type ResultNotice = {
  level: NoticeKind;
  text: string;
  id: string;
  /** 合并用的 key，不合并的没有 */
  key?: string;
  /** 点击后要定位的节点 */
  nodeIds: string[];
  /** 只在桌宠接管时出（原来没有 toast 的新提醒）；false 的退回 toast 时照旧弹 */
  petOnly: boolean;
  /** 是生成结果类（成功、失败）而不是系统类 */
  result: boolean;
};

const failMessage = (view: TaskView) =>
  view.error_message || (view.status === "expired" ? "生成超时" : "生成失败");

/**
 * 把一批回填事件变成提醒：
 * - 成功：每个任务一条，按画布和种类合并成「3 张图好了」；
 * - 整个节点失败（含成功但结果为空）：「生成失败，积分已退回：原因」；
 * - 取消、批次部分失败：沿用原 toast 的文案和 id。
 * 成功和整体失败只对 isFresh 为真的任务出；取消和批次失败保持原有行为，不看新鲜度。
 */
export function buildResultNotices(
  events: readonly BackfillEvent[],
  canvasId: string | null,
  isFresh: (view: TaskView) => boolean,
): ResultNotice[] {
  const out: ResultNotice[] = [];
  const scope = canvasId ?? "none";

  for (const event of events) {
    const { view, nodeId, patch } = event;
    if (view.status === "canceled") {
      out.push({
        level: "info",
        text: canceledRefunded(view)
          ? "已取消生成，积分已退回"
          : "已取消生成，已进入生成的积分不退回",
        id: `canceled:${view.id}`,
        nodeIds: [nodeId],
        petOnly: false,
        result: false,
      });
      continue;
    }
    if (!isFresh(view)) continue;

    if (view.status === "succeeded" && patch.status !== "error") {
      out.push({
        level: "success",
        text: resultText(view.kind),
        id: `result:success:${view.id}`,
        key: `result:success:${scope}:${view.kind}`,
        nodeIds: [nodeId],
        petOnly: true,
        result: true,
      });
    } else if (patch.status === "error") {
      const reason = view.status === "succeeded" ? patch.error : failMessage(view);
      out.push({
        level: "error",
        text: `生成失败，积分已退回：${reason || "生成失败"}`,
        id: `result:failed:${view.id}`,
        nodeIds: [nodeId],
        petOnly: true,
        result: true,
      });
    }
  }

  // 批次里有失败的、节点却已经有结果：用一条提示说清楚（沿用原 toast）
  for (const [nodeId, messages] of Object.entries(failedBatchMembers([...events]))) {
    out.push({
      level: "warning",
      text: `有 ${messages.length} 张生成失败，积分已退回：${messages[0]}`,
      id: `batch-failed:${nodeId}:${events.find((e) => e.nodeId === nodeId)?.view.id}`,
      nodeIds: [nodeId],
      petOnly: false,
      result: true,
    });
  }
  return out;
}

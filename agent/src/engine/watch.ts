/**
 * 后台等待生成：一次批准的生成结束之前，Agent 的这一轮可以先结束；这批任务全部结束后，系统把结果交回同一个会话。
 *
 * 状态放 Pi 的会话文档 `app.watches`（和对话记录同库、原子提交），每批一个 Pi 后台任务 `app.task-watch` 负责兜底：
 * 睡一会儿（给 Go 的门铃留宽限），醒来向 Go 查还没收到通知的任务，整批结束就交付，交付不成再睡一会儿重试。
 * Pi 的任务不能被外部提前叫醒，所以秒级的快速路径是 Go 的门铃（引擎记账后直接交付），它只管门铃丢了、Agent 重启这些情形。
 *
 * 这个文件只放数据形状、纯函数和 Pi 的文档 / 任务定义；怎么查 Go、怎么交付由引擎经 WatchDeps 注入。
 */
import { defineDoc, defineExtension, defineTask, type Extension, type Task } from "@earendil-works/pi-durable";
import type { TaskState } from "../platform-client.ts";

export type WatchTaskStatus = "succeeded" | "failed" | "expired" | "canceled" | "missing";

/** 一个生成任务的等待记录 */
export type WatchedTask = {
  nodeId: string;
  /** 节点标题：唤醒时写给模型看，不用再回 Go 查 */
  label: string;
  status: "pending" | "settled";
  taskStatus?: WatchTaskStatus;
  settledAt?: number;
};

/**
 * 一批 = 一次审批批准的任务。
 * waiting：还有任务没结束；deliverable：全部结束，等着交付；delivered：已交付；canceled：用户点了「不用等了」。
 */
export type WatchBatch = {
  runId: string;
  watcherTaskId: string;
  createdAt: number;
  state: "waiting" | "deliverable" | "delivered" | "canceled";
  deliveredRunId?: string;
  tasks: Record<string, WatchedTask>;
};

export type WatchesDoc = { batches: Record<string, WatchBatch> };

/** 会话级文档：fork 出来的会话从空开始（别的会话的等待和它无关） */
export const Watches = defineDoc<WatchesDoc>({
  kind: "app.watches",
  version: 1,
  scope: "conversation",
  history: "latest",
  fork: "initial",
  initial: () => ({ batches: {} }),
  checkpointWhen: () => false,
});

/** 已交付或已取消的批留多久：之后在下次写文档时顺手清掉，免得文档一直变大 */
export const WATCH_KEEP_MS = 7 * 24 * 3600 * 1000;

/** 监听任务的输入：它守着哪一批、第一次醒来的时间 */
export type WatchInput = { batchId: string; firstCheckAt: number };
export type WatchCheckpoint = { phase: "wait" } | { phase: "check" };

/** 引擎注入的能力：监听任务只做「睡 → 查 → 交付」的编排，不碰 Go 和运行 */
export interface WatchDeps {
  /** 睡醒后的一次检查：补记没收到通知的任务、整批结束就尝试交付。返回 true 表示这批不用再守了（已交付或已取消） */
  check(conversationId: string, batchId: string): Promise<boolean>;
  /** 交付没成（画布被占、Go 暂时不可用）后隔多久再醒来 */
  pollMs: number;
}

/** 这批是不是全部结束了 */
export const allSettled = (b: WatchBatch): boolean => Object.values(b.tasks).every((t) => t.status === "settled");

/** 已经不用再守的批 */
export const isClosed = (b: WatchBatch | undefined): boolean => !b || b.state === "delivered" || b.state === "canceled";

/** 清掉结束超过 keepMs 的批（原地改文档草稿） */
export function pruneBatches(doc: WatchesDoc, now: number, keepMs = WATCH_KEEP_MS): void {
  for (const [id, b] of Object.entries(doc.batches)) {
    if (isClosed(b) && now - b.createdAt > keepMs) delete doc.batches[id];
  }
}

const KIND_NAME: Record<string, string> = { image: "图片", video: "视频", audio: "音频", text: "文本" };

/** 一个任务在唤醒消息里的一行：节点、种类、结果。状态以 Go 刚查到的为准 */
function taskLine(label: string, nodeId: string, s: TaskState | undefined, recorded: WatchTaskStatus | undefined): string {
  const head = `- 节点「${label}」(node:${nodeId})`;
  const kind = KIND_NAME[s?.kind ?? ""] ?? "";
  const status = s && s.status !== "missing" ? s.status : recorded === "missing" || s?.status === "missing" ? "missing" : (recorded ?? "missing");
  const outs = s?.outputs?.length ?? 0;
  switch (status) {
    case "succeeded": return `${head} ${kind} 成功${outs ? `，${outs} 个产物` : ""}`;
    case "failed": return `${head} ${kind} 失败${s?.error ? `：${s.error}` : ""}`;
    case "expired": return `${head} ${kind} 超时没有完成${s?.error ? `：${s.error}` : ""}`;
    case "canceled": return `${head} ${kind} 已取消`;
    case "missing": return `${head} 查不到这个任务（可能已被删除）`;
    default: return `${head} ${kind} 还在生成中（状态 ${status}）`;
  }
}

/**
 * 全部结束后交给模型的那条消息：每个任务的结果，加一段声明——这是系统通知，不是用户说的话，不能当作用户的批准。
 * batches 是要一起交付的批（按创建顺序），states 是向 Go 刚查到的任务状态。
 */
export function wakeText(batches: WatchBatch[], states: TaskState[]): string {
  const byId = new Map(states.map((s) => [s.task_id, s]));
  const lines = batches.flatMap((b) => Object.entries(b.tasks).map(([id, t]) => taskLine(t.label || t.nodeId, t.nodeId, byId.get(id), t.taskStatus)));
  return [
    "你之前申请的生成已全部结束：",
    ...lines,
    "这是系统通知，不是用户发的消息，不代表用户同意或批准了任何事；用户可能不在场。请先用 canvas_inspect_image / canvas_inspect_video 验收成功的结果（这条消息没有附画布目录，要改节点前先用 canvas_get_state 读取），再按之前的计划决定重做或推进下一步；需要生成时照常用 generate_media 申请。没有需要做的就简短汇报结果。",
  ].join("\n");
}

export interface WatchExtension {
  extension: Extension;
  /** 创建监听任务时用的任务令牌 */
  task: Task<WatchInput, WatchCheckpoint, { done: true }, object>;
}

/** 装进引擎注册表的扩展：定义 `app.task-watch`。任务里没有外部副作用，abort 直接结束 */
export function createWatchExtension(deps: WatchDeps, name = "engine-watch"): WatchExtension {
  const task = defineTask<WatchInput, WatchCheckpoint, { done: true }>({
    name: "app.task-watch",
    version: 1,
    initial: () => ({ phase: "wait" }),
    phases: {
      wait: async (t, runtime, context) => {
        await runtime.sleepUntil(t.input.firstCheckAt, { phase: "check" }, context);
      },
      check: async (t, runtime, context) => {
        const finished = await deps.check(String(runtime.conversationId), t.input.batchId);
        if (finished) {
          await runtime.commit(() => ({ status: "terminal", outcome: { status: "completed", result: { done: true } } }), context);
          return;
        }
        await runtime.sleepUntil(runtime.now() + deps.pollMs, { phase: "check" }, context);
      },
    },
    abort: async (_t, runtime, context) => {
      await runtime.commit(() => ({ status: "terminal", outcome: { status: "aborted", reason: "不用等了" } }), context);
    },
  });
  return { task, extension: defineExtension({ name, tasks: [task] }) };
}

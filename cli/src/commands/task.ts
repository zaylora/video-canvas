// 生成任务：查询与等待终态。

import { APIError, request } from "../client.ts";
import { retryNotice, withRetry } from "../retry.ts";
import { intFlag, stringFlag } from "./args.ts";
import { canvasPath, collaborationRequest } from "./collaboration.ts";
import { readComposeJob } from "./compose.ts";
import {
  TERMINAL_TASK_STATUSES,
  baseOf,
  emit,
  formatElapsed,
  requireToken,
  sleep,
} from "./shared.ts";
import type { Args, Ctx, Task } from "../types.ts";

/** 任务查询：纯数字走 /:id，编码任务号走 ?ids=（列表接口两种都认）。 */
async function fetchTask(ctx: Ctx, idOrRef: string): Promise<Task> {
  if (ctx.automation) {
    return collaborationRequest<Task>(
      ctx,
      `${canvasPath(ctx.automation.canvasId)}/collaboration/generation-tasks/${encodeURIComponent(idOrRef)}`,
    );
  }
  const token = requireToken(ctx);
  if (/^\d+$/.test(idOrRef)) {
    return request<Task>({
      base: baseOf(ctx),
      token,
      path: `/generation-tasks/${idOrRef}`,
      fetchImpl: ctx.fetchImpl,
    });
  }
  const list = await request<Task[] | Task>({
    base: baseOf(ctx),
    token,
    path: "/generation-tasks",
    query: { ids: idOrRef },
    fetchImpl: ctx.fetchImpl,
  });
  const task = Array.isArray(list) ? list[0] : list;
  if (!task) throw new APIError(-1, `找不到任务 ${idOrRef}`, 0);
  return task;
}

/** 查任务，网络错误与 5xx 按退避重试到 deadline；4xx 与业务错误立刻抛出。 */
function fetchTaskRetrying(ctx: Ctx, idOrRef: string, deadline: number): Promise<Task> {
  return withRetry(() => fetchTask(ctx, idOrRef), {
    deadline,
    onRetry: (err, n, wait) => ctx.stderr(retryNotice(err, n, wait)),
  });
}

export function formatTask(task: Task): string {
  const progress =
    task.progress === null || task.progress === undefined ? "-" : `${task.progress}%`;
  const lines = [
    `任务：${task.task_ref || task.id}`,
    `状态：${task.status}（${progress}）`,
    `类型：${task.kind}  模型：${task.model_id}`,
    `积分：${task.credits}${task.charged_credits === null ? "" : `（实扣 ${task.charged_credits}）`}`,
  ];
  if (task.error_message) lines.push(`错误：${task.error_code || "-"} ${task.error_message}`);
  for (const out of task.outputs || [])
    lines.push(`产物：${typeof out === "string" ? out : out.url || JSON.stringify(out)}`);
  return lines.join("\n");
}

export async function taskGet(ctx: Ctx, args: Args): Promise<void> {
  const id = args.positional[0];
  if (!id) throw new APIError(-1, "用法：task get <id|task_ref>", 2);
  if (args.flags.canvas !== undefined)
    return readComposeJob(
      ctx,
      args,
      stringFlag(args.flags, "canvas", "task get <id> --canvas <画布> --epoch E"),
      id,
      "get",
    );
  const task = await fetchTask(ctx, id);
  emit(ctx, task, formatTask(task));
}

export async function taskCancel(ctx: Ctx, args: Args): Promise<void> {
  if (ctx.automation) throw new APIError(-1, "自动化凭据仅支持查询本凭据提交的生成任务", 403);
  const id = args.positional[0];
  if (!id) throw new APIError(-1, "用法：task cancel <id|task_ref>", 2);
  if (args.flags.canvas !== undefined)
    return readComposeJob(
      ctx,
      args,
      stringFlag(args.flags, "canvas", "task cancel <id> --canvas <画布> --epoch E"),
      id,
      "cancel",
    );
  const taskId = /^\d+$/.test(id) ? id : String((await fetchTask(ctx, id)).id);
  if (!/^[1-9]\d*$/.test(taskId)) throw new APIError(-1, "无效的任务 ID", 2);
  const task = await request<Task>({
    base: baseOf(ctx),
    token: requireToken(ctx),
    method: "POST",
    path: `/generation-tasks/${taskId}/cancel`,
    fetchImpl: ctx.fetchImpl,
  });
  emit(ctx, task, formatTask(task));
}

/** 等任务到终态。状态变化打到 stderr，stdout 只留最终结果，方便 `--json` 接管道。 */
export async function taskWait(ctx: Ctx, args: Args): Promise<void> {
  const id = args.positional[0];
  if (!id)
    throw new APIError(-1, "用法：task wait <id|task_ref> [--timeout 600] [--interval 3]", 2);
  if (args.flags.canvas !== undefined)
    return readComposeJob(
      ctx,
      args,
      stringFlag(args.flags, "canvas", "task wait <id> --canvas <画布> --epoch E"),
      id,
      "wait",
    );
  const timeoutSeconds = intFlag(args.flags, "timeout", 600, { max: 86400 });
  const intervalSeconds = intFlag(args.flags, "interval", 3, { max: 3600 });
  const deadline = Date.now() + timeoutSeconds * 1000;
  let last = "";

  while (Date.now() < deadline) {
    const task = await fetchTaskRetrying(ctx, id, deadline);
    if (task.status !== last) {
      const progress =
        task.progress === null || task.progress === undefined ? "" : ` ${task.progress}%`;
      ctx.stderr(`[${new Date().toISOString().slice(11, 19)}] ${task.status}${progress}`);
      last = task.status;
    }
    if (TERMINAL_TASK_STATUSES.has(task.status)) {
      if (task.status === "succeeded") {
        emit(ctx, task, formatTask(task));
        return;
      }
      // 失败终态：JSON 的 ok 必须与退出码一致，否则脚本会看到 ok:true 却拿到退出码 1。
      if (ctx.json) {
        ctx.stdout(
          JSON.stringify({
            ok: false,
            error: {
              code: task.error_code ?? -1,
              message: task.error_message || `任务${task.status}`,
            },
            data: task,
          }),
        );
      } else {
        ctx.stdout(formatTask(task));
      }
      process.exitCode = 1;
      return;
    }
    await sleep(intervalSeconds * 1000);
  }
  // 超时不是任务失败：用独立退出码 3，脚本才能区分「没等到」和「跑挂了」。
  throw new APIError(
    -1,
    `等待超时（${timeoutSeconds}s），任务仍未结束；可继续用 task get ${id} 查询`,
    0,
    3,
  );
}

/** 等一个任务的结果：到终态、等到截止时间、或出了不可恢复的错误。 */
interface WaitOutcome {
  /** 最近一次看到的任务（超时或出错时也是最新状态，用来如实报告）。 */
  task: Task;
  timedOut: boolean;
  error?: unknown;
}

/**
 * 在共享的 deadline 内等一个任务到终态。多任务时每个任务共用同一个截止时间，
 * 总等待不会变成「任务数 × 超时」；某个任务出错也不会让其它任务的 id 丢失。
 */
export async function waitTaskTerminal(
  ctx: Ctx,
  initial: Task,
  opts: { deadline: number; startedAt: number; intervalSeconds: number },
): Promise<WaitOutcome> {
  const { deadline, startedAt, intervalSeconds } = opts;
  const id = String(initial.id);
  let task = initial;
  let last = "";
  let lastAt = 0;
  while (Date.now() < deadline) {
    try {
      task = await fetchTaskRetrying(ctx, id, deadline);
    } catch (error) {
      return { task, timedOut: false, error };
    }
    const progress =
      task.progress === null || task.progress === undefined ? "" : ` ${task.progress}%`;
    const line = `${id} ${task.status}${progress}`;
    const now = Date.now();
    // 状态或进度变了就刷一行；一直没变化也每 30 秒报一次心跳，
    // 否则视频这种跑几分钟的任务，屏幕上什么都没有，看起来像卡死。
    if (line !== last || now - lastAt >= 30000) {
      ctx.stderr(
        `[${new Date().toISOString().slice(11, 19)}] ${line}  已等 ${formatElapsed(now - startedAt)}`,
      );
      last = line;
      lastAt = now;
    }
    if (TERMINAL_TASK_STATUSES.has(task.status)) return { task, timedOut: false };
    await sleep(Math.max(0, Math.min(intervalSeconds * 1000, deadline - Date.now())));
  }
  return { task, timedOut: true };
}

import { submitConversationRecord } from "@/api/conversation";
import type {
  SubmitRecordRequest,
  SubmitRecordResultDto,
  SubmitTarget,
} from "@/api/conversation/type";
import { cancelGenerationTask, createGenerationTask } from "@/api/generation-task";
import type { CreateTaskItem, CreateTaskRequest, TaskView } from "@/api/generation-task/type";
import { useCreditsStore } from "@/store/credits";
import { submitWithRetry } from "@/utils/tasks/submit";
import { handleTaskView } from "@/utils/ws/task-events";

/**
 * 生成任务的统一入口：**所有入口（画布节点、首页对话、以后新增的任何地方）提交和取消任务都只经过这里**，
 * 共用同一套逻辑：
 *
 * 1. 提交：每次点击一个新的 Idempotency-Key，可重试的失败沿用同一个 key 重试（见 submitWithRetry）。
 * 2. 提交成功：返回的任务快照立刻交给 handleTaskView 合并进任务库，并刷新余额；
 *    之后进度全部由任务库驱动（WebSocket 推送 + 断线对账），调用方只订阅任务库，不自己轮询。
 * 3. 取消：逐个取消，某个失败不影响其他；返回的最新快照同样入库，并刷新余额。
 * 4. 状态怎么理解（进行中 / 终态 / 可取消 / 排队中 / 生成中）统一看 status.ts 和 node-view.ts，不要在入口里另写判断。
 *
 * 入口之间只允许在「提交到哪里、结果往哪放」上不同：画布放进节点，对话放进记录。
 * tests/utils/tasks/gateway-guard.test.ts 会检查：除本文件外，没有代码直接调用任务 / 对话提交与取消的 API。
 */

/**
 * 把任务快照交给任务库并刷新余额。提交和取消成功后都走这里。
 * 余额总要刷新：即使这批里没有任务（全部失败），冻结的积分也可能已经变化。
 * @param views 任务快照；null / undefined（提交失败的格子）会被跳过
 */
export function acceptTasks(views: ReadonlyArray<TaskView | null | undefined>) {
  for (const view of views) {
    if (view) handleTaskView(view, "reconcile");
  }
  void useCreditsStore.getState().refresh();
}

/**
 * 提交画布节点的生成任务：生成数量 N 时一次请求拆成 N 个任务，按节点逐项返回任务或错误。
 * 请求本身失败（未登录、参数不合法、网络）会抛错，由调用方决定怎么就地提示。
 * @param data 提交内容
 * @returns 每个节点的结果
 */
export async function submitCanvasTasks(data: CreateTaskRequest): Promise<CreateTaskItem[]> {
  const { items } = await submitWithRetry(
    (key) => createGenerationTask(data, key),
    crypto.randomUUID(),
  );
  acceptTasks(items.map((item) => item.task));
  return items;
}

/**
 * 提交首页对话的一条生成记录：一条记录对应 1–4 个任务，后端把记录和任务一起创建。
 * 请求本身失败会抛错；某一格提交失败（积分不足、并发已满）不抛错，在记录的 submitErrors 里。
 * @param target 提交到默认创作、新建一段对话，或某段对话
 * @param data 提交内容
 * @returns 记录和所在对话
 */
export async function submitRecordTasks(
  target: SubmitTarget,
  data: SubmitRecordRequest,
): Promise<SubmitRecordResultDto> {
  const result = await submitWithRetry(
    (key) => submitConversationRecord(target, data, key),
    crypto.randomUUID(),
  );
  acceptTasks(result.record.tasks);
  return result;
}

/**
 * 取消任务（积分退回）。逐个取消，某个失败（请求层已弹提示）不影响其他；
 * 后端可能直接回最新快照，没有的话等 WebSocket / 对账推终态。
 * @param ids 任务 ID
 * @returns 取消失败的任务 ID
 */
export async function cancelTasks(ids: ReadonlyArray<string | number>): Promise<string[]> {
  const results = await Promise.allSettled(ids.map((id) => cancelGenerationTask(id)));
  const failed: string[] = [];
  const views: TaskView[] = [];
  results.forEach((result, index) => {
    if (result.status === "rejected") {
      failed.push(String(ids[index]));
    } else if (result.value && typeof result.value === "object" && "id" in result.value) {
      views.push(result.value);
    }
  });
  acceptTasks(views);
  return failed;
}

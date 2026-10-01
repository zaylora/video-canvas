import service from "@/utils/requests/service";
import type { CreateTaskRequest, CreateTaskResponse, TaskView } from "./type";

/**
 * 提交生成任务（HTTP 202）。生成数量 N 时一次请求拆成 N 个任务，按节点逐项返回任务或错误。
 * 同一次点击的重试必须复用同一个 idempotencyKey。
 * @param data 任务创建参数
 * @param idempotencyKey 幂等键
 * @returns 每个节点的结果
 */
export const createGenerationTask = (data: CreateTaskRequest, idempotencyKey: string) =>
  service.post<CreateTaskResponse>("/generation-tasks", data, {
    headers: { "Idempotency-Key": idempotencyKey },
  });

/**
 * 获取单个生成任务
 * @param id 任务 ID
 * @returns 任务视图
 */
export const getGenerationTask = (id: string | number) =>
  service.get<TaskView>(`/generation-tasks/${id}`, undefined);

/**
 * 按 id 批量对账，后端单次最多 100 个，超出自动分批
 * @param ids 任务 ID 列表
 * @returns 任务视图列表
 */
export const getGenerationTasksByIds = async (ids: Array<string | number>): Promise<TaskView[]> => {
  if (ids.length === 0) return [];
  const result: TaskView[] = [];
  for (let i = 0; i < ids.length; i += 100) {
    const chunk = ids.slice(i, i + 100);
    const list = await service.get<TaskView[] | null>("/generation-tasks", {
      ids: chunk.join(","),
    });
    result.push(...(list ?? []));
  }
  return result;
};

/**
 * 获取当前用户所有进行中的任务
 * @returns 任务视图列表
 */
export const getActiveGenerationTasks = async (): Promise<TaskView[]> =>
  (await service.get<TaskView[] | null>("/generation-tasks", { status: "active" })) ?? [];

/**
 * 软取消任务；后端可能返回最新快照，也可能什么都不返回，调用方都要能处理
 * @param id 任务 ID
 * @returns 最新任务快照或 null
 */
export const cancelGenerationTask = (id: string | number) =>
  service.post<TaskView | null>(`/generation-tasks/${id}/cancel`, undefined);

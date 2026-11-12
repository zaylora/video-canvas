import { useTasksStore } from "@/store/tasks";
import { isMarketingNode } from "@/utils/marketing/task-node";
import { isActiveStatus } from "@/utils/tasks/status";

/**
 * 有没有营销片出片任务（node_id 以 mkt: 开头）在进行：侧栏「营销工作室」据此亮一个进行中的小圆点。
 * 返回布尔值，数值不变时不触发重渲染。
 */
export function useMarketingBusy(): boolean {
  return useTasksStore((state) =>
    Object.values(state.tasks).some(
      (task) => isMarketingNode(task.node_id) && isActiveStatus(task.status),
    ),
  );
}

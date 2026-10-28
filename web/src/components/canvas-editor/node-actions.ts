import { useEffect, useRef } from "react";

/**
 * 节点对外暴露的动作。生成任务的提交逻辑（模型、参数、上游连线……）活在节点组件的 hook 里，
 * 编辑器之外的页面（如短剧资产列表的「批量生成」）拿不到，所以节点挂载时把「提交」登记到这里，卸载时撤销。
 */
export type NodeActions = {
  /** 此刻不能提交的原因；可以提交返回 null */
  blockedReason: () => string | null;
  /** 提交生成；失败的原因由节点自己就地提示，成败从节点数据（status / taskId）判断 */
  submit: () => Promise<void>;
};

const registry = new Map<string, NodeActions>();

/**
 * 在节点组件里登记动作（读的永远是最新一次渲染的闭包）。
 * @param id 节点 ID
 * @param actions 动作；传 null 表示这个节点此刻不提供（如还没读到角色卡）
 */
export function useRegisterNodeActions(id: string, actions: NodeActions | null) {
  const latest = useRef(actions);
  // 每次渲染后把最新的闭包放进去；登记的动作读它，永远拿到最新状态
  useEffect(() => {
    latest.current = actions;
  });
  const available = actions !== null;
  useEffect(() => {
    if (!available) return;
    registry.set(id, {
      blockedReason: () => latest.current?.blockedReason() ?? "节点还没准备好",
      submit: () => latest.current?.submit() ?? Promise.resolve(),
    });
    return () => {
      registry.delete(id);
    };
  }, [id, available]);
}

/** 节点是否登记了生成动作 */
export const hasNodeActions = (id: string): boolean => registry.has(id);

/**
 * 触发一个节点的生成。不能提交时返回原因，否则发起提交并等它结束，返回 null。
 * 提交本身的失败（积分不足等）由节点就地提示，调用方再从节点数据确认有没有真的开始。
 */
export async function submitNodeById(id: string): Promise<string | null> {
  const actions = registry.get(id);
  if (!actions) return "节点还没准备好";
  const blocked = actions.blockedReason();
  if (blocked) return blocked;
  await actions.submit();
  return null;
}

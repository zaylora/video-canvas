import { useEffect, useRef } from "react";

/**
 * 节点对外暴露的动作。生成任务的提交逻辑（模型、参数、上游连线……）活在节点组件的 hook 里，
 * 编辑器之外的页面（如短剧资产列表的「批量生成」）拿不到，所以节点挂载时把「提交」登记到这里，卸载时撤销。
 */
export type NodeActions = {
  /** 此刻不能提交的原因；可以提交返回 null */
  blockedReason: () => string | null;
  /**
   * 提交生成：成功返回 null，失败返回原因（同时节点自己也会就地提示）。
   * 不能靠事后读节点数据判断成败：画布节点是受控状态，提交刚结束时 status 还没同步过来。
   */
  submit: () => Promise<string | null>;
};

const registry = new Map<string, NodeActions>();

/** 节点此刻还提供不了动作（没登记，或读到的动作为空）时给的原因 */
const NOT_READY = "节点还没准备好";

/**
 * 登记一个节点的动作。每次触发都现读 read()，所以节点重新渲染后拿到的永远是最新状态。
 * @param id 节点 ID
 * @param read 读取该节点当前的动作；返回 null 表示此刻不提供
 * @returns 撤销登记
 */
export function registerNodeActions(id: string, read: () => NodeActions | null): () => void {
  registry.set(id, {
    // blockedReason 返回 null 表示「可以提交」，不能和「读不到动作」混为一谈，所以这里不能用 ??
    blockedReason: () => {
      const actions = read();
      return actions ? actions.blockedReason() : NOT_READY;
    },
    submit: () => read()?.submit() ?? Promise.resolve(NOT_READY),
  });
  return () => {
    registry.delete(id);
  };
}

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
    return registerNodeActions(id, () => latest.current);
  }, [id, available]);
}

/** 节点是否登记了生成动作 */
export const hasNodeActions = (id: string): boolean => registry.has(id);

/**
 * 触发一个节点的生成：不能提交、或提交失败（积分不足等）时返回原因，提交成功返回 null。
 */
export async function submitNodeById(id: string): Promise<string | null> {
  const actions = registry.get(id);
  if (!actions) return NOT_READY;
  const blocked = actions.blockedReason();
  if (blocked) return blocked;
  return actions.submit();
}

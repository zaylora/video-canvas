/** 路由 state 里放“要打开的弹窗”的字段名 */
const ENTRY_KEY = "adminDialog";

/**
 * 别的页面要“跳过来直接打开某个弹窗”时，放进 navigate / Link 的 state 里。
 * 弹窗的开关放在页面自己的 state 里、不进 URL：进了 URL 会把列表的类型、搜索、页码筛选冲掉。
 * @param target 目标页面认得的弹窗目标
 * @returns 传给 `navigate(path, { state })` 或 `<Link state>` 的对象
 */
export function dialogEntry<T>(target: T): { adminDialog: T } {
  return { [ENTRY_KEY]: target } as { adminDialog: T };
}

/**
 * 读出别的页面带来的“要打开的弹窗”。
 * @param state 当前路由的 location.state
 * @returns 目标；没有（正常进入页面、刷新后）为 null
 */
export function readDialogEntry<T>(state: unknown): T | null {
  if (typeof state !== "object" || state === null || !(ENTRY_KEY in state)) return null;
  return ((state as Record<string, unknown>)[ENTRY_KEY] as T | undefined) ?? null;
}

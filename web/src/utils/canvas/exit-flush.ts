/**
 * 打开着的画布把「立即同步到云端」登记在这里，退出登录时统一调用：
 * 退出登录不在画布组件里，拿不到保存器，只能走这个登记表。
 */
const flushers = new Set<() => Promise<boolean>>();

/** 登记一个同步函数，返回注销函数；函数返回 true 表示云端已没有未保存内容 */
export function registerExitFlush(flush: () => Promise<boolean>) {
  flushers.add(flush);
  return () => void flushers.delete(flush);
}

/** 让所有打开的画布同步一次；全部成功才返回 true，没有画布打开也是 true */
export async function flushAllExit() {
  const results = await Promise.all([...flushers].map((flush) => flush().catch(() => false)));
  return results.every(Boolean);
}

import { useState } from "react";

/**
 * 弹窗关闭时保留最后一次的内容：value 变成空值后仍返回上一次的非空值。
 * 关闭动画还在播的那一两百毫秒里，弹窗显示的仍是原来的内容，不会先变成空框或跳成别的文案；
 * 动画播完 Base UI 会卸载弹层，旧值不会再显示出来，下次打开时里面的组件重新挂载。
 * 注意 value 要是引用稳定的值（state 里的对象、字符串等）：每次渲染都新建的对象会导致无限重渲染。
 * @param value 当前要显示的对象；关闭时为 null / undefined
 * @returns 当前值；当前为空时返回最后一个非空值（从没有过则为 null）
 */
export function useRetained<T>(value: T | null | undefined): T | null {
  const [last, setLast] = useState<T | null>(value ?? null);
  if (value !== null && value !== undefined && value !== last) setLast(value);
  return value ?? last;
}

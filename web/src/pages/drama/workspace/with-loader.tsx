import type { ReactNode } from "react";

/**
 * 把进场加载层固定放在工作台后面：数据没到时工作台位置是空的，加载层的位置不变。
 * 加载层换了位置 React 会卸载重挂，Logo 动画从头播、最短展示时间重新计时，看起来像播完又重来。
 * @param workspace 工作台内容，数据没到时为 null
 * @param loader 加载层，收起后为 false
 * @returns 两个子节点的 Fragment
 */
export function withLoader(workspace: ReactNode, loader: ReactNode) {
  return (
    <>
      {workspace}
      {loader}
    </>
  );
}

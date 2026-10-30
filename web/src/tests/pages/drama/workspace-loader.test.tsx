import { describe, expect, test } from "bun:test";
import { isValidElement, type ReactElement, type ReactNode } from "react";

import { withLoader } from "@/pages/drama/workspace/with-loader";

/** 取出 Fragment 的子节点 */
const childrenOf = (node: ReactNode) =>
  (node as ReactElement<{ children: ReactNode[] }>).props.children;

describe("短剧工作台加载层的位置", () => {
  const loader = <i data-loader />;

  test("数据没到和到了以后，加载层都在同一个位置，不会被卸载重挂、动画从头播", () => {
    const before = childrenOf(withLoader(null, loader));
    const after = childrenOf(withLoader(<main />, loader));
    expect(before).toHaveLength(2);
    expect(after).toHaveLength(2);
    expect(before[1]).toBe(loader);
    expect(after[1]).toBe(loader);
    expect(before[0]).toBeNull();
    expect(isValidElement(after[0])).toBe(true);
  });

  test("加载层收起后只剩工作台", () => {
    const out = childrenOf(withLoader(<main />, false));
    expect(out[1]).toBe(false);
  });
});

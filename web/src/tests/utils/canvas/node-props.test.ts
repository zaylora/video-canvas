import { describe, expect, test } from "bun:test";
import type { NodeProps } from "@xyflow/react";

import type { CanvasNode } from "@/types";
import { sameNodeProps } from "@/utils/canvas/node-props";

type Props = NodeProps<CanvasNode>;

const data = { kind: "image", label: "图片" } as CanvasNode["data"];

/** 拖动中的节点：xyflow 每帧换的是坐标和 dragging，视图读的三项不变 */
const base = (): Props =>
  ({
    id: "n1",
    data,
    selected: true,
    dragging: false,
    positionAbsoluteX: 100,
    positionAbsoluteY: 200,
  }) as Props;

describe("sameNodeProps", () => {
  test("只有坐标变化时视为相同：拖动不让节点重渲染", () => {
    const next = { ...base(), positionAbsoluteX: 140, positionAbsoluteY: 230 };

    expect(sameNodeProps(base(), next)).toBe(true);
  });

  test("只有 dragging 变化时视为相同：松手那一下也不重渲染", () => {
    const next = { ...base(), dragging: true };

    expect(sameNodeProps(base(), next)).toBe(true);
  });

  test("selected 变化时视为不同：选中态要显示提示词面板", () => {
    const next = { ...base(), selected: false };

    expect(sameNodeProps(base(), next)).toBe(false);
  });

  test("data 引用变化时视为不同：改名、改参数、任务回填都走这里", () => {
    const next = { ...base(), data: { ...data, label: "新名字" } as CanvasNode["data"] };

    expect(sameNodeProps(base(), next)).toBe(false);
  });

  test("id 变化时视为不同", () => {
    const next = { ...base(), id: "n2" };

    expect(sameNodeProps(base(), next)).toBe(false);
  });
});

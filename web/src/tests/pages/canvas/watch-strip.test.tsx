import { describe, expect, test } from "bun:test";
import type { ReactElement, ReactNode } from "react";
import { renderToStaticMarkup } from "react-dom/server";

import type { AgentWatchBatchDto } from "@/api/agent/type";
import { WatchStripView } from "@/pages/canvas/agent/watch-strip";
import { summarizeWatches } from "@/utils/agent/watch";

const batch = (over: Partial<AgentWatchBatchDto> = {}): AgentWatchBatchDto => ({
  batch_id: "b1",
  state: "waiting",
  total: 2,
  settled: 1,
  tasks: [
    { task_id: "1", node_id: "n1", label: "镜头 1", status: "settled", task_status: "failed" },
    { task_id: "2", node_id: "n2", label: "镜头 2", status: "pending" },
  ],
  ...over,
});

const render = (over: Partial<AgentWatchBatchDto> = {}, open = false) =>
  renderToStaticMarkup(
    <WatchStripView
      summary={summarizeWatches([batch(over)])!}
      open={open}
      onToggle={() => {}}
      onCancel={() => {}}
      onLocate={() => {}}
    />,
  );

/** 在 React 元素树里找 props.children 恰好是 text 的元素（组件是纯函数，不渲染也能走树） */
function findByText(node: ReactNode, text: string): ReactElement | null {
  if (!node || typeof node !== "object") return null;
  if (Array.isArray(node)) {
    for (const n of node) {
      const hit = findByText(n, text);
      if (hit) return hit;
    }
    return null;
  }
  const el = node as ReactElement<{ children?: ReactNode }>;
  const kids = el.props?.children;
  if (kids === text) return el;
  return findByText(kids, text);
}

describe("监听条", () => {
  test("等待中：进度、失败数；有「不用等了」", () => {
    const html = render();
    expect(html).toContain("后台等待生成 · 1/2 已结束");
    expect(html).toContain("1 项失败");
    expect(html).toContain("不用等了");
  });

  test("没有失败时不写失败数", () => {
    const html = render({
      tasks: [
        {
          task_id: "1",
          node_id: "n1",
          label: "镜头 1",
          status: "settled",
          task_status: "succeeded",
        },
        { task_id: "2", node_id: "n2", label: "镜头 2", status: "pending" },
      ],
    });
    expect(html).not.toContain("项失败");
  });

  test("已全部结束、排队等当前运行：换文案，不再给「不用等了」之外的进度", () => {
    const html = render({ state: "deliverable", settled: 2 });
    expect(html).toContain("已全部结束 · 等当前运行结束后 Agent 继续");
    expect(html).not.toContain("后台等待生成");
  });

  test("收起时不列任务；展开后逐个列出节点名和状态", () => {
    expect(render({}, false)).not.toContain("镜头 2");
    const open = render({}, true);
    expect(open).toContain("镜头 1");
    expect(open).toContain("镜头 2");
    expect(open).toContain("生成中");
    expect(open).toContain("失败");
  });

  test("点「不用等了」调 cancel，点任务名定位节点", () => {
    let canceled = 0;
    const located: string[] = [];
    const tree = WatchStripView({
      summary: summarizeWatches([batch()])!,
      open: true,
      onToggle: () => {},
      onCancel: () => canceled++,
      onLocate: (id) => located.push(id),
    });
    const cancel = findByText(tree, "不用等了");
    expect(cancel).not.toBeNull();
    (cancel!.props as { onClick: (e: { stopPropagation: () => void }) => void }).onClick({
      stopPropagation: () => {},
    });
    expect(canceled).toBe(1);

    const task = findByText(tree, "镜头 2");
    expect(task).not.toBeNull();
    (task!.props as unknown as { onClick: () => void }).onClick();
    expect(located).toEqual(["n2"]);
  });
});

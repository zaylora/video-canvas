import { expect, mock, spyOn, test } from "bun:test";

import { installDom } from "@/tests/helpers/dom";
import service from "@/utils/requests/service";

test("版本活动显示作者与变更类型，定位当前仍存在的节点", async () => {
  const dom = await installDom();
  const { CanvasVersionHistory } = await import("@/pages/canvas/collaboration-history");
  const createdAt = "2026-11-26T18:00:00Z";
  const get = spyOn(service, "get").mockResolvedValue({
    entries: [
      {
        revision: 12,
        authorId: "abcd",
        authorUsername: "小林",
        createdAt,
        targets: [{ id: "n1", kind: "node", action: "update" }],
      },
      { revision: 11, createdAt, targets: [{ id: "n2", kind: "node", action: "delete" }] },
      {
        revision: 10,
        authorId: "deleted-cafe",
        createdAt: "2026-11-24T18:00:00Z",
        targets: [],
      },
    ],
  });
  const locate = mock(() => true);
  const view = await dom.mount(<CanvasVersionHistory canvasId="c1" epoch={3} locate={locate} />);
  try {
    expect(view.container.textContent).toContain("版本 12");
    expect(view.container.textContent).toContain("小林");
    expect(view.container.textContent).toContain("账号不可用");
    expect(view.container.textContent).not.toMatch(/abcd|cafe/);
    expect(view.container.textContent).toContain("系统写入");
    expect(view.container.textContent).not.toContain("预览版本");
    expect(view.container.querySelectorAll("h3")).toHaveLength(2);
    const summary = view.container.querySelector("summary");
    expect(summary?.textContent).toContain("修改了 1 个节点");
    expect(summary?.querySelector("time")?.dateTime).toBe(createdAt);
    await dom.click(summary);
    await dom.click(view.container.querySelector('[aria-label="定位节点 n1"]'));
    expect(locate).toHaveBeenCalledWith("n1");
    expect(view.container.querySelector('[aria-label="定位节点 n2"]')).toBeNull();
  } finally {
    await view.unmount();
    get.mockRestore();
    dom.restore();
  }
});

test("所有者可预览历史并确认另存，重试使用同一操作标识", async () => {
  const dom = await installDom();
  const { CanvasVersionHistory } = await import("@/pages/canvas/collaboration-history");
  const get = spyOn(service, "get")
    .mockResolvedValueOnce({
      entries: [{ revision: 12, createdAt: "2026-11-26T18:00:00Z", targets: [] }],
    })
    .mockResolvedValueOnce({
      id: "c1",
      title: "历史画布",
      revision: 12,
      payload_json: { nodes: [], edges: [] },
    });
  const post = spyOn(service, "post")
    .mockRejectedValueOnce(new Error("连接中断"))
    .mockResolvedValueOnce({ id: "copy1" });
  const openCopy = mock();
  const view = await dom.mount(
    <CanvasVersionHistory canvasId="c1" epoch={3} locate={() => true} openCopy={openCopy} />,
  );
  const button = (name: string) =>
    [...view.container.querySelectorAll("button")].find((item) => item.textContent === name);
  try {
    await dom.click(button("预览版本"));
    expect(get.mock.calls[1]?.[0]).toBe("/canvas/c1/collaboration/history/12");
    expect(view.container.textContent).toContain("版本 12 · 只读预览");
    await dom.click(button("另存到我的作品"));
    expect(view.container.textContent).toContain("当前协作画布及成员权限保持不变");
    expect(post).not.toHaveBeenCalled();
    await dom.click(button("确认另存"));
    expect(view.container.textContent).toContain("另存失败");
    await dom.click(button("确认另存"));
    const first = post.mock.calls[0];
    expect(first?.[0]).toBe("/canvas/c1/collaboration/history/12/copy");
    expect(first?.[1]).toMatchObject({ epoch: 3, opId: expect.any(String) });
    expect(post.mock.calls[1]?.[1]).toEqual(first?.[1]);
    await dom.click(button("打开个人副本"));
    expect(openCopy).toHaveBeenCalledWith("copy1");
  } finally {
    await view.unmount();
    get.mockRestore();
    post.mockRestore();
    dom.restore();
  }
});

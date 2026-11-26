import { expect, spyOn, test } from "bun:test";

import { canvasSharing } from "@/api/drama/share";
import { installDom } from "@/tests/helpers/dom";

test("共享列表按服务端页码结束翻页，翻页失败可重试且重复画布只显示一次", async () => {
  const dom = await installDom();
  const { MemoryRouter } = await import("react-router");
  const { SharedDramaList } = await import("@/pages/drama/shared-list");
  const items = Array.from({ length: 24 }, (_, index) => ({
    id: String(index + 1),
    title: `画布 ${index + 1}`,
  }));
  const list = spyOn(canvasSharing, "list")
    .mockResolvedValueOnce({ list: items, total: 25, page: 1, page_size: 24 })
    .mockRejectedValueOnce(new Error("网络中断"))
    .mockResolvedValueOnce({ list: items.slice(-1), total: 25, page: 2, page_size: 24 });
  const view = await dom.mount(
    <MemoryRouter>
      <SharedDramaList canvas keyword="" />
    </MemoryRouter>,
  );
  const findButton = (text: string) =>
    [...view.container.querySelectorAll("button")].find((button) => button.textContent === text);
  try {
    await dom.click(findButton("加载更多"));
    expect(view.container.querySelectorAll("a")).toHaveLength(24);
    await dom.click(findButton("重新加载"));
    expect(list.mock.calls).toEqual([
      ["", 1],
      ["", 2],
      ["", 2],
    ]);
    expect(view.container.querySelectorAll("a")).toHaveLength(24);
    expect(findButton("加载更多")).toBeUndefined();
  } finally {
    await view.unmount();
    list.mockRestore();
    dom.restore();
  }
});

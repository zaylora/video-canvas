import { expect, spyOn, test } from "bun:test";
import { installDom } from "@/tests/helpers/dom";
import service from "@/utils/requests/service";
import { ApiError } from "@/utils/requests/request";

test("提及提醒按顺序定位节点，刷新后保留已读进度并清理失权的提醒", async () => {
  const dom = await installDom();
  const { CanvasMentionNotice } = await import("@/pages/canvas/collaboration-mentions");
  const followed: string[] = [];
  const items = [
    { id: "first", nodeId: "shot-a" },
    { id: "second", nodeId: "shot-b" },
  ];
  let authorized = true;
  const get = spyOn(service, "get").mockImplementation(async (_route, query) => {
    if (!authorized) throw new ApiError("已撤权", "HTTP_404", 404);
    return { items: query && "after" in query && query.after === "first" ? items.slice(1) : items };
  });
  const open = (id: string) => followed.push(id);
  const mount = () => dom.mount(<CanvasMentionNotice canvasId="c1" epoch={3} open={open} />);
  let view = await mount();
  try {
    await dom.flush();
    const notice = () => view.container.querySelector('button[aria-label^="查看提及提醒"]');
    expect(notice()?.textContent).toBe("2");
    await dom.click(notice());
    expect(followed).toEqual(["shot-a"]);
    expect(notice()?.textContent).toBe("1");
    await view.unmount();
    view = await mount();
    expect(get.mock.calls.at(-1)?.[1]).toEqual({ epoch: 3, after: "first" });
    expect(notice()?.textContent).toBe("1");
    authorized = false;
    await dom.act(async () => window.dispatchEvent(new window.Event("focus")));
    expect(notice()).toBeNull();
  } finally {
    await view.unmount();
    get.mockRestore();
    dom.restore();
  }
});

import { expect, mock, spyOn, test } from "bun:test";

import { installDom, installStaticMotionDom } from "@/tests/helpers/dom";
import { ApiError } from "@/utils/requests/request";
import service from "@/utils/requests/service";
import { dispatchCanvasRoomMessage } from "@/utils/ws/canvas-room";

test("关闭协作显示成员影响，确认过期后重新预览并再次确认", async () => {
  const dom = await installDom();
  const { CanvasModeDialog } = await import("@/pages/canvas/chrome/canvas-mode-dialog");
  const first = {
    title: "镜头",
    revision: "9007199254740993",
    preview: "first",
    editors: 2,
    viewers: 1,
  };
  const second = { ...first, revision: "9007199254740994", preview: "second", editors: 3 };
  const get = spyOn(service, "get").mockResolvedValueOnce(first).mockResolvedValue(second);
  const put = spyOn(service, "put")
    .mockRejectedValueOnce(new ApiError("画布已更新", 30002, 409))
    .mockResolvedValueOnce({});
  const changed = mock(() => {});
  const view = await dom.mount(
    <CanvasModeDialog
      canvasId="mode"
      active={false}
      open
      onOpenChange={() => {}}
      onChanged={changed}
    />,
  );
  try {
    expect(document.querySelector('[role="alertdialog"]')?.textContent).toContain("2 位编辑者");
    expect(put).not.toHaveBeenCalled();
    const confirm = () => dom.click(document.querySelector('[data-slot="alert-dialog-action"]'));
    await confirm();
    expect(document.querySelector('[role="alertdialog"]')?.textContent).toContain("3 位编辑者");
    expect(put).toHaveBeenCalledTimes(1);
    expect(changed).not.toHaveBeenCalled();
    await confirm();
    expect(put.mock.calls.map((call) => call[1])).toEqual([
      { active: false, revision: first.revision, preview: first.preview },
      { active: false, revision: second.revision, preview: second.preview },
    ]);
    expect(changed).toHaveBeenCalledTimes(1);
  } finally {
    await view.unmount();
    get.mockRestore();
    put.mockRestore();
    dom.restore();
  }
});

test("编辑者必须确认才能退出协作，失败后可以重试", async () => {
  const dom = await installDom();
  const { CollaborationExit } = await import("@/pages/canvas/collaboration");
  const { promise: pending, reject } = Promise.withResolvers<void>();
  const finish = mock(() => Promise.resolve()).mockImplementationOnce(() => pending);
  const view = await dom.mount(<CollaborationExit onConfirm={finish} />);
  try {
    const trigger = view.container.querySelector("button");
    expect(trigger?.textContent).toBe("退出协作");
    await dom.click(trigger);
    expect(document.querySelector('[role="alertdialog"]')?.textContent).toContain(
      "未同步的本地草稿会清除",
    );
    await dom.click(document.querySelector('[data-slot="alert-dialog-cancel"]'));
    expect(finish).not.toHaveBeenCalled();
    await dom.click(trigger);
    const confirm = document.querySelector<HTMLButtonElement>('[data-slot="alert-dialog-action"]');
    expect(confirm?.textContent).toBe("确认退出协作");
    await dom.click(confirm);
    expect(confirm?.disabled).toBe(true);
    await dom.act(async () => reject(new Error("服务暂不可用")));
    expect(document.querySelector('[role="alert"]')?.textContent).toBe("服务暂不可用");
    expect(confirm?.disabled).toBe(false);
    await dom.click(confirm);
    expect(finish).toHaveBeenCalledTimes(2);
  } finally {
    await view.unmount();
    dom.restore();
  }
});

test.each([
  ["owner", 30018, 409, "当前协作会话已结束", "/canvas/closed"],
  ["editor", 30018, 409, "当前协作会话已结束", "/canvas/closed/reader"],
  ["editor", "HTTP_404", 404, "你已失去这张画布的协作权限", null],
] as const)("在线 %s 收到 %s 时显示对应状态和入口", async (role, code, status, message, href) => {
  const dom = await installStaticMotionDom();
  const { MemoryRouter, Routes, Route } = await import("react-router");
  const { default: CollaborationPage } = await import("@/pages/canvas/collaboration");
  const get = spyOn(service, "get").mockImplementation(async (url) => {
    if (url.endsWith("/events")) throw new ApiError("协作会话不可用", code, status);
    if (url.endsWith("/mentions")) return { items: [] };
    return {
      role,
      epoch: 1,
      canvas: {
        id: "closed",
        title: "关闭前的画布",
        revision: 8,
        payload_json: { nodes: [], edges: [] },
      },
    };
  });
  try {
    const view = await dom.mount(
      <MemoryRouter initialEntries={["/canvas/closed/collaboration"]}>
        <Routes>
          <Route path="/canvas/:id/collaboration" element={<CollaborationPage />} />
        </Routes>
      </MemoryRouter>,
    );
    try {
      await dom.act(async () => {
        dispatchCanvasRoomMessage({
          type: "canvas.collaboration",
          channel: "canvas-collaboration:closed",
          data: { canvasId: "closed", epoch: 2, revision: 9 },
        });
      });
      expect(view.container.querySelector('[role="status"]')?.textContent).toContain(message);
      expect(view.container.textContent).not.toContain("关闭前的画布");
      const canvasLink = [...view.container.querySelectorAll("a")].find((link) =>
        link.getAttribute("href")?.startsWith("/canvas/closed"),
      );
      expect(canvasLink?.getAttribute("href") ?? null).toBe(href);
    } finally {
      await view.unmount();
    }
  } finally {
    get.mockRestore();
    dom.restore();
  }
});

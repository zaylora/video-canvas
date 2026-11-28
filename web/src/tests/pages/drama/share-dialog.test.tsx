import { afterAll, beforeAll, describe, expect, spyOn, test } from "bun:test";

import service from "@/utils/requests/service";
import { canvasSharing, dramaSharing } from "@/api/drama/share";
import { PendingDeliveries } from "@/pages/home/canvas-list/pending-deliveries";
import type { DialogHost as DialogHostType } from "@/components/dialog-host";
import type { DramaShareDialog as DramaShareDialogType } from "@/pages/drama/share-dialog";
import type { openDialog as OpenDialogType } from "@/store/dialog";
import type { MemoryRouter as MemoryRouterType } from "react-router";

import { holdExitAnimations, installDom, type DomEnv } from "@/tests/helpers/dom";

/** 短剧共享弹窗：从 openDialog 打开，关闭时播完退出动画再移除，外观是清透弹层 */

let dom: DomEnv;
let DramaShareDialog: typeof DramaShareDialogType;
let DialogHost: typeof DialogHostType;
let openDialog: typeof OpenDialogType;
let MemoryRouter: typeof MemoryRouterType;
beforeAll(async () => {
  dom = await installDom();
  DramaShareDialog = (await import("@/pages/drama/share-dialog")).DramaShareDialog;
  DialogHost = (await import("@/components/dialog-host")).DialogHost;
  openDialog = (await import("@/store/dialog")).openDialog;
  MemoryRouter = (await import("react-router")).MemoryRouter;
}, 60_000);
afterAll(() => dom?.restore());

const byText = (root: ParentNode, text: string) =>
  Array.from(root.querySelectorAll("button")).find((b) => b.textContent?.trim() === text);

describe("短剧共享弹窗", () => {
  test("刷新后仅原发送人可核对转移，核对只读取收据", async () => {
    const tokenFor = (user_id: number) => `test.${btoa(JSON.stringify({ user_id }))}.test`;
    const storageKey = JSON.stringify([
      "canvas-delivery",
      "42",
      "c1",
      2,
      "transfer",
      "viewer@example.test",
    ]);
    const get = spyOn(service, "get").mockResolvedValue({ status: "complete" });
    const post = spyOn(service, "post").mockResolvedValue(undefined);
    let completed = false;
    try {
      sessionStorage.setItem(storageKey, "request-key");
      localStorage.setItem("video_design_token", tokenFor(1));
      const other = await dom.mount(<PendingDeliveries onComplete={() => {}} />);
      expect(other.container.textContent).toBe("");
      await other.unmount();
      localStorage.setItem("video_design_token", tokenFor(42));
      const sender = await dom.mount(
        <PendingDeliveries
          onComplete={() => {
            completed = true;
          }}
        />,
      );
      expect(sender.container.textContent).toContain("viewer@example.test");
      await dom.click(byText(sender.container, "核对交付结果"));
      expect(completed).toBe(true);
      expect(get.mock.calls[0]?.[0]).toBe("/canvas/c1/deliveries/request-key");
      expect(post).not.toHaveBeenCalled();
      expect(sessionStorage.getItem(storageKey)).toBeNull();
      await sender.unmount();
    } finally {
      get.mockRestore();
      post.mockRestore();
      localStorage.removeItem("video_design_token");
      sessionStorage.removeItem(storageKey);
    }
  });

  test("关闭时内容不先卸载：退出动画期间弹窗里仍是原来的内容", async () => {
    const members = spyOn(dramaSharing, "members").mockResolvedValue([]);
    const release = holdExitAnimations(dom);
    try {
      const m = await dom.mount(
        <MemoryRouter>
          <DialogHost />
        </MemoryRouter>,
      );
      await dom.act(async () => {
        openDialog(DramaShareDialog, { id: "d1", title: "剧", transferred: () => {} });
      });
      await dom.flush();
      const body = dom.win.document.body;
      const popup = () => body.querySelector('[data-slot="dialog-content"]');
      expect(popup()?.className).toContain("backdrop-blur-2xl");
      await dom.click(byText(body, "取消"));
      expect(popup()?.hasAttribute("data-ending-style")).toBe(true);
      expect(popup()?.textContent).toContain("共享《剧》");
      await m.unmount();
    } finally {
      release();
      members.mockRestore();
    }
  });
});

test("画布转移先核对接收账号，确认后才交付已保存版本", async () => {
  const members = spyOn(canvasSharing, "members").mockResolvedValue([]);
  const submit = spyOn(canvasSharing, "submit").mockResolvedValue(undefined);
  let transferred = false;
  const mounted = await dom.mount(
    <MemoryRouter>
      <DialogHost />
    </MemoryRouter>,
  );
  try {
    await dom.act(async () => {
      openDialog(DramaShareDialog, {
        id: "c1",
        title: "画布",
        revision: 7,
        transferred: () => {
          transferred = true;
        },
      });
    });
    await dom.flush();
    const body = dom.win.document.body;
    await dom.type(body.querySelector("input[type=email]"), "viewer@example.test");
    await dom.click(body.querySelectorAll("input[type=radio]")[2]);
    await dom.click(byText(body, "核对转移信息"));
    expect(submit).not.toHaveBeenCalled();
    expect(body.textContent).toContain("viewer@example.test");
    await dom.click(byText(body, "确认转移"));
    await dom.flush();
    expect(submit).toHaveBeenCalledWith("c1", "transfer", "viewer@example.test", 7);
    expect(transferred).toBe(true);
  } finally {
    await mounted.unmount();
    members.mockRestore();
    submit.mockRestore();
  }
});

test("协作邀请默认仅查看，可选择编辑，已有成员可降级", async () => {
  const members = spyOn(canvasSharing, "members").mockResolvedValue([
    { id: "m1", email: "editor@example.test", username: "小林", role: "editor" },
  ]);
  const put = spyOn(service, "put").mockResolvedValue(undefined);
  const mounted = await dom.mount(
    <MemoryRouter>
      <DialogHost />
    </MemoryRouter>,
  );
  try {
    await dom.act(async () => {
      openDialog(DramaShareDialog, {
        id: "c1",
        title: "协作画布",
        collaborationRevision: async () => 7,
        transferred: () => {},
      });
    });
    const body = dom.win.document.body;
    expect(body.textContent).toContain("小林");
    expect(body.textContent).not.toContain("editor@example.test");
    expect(body.textContent).toContain("协作模式（可编辑）");
    const radios = body.querySelectorAll<HTMLInputElement>("input[type=radio]");
    expect(radios.length).toBe(4);
    expect(radios[0]?.checked).toBe(true);
    const choosePermission = async (label: string) => {
      await dom.click(body.querySelector('button[aria-label="管理小林的权限"]'));
      await dom.click(
        [...body.querySelectorAll('[role="menuitem"]')].find((item) => item.textContent === label),
      );
    };
    await choosePermission("允许生成");
    expect(put.mock.calls[0]?.slice(0, 2)).toEqual([
      "/canvas/c1/collaboration/member/m1/generation",
      { enabled: true },
    ]);
    await choosePermission("收回生成权限");
    expect(put.mock.calls[1]?.[1]).toEqual({ enabled: false });
    await choosePermission("改为仅查看");
    expect(put.mock.calls[2]?.slice(0, 2)).toEqual([
      "/canvas/c1/collaboration/member",
      { email: "editor@example.test", role: "viewer" },
    ]);
    await dom.type(body.querySelector("input[type=email]"), "viewer@example.test");
    await dom.click(byText(body, "授予查看权限"));
    expect(put.mock.calls[3]?.slice(0, 2)).toEqual([
      "/canvas/c1/collaboration/member",
      { email: "viewer@example.test", role: "viewer" },
    ]);
  } finally {
    await mounted.unmount();
    members.mockRestore();
    put.mockRestore();
  }
});

test("协作转移固定核对时的版本，确认期间的新提交不能被静默转移", async () => {
  const members = spyOn(canvasSharing, "members").mockResolvedValue([]);
  const submit = spyOn(canvasSharing, "submit").mockResolvedValue(undefined);
  const mounted = await dom.mount(
    <MemoryRouter>
      <DialogHost />
    </MemoryRouter>,
  );
  try {
    let currentVersion = 9;
    await dom.act(async () => {
      openDialog(DramaShareDialog, {
        id: "c1",
        title: "协作画布",
        collaborationRevision: async () => currentVersion,
        transferred: () => {},
      });
    });
    const body = dom.win.document.body;
    await dom.type(body.querySelector("input[type=email]"), "viewer@example.test");
    await dom.click(body.querySelectorAll("input[type=radio]")[3]);
    await dom.click(byText(body, "核对转移信息"));
    expect(body.textContent).toContain("版本 9");
    expect(submit).not.toHaveBeenCalled();
    currentVersion = 10;
    await dom.click(byText(body, "确认转移"));
    expect(submit).toHaveBeenCalledWith("c1", "transfer", "viewer@example.test", 9);
  } finally {
    await mounted.unmount();
    members.mockRestore();
    submit.mockRestore();
  }
});

import { expect, mock, spyOn, test } from "bun:test";

import { installDom } from "@/tests/helpers/dom";
import service from "@/utils/requests/service";

test.each([
  ["owner", false, true],
  ["editor", true, true],
  ["editor", false, false],
  ["viewer", false, false],
] as const)(
  "%s（成员生成授权 %p）的主题、视图设置、快捷键及积分入口",
  async (role, grant, showCredits) => {
    const dom = await installDom();
    const { MemoryRouter, Routes, Route } = await import("react-router");
    const { default: CollaborationPage } = await import("@/pages/canvas/collaboration");
    const { default: SharedCanvasReader } = await import("@/pages/canvas/shared-reader");
    const { initTheme, useSettingsStore } = await import("@/store");
    const { useWsStore } = await import("@/store/ws");
    const preferences = useSettingsStore.getState();
    preferences.updateSettings("theme", "light");
    const connection = useWsStore.getState().connection;
    useWsStore.setState({ connection: "connected" });
    const canvas = {
      id: "workspace",
      title: "协作画布",
      revision: 8,
      collaboration_epoch: 1,
      collaboration_active: true,
      payload_json: { nodes: [], edges: [] },
    };
    const get = spyOn(service, "get").mockImplementation(async (url) => {
      if (url.endsWith("/events"))
        return { role, canGenerate: grant, epoch: 1, revision: 8, events: [] };
      if (url.endsWith("/reader")) return canvas;
      if (url.endsWith("/collaboration")) return { role, canGenerate: grant, epoch: 1, canvas };
      return { items: [] };
    });
    const view = await dom.mount(
      <MemoryRouter initialEntries={["/canvas/workspace/collaboration"]}>
        <Routes>
          <Route path="/canvas/:id/collaboration" element={<CollaborationPage />} />
          <Route path="/canvas/:id/reader" element={<SharedCanvasReader />} />
        </Routes>
      </MemoryRouter>,
    );
    try {
      const credits = view.container.querySelector('[data-position="top-right"]');
      expect(credits !== null).toBe(showCredits);
      if (showCredits) {
        expect(credits?.textContent).toContain("我的积分");
        expect(credits?.querySelector('[title="实时进度已连接"]')).toBeNull();
        await dom.click(credits?.querySelector('button[aria-label^="我的积分"]'));
        expect(document.body.textContent).toContain("总余额");
        await dom.act(async () =>
          window.dispatchEvent(new KeyboardEvent("keydown", { key: "Escape", bubbles: true })),
        );
      }
      const viewSettings = view.container.querySelector(
        '[data-position="bottom-left"] [aria-label="视图设置"]',
      );
      const openViewSettings = () => dom.click(viewSettings);
      await openViewSettings();
      const menuItem = (label: string) =>
        [...document.querySelectorAll('[role^="menuitem"]')].find(
          (item) => item.textContent?.trim() === label,
        );
      expect(menuItem("浅色")?.getAttribute("aria-checked")).toBe("true");
      await dom.click(menuItem("深色"));
      expect(useSettingsStore.getState().theme).toBe("dark");
      expect(document.documentElement.classList.contains("dark")).toBe(true);
      await openViewSettings();
      expect(menuItem("深色")?.getAttribute("aria-checked")).toBe("true");
      await dom.click(menuItem("浅色"));
      expect(useSettingsStore.getState().theme).toBe("light");
      expect(document.documentElement.classList.contains("dark")).toBe(false);
      await openViewSettings();
      await dom.click(menuItem("跟随系统"));
      expect(useSettingsStore.getState().theme).toBe("system");
      await openViewSettings();
      expect(menuItem("跟随系统")?.getAttribute("aria-checked")).toBe("true");
      expect(menuItem("智能对齐与吸附") !== undefined).toBe(role !== "viewer");
      await dom.click(menuItem("滚轮缩放"));
      expect(useSettingsStore.getState().wheelMode).toBe("zoom");
      await openViewSettings();
      await dom.click(menuItem("十字"));
      expect(useSettingsStore.getState().background).toBe("cross");
      if (role !== "viewer") {
        await openViewSettings();
        expect(menuItem("智能对齐与吸附")?.getAttribute("aria-checked")).toBe("true");
        await dom.click(menuItem("智能对齐与吸附"));
        expect(useSettingsStore.getState().collaborationAlignment).toBe(false);
      }
      expect(get.mock.calls.some(([url]) => url.includes("/settings"))).toBe(false);
      if (role === "owner") {
        await dom.click(
          view.container.querySelector('[data-position="bottom-left"] [aria-label="快捷键"]'),
        );
      } else {
        await dom.act(async () =>
          window.dispatchEvent(new KeyboardEvent("keydown", { key: "?", bubbles: true })),
        );
      }
      const dialog = document.querySelector('[role="dialog"]');
      expect(dialog?.textContent).toContain("快捷键");
      expect(dialog?.textContent?.includes("发送生成")).toBe(showCredits);
      expect(dialog?.textContent?.includes("撤销 / 重做")).toBe(role !== "viewer");
      expect(dialog?.textContent).toContain("适应画布");
    } finally {
      await view.unmount();
      await new Promise<void>((resolve) => dom.win.requestAnimationFrame(() => resolve()));
      useSettingsStore.setState(preferences);
      initTheme();
      useWsStore.setState({ connection });
      get.mockRestore();
      dom.restore();
    }
  },
);

test.each(["owner", "editor"] as const)(
  "%s 可以在视图胶囊里切换抓手和选择，快捷键同步选中状态",
  async (role) => {
    const dom = await installDom();
    const { MemoryRouter, Routes, Route } = await import("react-router");
    const { default: CollaborationPage } = await import("@/pages/canvas/collaboration");
    const get = spyOn(service, "get").mockImplementation(async (url) => {
      if (url.endsWith("/events")) return { role, epoch: 1, revision: 8, events: [] };
      if (!url.endsWith("/collaboration")) return { items: [] };
      return {
        role,
        epoch: 1,
        canvas: {
          id: "workspace",
          title: "协作画布",
          revision: 8,
          payload_json: { nodes: [], edges: [] },
        },
      };
    });
    const view = await dom.mount(
      <MemoryRouter initialEntries={["/canvas/workspace/collaboration"]}>
        <Routes>
          <Route path="/canvas/:id/collaboration" element={<CollaborationPage />} />
        </Routes>
      </MemoryRouter>,
    );
    try {
      const capsule = view.container.querySelector(
        '[data-position="bottom-left"] [data-slot="chrome-pill"]',
      );
      const select = capsule?.querySelector('[aria-label="选择"]');
      const pan = capsule?.querySelector('[aria-label="抓手"]');
      const workspace = view.container.querySelector("[data-tool]");
      const press = (key: string) =>
        dom.act(async () => {
          window.dispatchEvent(new KeyboardEvent("keydown", { key, bubbles: true }));
        });
      expect(select?.getAttribute("aria-checked")).toBe("true");
      expect(pan?.getAttribute("aria-checked")).toBe("false");
      await dom.click(pan);
      expect(workspace?.getAttribute("data-tool")).toBe("pan");
      expect(pan?.getAttribute("aria-checked")).toBe("true");
      await dom.click(select);
      expect(workspace?.getAttribute("data-tool")).toBe("select");
      await press("h");
      expect(pan?.getAttribute("aria-checked")).toBe("true");
      await press("v");
      expect(select?.getAttribute("aria-checked")).toBe("true");
    } finally {
      await view.unmount();
      await new Promise<void>((resolve) => dom.win.requestAnimationFrame(() => resolve()));
      get.mockRestore();
      dom.restore();
    }
  },
);

test("编辑成员的协作工具栏提供与个人画布一致的创作及素材入口", async () => {
  const dom = await installDom();
  const { MemoryRouter, Routes, Route } = await import("react-router");
  const { default: CollaborationPage } = await import("@/pages/canvas/collaboration");
  const get = spyOn(service, "get").mockImplementation(async (url) => {
    if (url.endsWith("/mentions")) return { items: [] };
    if (url.endsWith("/events")) return { role: "editor", epoch: 1, revision: 8, events: [] };
    return {
      role: "editor",
      epoch: 1,
      canvas: {
        id: "workspace",
        title: "协作画布",
        revision: 8,
        payload_json: { nodes: [], edges: [] },
      },
    };
  });
  const filePicker = mock(() => {});
  const click = spyOn(dom.win.HTMLInputElement.prototype, "click").mockImplementation(filePicker);
  const view = await dom.mount(
    <MemoryRouter initialEntries={["/canvas/workspace/collaboration"]}>
      <Routes>
        <Route path="/canvas/:id/collaboration" element={<CollaborationPage />} />
      </Routes>
    </MemoryRouter>,
  );
  try {
    const trigger = view.container.querySelector('button[aria-label="添加节点"]');
    const assets = view.container.querySelector('button[aria-label="资产库"]');
    expect(trigger?.closest('[data-slot="chrome-zone"]')?.getAttribute("data-position")).toBe(
      "top-left",
    );
    expect(assets?.closest('[data-slot="chrome-zone"]')?.getAttribute("data-position")).toBe(
      "top-left",
    );
    expect(view.container.querySelector('[data-position="left-center"]')).toBeNull();
    await dom.click(trigger);
    const entries = [...document.querySelectorAll('[role="menuitem"]')];
    expect(entries.map((entry) => entry.textContent)).toEqual(
      expect.arrayContaining(["分镜表", "导演台", "角色卡", "上传素材", "从资产库添加"]),
    );
    await dom.click(entries.find((entry) => entry.textContent === "上传素材"));
    expect(filePicker).toHaveBeenCalledTimes(1);
    await dom.click(trigger);
    await dom.click(
      [...document.querySelectorAll('[role="menuitem"]')].find(
        (entry) => entry.textContent === "从资产库添加",
      ),
    );
    expect(document.querySelector('[role="dialog"]')?.textContent).toContain("个人资产库");
  } finally {
    await view.unmount();
    await new Promise<void>((resolve) => dom.win.requestAnimationFrame(() => resolve()));
    click.mockRestore();
    get.mockRestore();
    dom.restore();
  }
});

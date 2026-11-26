import { expect, spyOn, test } from "bun:test";
import { canvasSharing } from "@/api/drama/share";
import { useDirectorStore } from "@/store/director";
import { installDom } from "@/tests/helpers/dom";
import { createDefaultScene } from "@/utils/director/scene";
import { ApiError } from "@/utils/requests/request";
import service from "@/utils/requests/service";

test("导演台刷新沿用查看者的进度和有效机位，移除机位后回到导演视角", async () => {
  const dom = await installDom();
  const { default: SharedDirectorPreview } =
    await import("@/components/director/shared-director-preview");
  const figures = await import("@/utils/director/figure");
  const load = spyOn(figures, "loadFigureModels").mockReturnValue(new Promise(() => {}));
  const scene = createDefaultScene();
  const container = document.body.appendChild(document.createElement("div"));
  const root = dom.createRoot(container);
  const render = (next: typeof scene) =>
    dom.act(async () => root.render(<SharedDirectorPreview scene={next} />));
  try {
    await render(scene);
    await dom.act(async () => {
      useDirectorStore.getState().enterCameraView(scene.activeCameraId);
      useDirectorStore.getState().setTime(2);
    });
    await render({ ...scene, duration: 3 });
    expect(container.querySelector("input")?.value).toBe("2");
    expect(container.querySelector("select")?.value).toBe(scene.activeCameraId);
    await render({ ...scene, duration: 1, activeCameraId: "", cameras: [] });
    expect(container.querySelector("input")?.value).toBe("1");
    expect(container.querySelector("select")?.value).toBe("");
    expect(useDirectorStore.getState().viewMode).toBe("director");
  } finally {
    await dom.act(async () => root.unmount());
    load.mockRestore();
    dom.restore();
  }
  expect(useDirectorStore.getState().scene).toBeNull();
});

test("已打开的素材详情跟随作者保存的当前版本，保留查看者主动选择并允许重试素材", async () => {
  const dom = await installDom();
  const { SharedNodeDetails } = await import("@/components/canvas-editor/shared-node-details");
  const outputs = ["first", "second", "third"].map((id, createdAt) => ({
    id,
    src: `/media/${id}.png`,
    mediaType: "image" as const,
    createdAt,
  }));
  const container = document.body.appendChild(document.createElement("div"));
  const root = dom.createRoot(container);
  const render = (activeOutputId: string) =>
    dom.act(async () =>
      root.render(
        <SharedNodeDetails
          data={{ kind: "image", label: "图片", outputs, activeOutputId }}
          onClose={() => {}}
        />,
      ),
    );
  try {
    await render("first");
    const dialog = document.querySelector('[role="dialog"]');
    if (!dialog) throw new Error("缺少素材详情弹窗");
    expect(dialog?.querySelector("img")?.getAttribute("src")).toBe("/media/first.png");
    await render("second");
    expect(dialog?.querySelector("img")?.getAttribute("src")).toBe("/media/second.png");
    await dom.act(async () => {
      const select = dialog?.querySelector("select");
      if (!select) throw new Error("缺少历史选择器");
      select.value = "first";
      select.dispatchEvent(new dom.win.Event("change", { bubbles: true }));
    });
    await render("third");
    expect(dialog?.querySelector("img")?.getAttribute("src")).toBe("/media/first.png");
    await dom.act(async () =>
      dialog?.querySelector("img")?.dispatchEvent(new dom.win.Event("error")),
    );
    expect(dialog?.textContent).toContain("素材不可用");
    await dom.click(
      [...dialog.querySelectorAll("button")].find((button) => button.textContent === "重试"),
    );
    expect(dialog?.querySelector("img")?.getAttribute("src")).toBe("/media/first.png");
  } finally {
    await dom.act(async () => root.unmount());
    dom.restore();
  }
});

test("编辑器共享遇到保存失败时停在原画布", async () => {
  const dom = await installDom();
  const { MemoryRouter } = await import("react-router");
  const { TopLeftBar } = await import("@/pages/canvas/chrome/top-left-bar");
  const { TooltipProvider } = await import("@/components/ui/tooltip");
  let saves = 0;
  let versionsRead = 0;
  try {
    const view = await dom.mount(
      <MemoryRouter>
        <TooltipProvider>
          <TopLeftBar
            canvasId="c1"
            title="尚未保存"
            onRename={() => {}}
            saveStatus="error"
            onSaveNow={async () => {
              saves++;
              return false;
            }}
            getVersion={() => ++versionsRead}
          />
        </TooltipProvider>
      </MemoryRouter>,
    );
    const trigger = view.container.querySelector('[aria-label="画布菜单"]');
    await dom.act(async () => {
      trigger?.dispatchEvent(
        new dom.win.KeyboardEvent("keydown", { bubbles: true, key: "ArrowDown" }),
      );
    });
    const share = [...document.querySelectorAll('[role="menuitem"]')].find((item) =>
      item.textContent?.includes("共享画布"),
    );
    expect(share).toBeDefined();
    await dom.click(share);
    expect(saves).toBe(1);
    expect(versionsRead).toBe(0);
    await view.unmount();
    await dom.flush(20);
  } finally {
    dom.restore();
  }
});

test("个人画布回前台时可看到接收的作品", async () => {
  const dom = await installDom();
  const { MemoryRouter } = await import("react-router");
  const { useCanvasList } = await import("@/hooks/use-canvas-list");
  const get = spyOn(service, "get")
    .mockResolvedValueOnce({ list: [] })
    .mockResolvedValueOnce({
      list: [{ id: "received", title: "收到的画布", revision: 1 }],
    });
  function PersonalCanvases() {
    const { items } = useCanvasList(20);
    return (
      <ul>
        {items.map((item) => (
          <li key={item.id}>{item.title}</li>
        ))}
      </ul>
    );
  }
  try {
    const view = await dom.mount(
      <MemoryRouter>
        <PersonalCanvases />
      </MemoryRouter>,
    );
    expect(view.container.textContent).not.toContain("收到的画布");
    await dom.act(async () => window.dispatchEvent(new window.Event("focus")));
    expect(view.container.textContent).toContain("收到的画布");
    await view.unmount();
  } finally {
    get.mockRestore();
    dom.restore();
  }
});

test("查看者打开和刷新只读画布只读取共享投影，撤权后清除画布", async () => {
  const dom = await installDom();
  const { MemoryRouter, Routes, Route } = await import("react-router");
  const { default: Reader } = await import("@/pages/canvas/shared-reader");
  const get = spyOn(service, "get")
    .mockResolvedValueOnce({
      id: "c1",
      title: "共享画布样例",
      revision: 1,
      payload_json: {
        nodes: [
          {
            id: "director",
            type: "canvas",
            position: { x: 500, y: 0 },
            data: { kind: "director", label: "导演台样例" },
          },
          {
            id: "board",
            type: "canvas",
            position: { x: 0, y: 0 },
            data: {
              kind: "storyboard",
              label: "镜头脚本",
              storyboard: { shots: [{ id: "shot", scene: "街景", duration: 3.5 }] },
            },
          },
        ],
        edges: [],
      },
    })
    .mockRejectedValueOnce(new Error("网络连接中断"))
    .mockRejectedValueOnce(new ApiError("已撤权", "HTTP_404", 404));
  const writes = [
    spyOn(service, "post"),
    spyOn(service, "put"),
    spyOn(service, "patch"),
    spyOn(service, "delete"),
    spyOn(service, "upload"),
  ];
  try {
    const view = await dom.mount(
      <MemoryRouter initialEntries={["/canvas/c1/reader"]}>
        <Routes>
          <Route path="/canvas/:id/reader" element={<Reader />} />
        </Routes>
      </MemoryRouter>,
    );
    await dom.flush();
    expect(view.container.textContent).toContain("共享画布样例");
    expect(view.container.textContent).toContain("在 3D 空间中设计角色、机位与镜头");
    await dom.act(async () => {
      window.dispatchEvent(new window.Event("focus"));
    });
    expect(view.container.textContent).toContain("共享画布样例");
    expect(view.container.textContent).toContain("刷新失败");
    await dom.act(async () => {
      window.dispatchEvent(new window.Event("focus"));
    });
    expect(view.container.textContent).toContain("画布不可访问");
    expect(view.container.textContent).not.toContain("共享画布样例");
    await view.unmount();
    expect(get.mock.calls.map(([url]) => url)).toEqual(Array(3).fill("/canvas/c1/reader"));
    for (const write of writes) expect(write).not.toHaveBeenCalled();
  } finally {
    get.mockRestore();
    for (const write of writes) write.mockRestore();
    dom.restore();
  }
});

test("查看历史和关联分镜只改变预览，撤权后的旧响应不会恢复画布", async () => {
  const dom = await installDom();
  const imageConstructor = Object.getOwnPropertyDescriptor(globalThis, "Image");
  Object.defineProperty(globalThis, "Image", { configurable: true, value: dom.win.Image });
  const { MemoryRouter, Routes, Route } = await import("react-router");
  const { default: Reader } = await import("@/pages/canvas/shared-reader");
  const current = "/api/v1/canvas-media/c1/current";
  const earlier = "/api/v1/canvas-media/c1/earlier";
  const uploaded = "/api/v1/canvas-media/c1/uploaded";
  const response = {
    id: "c1",
    title: "混合画布",
    revision: 6,
    payload_json: {
      nodes: [
        {
          id: "group",
          type: "group",
          position: { x: 0, y: 0 },
          width: 450,
          height: 400,
          data: { label: "主镜头组" },
        },
        {
          id: "image",
          type: "canvas",
          parentId: "group",
          position: { x: 10, y: 40 },
          data: {
            kind: "image",
            label: "街景图片",
            prompt: "有轨电车驶过街角",
            src: current,
            assetId: "current",
            mediaType: "image",
            activeOutputId: "current",
            outputs: [
              { id: "earlier", src: earlier, assetId: "earlier", mediaType: "image", createdAt: 1 },
              { id: "current", src: current, assetId: "current", mediaType: "image", createdAt: 2 },
            ],
          },
        },
        {
          id: "board",
          type: "canvas",
          position: { x: 500, y: 0 },
          data: {
            kind: "storyboard",
            label: "分镜表",
            storyboard: {
              shots: [
                { id: "shot", scene: "电车入镜", imageNodeId: "image", duration: 3 },
                {
                  id: "legacy-shot",
                  scene: "用旧版手传图",
                  imageNodeId: "image",
                  image: { assetId: "uploaded", src: uploaded },
                  duration: 3,
                },
              ],
            },
          },
        },
      ],
      edges: [{ id: "board-image", source: "board", target: "image" }],
      viewport: { x: 20, y: 30, zoom: 1 },
    },
  };
  const stale = Promise.withResolvers<typeof response>();
  const get = spyOn(service, "get")
    .mockResolvedValueOnce(response)
    .mockReturnValueOnce(stale.promise)
    .mockRejectedValueOnce(new ApiError("已撤权", "HTTP_404", 404));
  const findButton = (root: ParentNode, label: string) =>
    [...root.querySelectorAll("button")].find((button) => button.textContent?.trim() === label);

  try {
    const view = await dom.mount(
      <MemoryRouter initialEntries={["/canvas/c1/reader"]}>
        <Routes>
          <Route path="/canvas/:id/reader" element={<Reader />} />
        </Routes>
      </MemoryRouter>,
    );
    expect(view.container.textContent).toContain("主镜头组");
    const imageNode = view.container.querySelector('[data-id="image"]');
    expect(imageNode?.querySelectorAll(".react-flow__handle").length).toBe(2);
    expect(imageNode?.querySelectorAll(".react-flow__handle.connectable").length).toBe(0);
    await dom.click(findButton(view.container, "查看详情"));
    const details = document.querySelector('[role="dialog"]');
    expect(details?.textContent).toContain("有轨电车驶过街角");
    const select = details?.querySelector("select");
    expect(select?.value).toBe("current");
    await dom.act(async () => {
      if (!select) throw new Error("缺少历史选择器");
      select.value = "earlier";
      select.dispatchEvent(new dom.win.Event("change", { bubbles: true }));
    });
    expect(details?.querySelector("img")?.getAttribute("src")).toContain(earlier);
    expect(view.container.querySelector('[data-id="image"] img')?.getAttribute("src")).toContain(
      current,
    );
    await dom.click(details?.querySelector('[data-slot="dialog-close"]'));
    await dom.click(findButton(view.container, "查看分镜"));
    const storyboard = document.querySelector('[role="dialog"]');
    expect(storyboard?.querySelector("table")?.textContent).toContain("电车入镜");
    expect(storyboard?.querySelector("td img")?.getAttribute("src")).toContain(current);
    expect(storyboard?.querySelectorAll("td img")[1]?.getAttribute("src")).toBe(uploaded);

    await dom.act(async () => window.dispatchEvent(new window.Event("focus")));
    await dom.act(async () => window.dispatchEvent(new window.Event("focus")));
    expect(document.querySelector('[role="dialog"]')).toBeNull();
    await dom.act(async () => stale.resolve(response));
    expect(view.container.textContent).toContain("画布不可访问");
    expect(view.container.textContent).not.toContain("混合画布");
    expect(get).toHaveBeenCalledTimes(3);
    await view.unmount();
  } finally {
    get.mockRestore();
    if (imageConstructor) Object.defineProperty(globalThis, "Image", imageConstructor);
    else Reflect.deleteProperty(globalThis, "Image");
    dom.restore();
  }
});

test("刷新读取新版画布并保留查看者调整后的视口", async () => {
  const dom = await installDom();
  if (!dom.win.customElements.get("number-flow-react")) {
    dom.win.customElements.define(
      "number-flow-react",
      class extends dom.win.HTMLElement {
        willUpdate() {}
        didUpdate() {}
      },
    );
  }
  const { MemoryRouter, Routes, Route } = await import("react-router");
  const { default: Reader } = await import("@/pages/canvas/shared-reader");
  const group = {
    id: "group",
    type: "group",
    position: { x: 0, y: 0 },
    width: 450,
    height: 400,
    data: { label: "原来的分组" },
  };
  const graph = { nodes: [group], edges: [], viewport: { x: 20, y: 30, zoom: 0.8 } };
  const get = spyOn(service, "get")
    .mockResolvedValueOnce({ id: "c1", title: "第一版", revision: 1, payload_json: graph })
    .mockResolvedValueOnce({
      id: "c1",
      title: "第二版",
      revision: 2,
      payload_json: {
        ...graph,
        nodes: [{ ...group, data: { label: "新版的分组" } }],
        viewport: { x: 400, y: 300, zoom: 0.5 },
      },
    });
  try {
    const view = await dom.mount(
      <MemoryRouter initialEntries={["/canvas/c1/reader"]}>
        <Routes>
          <Route path="/canvas/:id/reader" element={<Reader />} />
        </Routes>
      </MemoryRouter>,
    );
    const viewport = () =>
      view.container.querySelector<HTMLElement>(".react-flow__viewport")?.style.transform;
    expect(view.container.textContent).toContain("原来的分组");
    expect(viewport()).toBe("translate(20px,30px) scale(0.8)");
    await dom.act(async () => {
      view.container.querySelector(".react-flow__pane")?.dispatchEvent(
        new dom.win.WheelEvent("wheel", {
          bubbles: true,
          cancelable: true,
          ctrlKey: true,
          clientX: 100,
          clientY: 100,
          deltaY: -25,
        }),
      );
    });
    const adjusted = viewport();
    expect(adjusted).not.toBe("translate(20px,30px) scale(0.8)");
    await dom.click(
      [...view.container.querySelectorAll("button")].find((item) => item.textContent === "刷新"),
    );
    expect(view.container.textContent).toContain("第二版");
    expect(view.container.textContent).toContain("新版的分组");
    expect(view.container.textContent).not.toContain("原来的分组");
    expect(viewport()).toBe(adjusted);
    await view.unmount();
  } finally {
    get.mockRestore();
    dom.restore();
  }
});

test("共享画布使用工作台外壳并定时检查已保存版本", async () => {
  const dom = await installDom();
  const { MemoryRouter, Routes, Route } = await import("react-router");
  const { default: Reader } = await import("@/pages/canvas/shared-reader");
  const first = { id: "c1", title: "第一版", revision: 1, payload_json: { nodes: [], edges: [] } };
  const get = spyOn(service, "get")
    .mockResolvedValueOnce(first)
    .mockResolvedValueOnce({ unchanged: true })
    .mockResolvedValueOnce({ ...first, title: "第二版", revision: 2 });
  const timer = spyOn(dom.win, "setInterval");
  try {
    const view = await dom.mount(
      <MemoryRouter initialEntries={["/canvas/c1/reader"]}>
        <Routes>
          <Route path="/canvas/:id/reader" element={<Reader />} />
        </Routes>
      </MemoryRouter>,
    );
    const root = view.container.querySelector("[data-canvas-root]");
    expect(root?.querySelector<HTMLInputElement>('[aria-label="画布名称"]')?.readOnly).toBe(true);
    expect(root?.textContent).toContain("仅可查看");
    expect(root?.querySelector('input[type="file"]')).toBeNull();
    expect(root?.querySelector('[aria-label="缩放"]')).not.toBeNull();
    expect(root?.querySelector('[aria-label="小地图"]')).not.toBeNull();
    expect(root?.querySelector('[aria-label="画布菜单"]')).toBeNull();
    const tick = timer.mock.calls.find(([, ms]) => ms === 15_000)?.[0];
    if (typeof tick !== "function") throw new Error("共享画布未定时刷新");
    await dom.act(async () => tick());
    expect(get.mock.calls[1]?.[1]).toEqual({ revision: 1 });
    expect(root?.querySelector<HTMLInputElement>('[aria-label="画布名称"]')?.value).toBe("第一版");
    await dom.act(async () => tick());
    expect(root?.querySelector<HTMLInputElement>('[aria-label="画布名称"]')?.value).toBe("第二版");
    await view.unmount();
    await dom.flush(20);
  } finally {
    timer.mockRestore();
    get.mockRestore();
    dom.restore();
  }
});

test("查看者可调整刷新间隔，慢请求期间和后台停留时跳过自动检查", async () => {
  const dom = await installDom();
  const { MemoryRouter, Routes, Route } = await import("react-router");
  const { default: Reader } = await import("@/pages/canvas/shared-reader");
  const first = {
    id: "c1",
    title: "当前版本",
    revision: 1,
    payload_json: { nodes: [], edges: [] },
  };
  const pending = Promise.withResolvers<{ unchanged: true }>();
  const get = spyOn(service, "get")
    .mockResolvedValueOnce(first)
    .mockReturnValueOnce(pending.promise)
    .mockResolvedValue({ unchanged: true });
  const timer = spyOn(dom.win, "setInterval");
  const page = (
    <MemoryRouter initialEntries={["/canvas/c1/reader"]}>
      <Routes>
        <Route path="/canvas/:id/reader" element={<Reader />} />
      </Routes>
    </MemoryRouter>
  );
  try {
    const view = await dom.mount(page);
    try {
      const trigger = view.container.querySelector('[aria-label="自动刷新设置"]');
      expect(Boolean(trigger)).toBe(true);
      await dom.click(trigger);
      const slider = document.querySelector('input[type="range"]');
      if (!slider) throw new Error("缺少自定义间隔滑杆");
      expect(slider.getAttribute("min")).toBe("1");
      await dom.act(async () =>
        slider.dispatchEvent(new dom.win.KeyboardEvent("keydown", { key: "Home", bubbles: true })),
      );
      const tick = timer.mock.calls.findLast(([, ms]) => ms === 1_000)?.[0];
      if (typeof tick !== "function") throw new Error("未启用每秒刷新");
      await dom.act(async () => tick());
      await dom.act(async () => tick());
      expect(get).toHaveBeenCalledTimes(2);
      expect(view.container.textContent).toContain("当前版本");

      await dom.act(async () =>
        slider.dispatchEvent(new dom.win.KeyboardEvent("keydown", { key: "End", bubbles: true })),
      );
      expect(timer.mock.calls.some(([, ms]) => ms === 60_000)).toBe(true);
      expect(get).toHaveBeenCalledTimes(2);
      expect(get.mock.calls[1]?.[2]?.signal?.aborted).toBe(false);
      await dom.act(async () => pending.resolve({ unchanged: true }));

      const customTick = timer.mock.calls.findLast(([, ms]) => ms === 60_000)?.[0];
      if (typeof customTick !== "function") throw new Error("缺少自定义检查定时器");
      Object.defineProperty(document, "hidden", { configurable: true, value: true });
      await dom.act(async () => customTick());
      expect(get).toHaveBeenCalledTimes(2);
      Object.defineProperty(document, "hidden", { configurable: true, value: false });
      await dom.act(async () => customTick());
      expect(get).toHaveBeenCalledTimes(3);
    } finally {
      await view.unmount();
    }
    timer.mockClear();
    get.mockResolvedValueOnce(first);
    const reopened = await dom.mount(page);
    try {
      expect(timer.mock.calls.some(([, ms]) => ms === 60_000)).toBe(true);
    } finally {
      await reopened.unmount();
    }
  } finally {
    timer.mockRestore();
    get.mockRestore();
    dom.restore();
  }
});

test("不刷新会停止自动检查并保留当前画布，仍可手动刷新", async () => {
  const dom = await installDom();
  localStorage.setItem("canvas:shared-refresh-seconds", "23");
  const { MemoryRouter, Routes, Route } = await import("react-router");
  const { default: Reader } = await import("@/pages/canvas/shared-reader");
  const first = { id: "c1", title: "第一版", revision: 1, payload_json: { nodes: [], edges: [] } };
  const pending = Promise.withResolvers<typeof first>();
  const get = spyOn(service, "get")
    .mockResolvedValueOnce(first)
    .mockReturnValueOnce(pending.promise)
    .mockResolvedValueOnce({ ...first, title: "手动更新后", revision: 3 })
    .mockResolvedValue(first);
  const timer = spyOn(dom.win, "setInterval");
  const clearTimer = spyOn(dom.win, "clearInterval");
  const page = (
    <MemoryRouter initialEntries={["/canvas/c1/reader"]}>
      <Routes>
        <Route path="/canvas/:id/reader" element={<Reader />} />
      </Routes>
    </MemoryRouter>
  );
  try {
    const view = await dom.mount(page);
    try {
      const timerIndex = timer.mock.calls.findIndex(([, ms]) => ms === 23_000);
      const tick = timer.mock.calls[timerIndex]?.[0];
      if (typeof tick !== "function") throw new Error("自动刷新未启用");
      await dom.act(async () => tick());
      await dom.click(view.container.querySelector('[aria-label="自动刷新设置"]'));
      const disabledOption = document.querySelector<HTMLInputElement>('input[value="disabled"]');
      expect(Boolean(disabledOption)).toBe(true);
      await dom.click(disabledOption);
      expect(disabledOption?.checked).toBe(true);
      expect(document.querySelector('input[type="range"]')).toBeNull();
      expect(clearTimer).toHaveBeenCalledWith(timer.mock.results[timerIndex]?.value);
      expect(get.mock.calls[1]?.[2]?.signal?.aborted).toBe(true);
      await dom.act(async () => pending.resolve({ ...first, title: "不应显示", revision: 2 }));
      await dom.act(async () => window.dispatchEvent(new window.Event("focus")));
      expect(get).toHaveBeenCalledTimes(2);
      expect(view.container.textContent).toContain("第一版");
      const refresh = [...view.container.querySelectorAll("button")].find(
        (button) => button.textContent === "刷新",
      );
      await dom.click(refresh);
      expect(get).toHaveBeenCalledTimes(3);
      expect(view.container.textContent).toContain("手动更新后");
    } finally {
      await view.unmount();
    }
    timer.mockClear();
    const reopened = await dom.mount(page);
    try {
      expect(timer.mock.calls.some(([, ms]) => ms === 23_000)).toBe(false);
      await dom.click(reopened.container.querySelector('[aria-label="自动刷新设置"]'));
      await dom.click(document.querySelector('input[value="automatic"]'));
      expect(timer.mock.calls.some(([, ms]) => ms === 23_000)).toBe(true);
      expect(document.querySelector<HTMLInputElement>('input[type="range"]')?.value).toBe("23");
    } finally {
      await reopened.unmount();
    }
  } finally {
    clearTimer.mockRestore();
    timer.mockRestore();
    get.mockRestore();
    dom.restore();
  }
});

test("退出共享先说明权限变化，取消保留画布，确认成功后返回共享列表", async () => {
  const dom = await installDom();
  const { MemoryRouter, Routes, Route, useLocation } = await import("react-router");
  const { default: Reader } = await import("@/pages/canvas/shared-reader");
  const get = spyOn(service, "get").mockResolvedValue({
    id: "c1",
    title: "朋友的画布",
    revision: 1,
    payload_json: { nodes: [], edges: [] },
  });
  const pending = Promise.withResolvers<Awaited<ReturnType<typeof canvasSharing.remove>>>();
  const remove = spyOn(canvasSharing, "remove").mockReturnValue(pending.promise);
  function SharedListLocation() {
    return <output>{useLocation().search}</output>;
  }
  try {
    const view = await dom.mount(
      <MemoryRouter initialEntries={["/canvas/c1/reader"]}>
        <Routes>
          <Route path="/canvas/:id/reader" element={<Reader />} />
          <Route path="/canvases" element={<SharedListLocation />} />
        </Routes>
      </MemoryRouter>,
    );
    try {
      const leave = [...view.container.querySelectorAll("button")].find(
        (button) => button.textContent === "退出共享",
      );
      await dom.click(leave);
      const dialog = document.querySelector('[role="alertdialog"]');
      expect(dialog?.textContent).toContain("失去查看权限");
      expect(dialog?.textContent).toContain("从你的「共享给我」列表中移除");
      expect(dialog?.textContent).toContain("联系分享者重新共享");
      expect(remove).not.toHaveBeenCalled();

      await dom.click(dialog?.querySelector('[data-slot="alert-dialog-cancel"]'));
      expect(remove).not.toHaveBeenCalled();
      expect(view.container.textContent).toContain("朋友的画布");

      await dom.click(leave);
      const confirm = document.querySelector<HTMLButtonElement>(
        '[data-slot="alert-dialog-action"]',
      );
      expect(confirm?.textContent).toBe("确认退出共享");
      await dom.click(confirm);
      expect(remove.mock.calls).toEqual([["c1", "me"]]);
      expect(confirm?.disabled).toBe(true);
      await dom.click(confirm);
      expect(remove).toHaveBeenCalledTimes(1);
      expect(view.container.textContent).toContain("朋友的画布");

      await dom.act(async () => pending.resolve(undefined));
      expect(view.container.textContent).toBe("?scope=shared-canvas");
    } finally {
      await view.unmount();
    }
  } finally {
    remove.mockRestore();
    get.mockRestore();
    dom.restore();
  }
});

test("共享画布在角色素材和导演台封面加载失败时显示节点级提示", async () => {
  const dom = await installDom();
  const imageConstructor = Object.getOwnPropertyDescriptor(globalThis, "Image");
  Object.defineProperty(globalThis, "Image", { configurable: true, value: dom.win.Image });
  const { SharedCanvasScene } = await import("@/components/canvas-editor/shared-scene");
  const src = "/api/v1/canvas-media/c1/missing";
  try {
    const view = await dom.mount(
      <SharedCanvasScene
        overlay={() => null}
        graph={{
          nodes: [
            {
              id: "role",
              type: "canvas",
              position: { x: 0, y: 0 },
              data: {
                kind: "character",
                label: "角色",
                src,
                character: {
                  name: "角色",
                  look: "造型",
                  description: "",
                  voice: null,
                  extras: [{ assetId: "extra", src: `${src}-extra` }],
                },
              },
            },
            {
              id: "director",
              type: "canvas",
              position: { x: 400, y: 0 },
              data: {
                kind: "director",
                label: "导演台",
                scene: {
                  ...createDefaultScene(),
                  thumb: { url: `${src}-scene`, assetId: "scene" },
                },
              },
            },
          ],
          edges: [],
        }}
      />,
    );
    const images = view.container.querySelectorAll(`img[src^="${src}"]`);
    expect(images.length).toBe(3);
    await dom.act(async () => {
      for (const image of images) image.dispatchEvent(new dom.win.Event("error"));
    });
    expect(view.container.querySelectorAll('[aria-label="素材不可用"]').length).toBe(3);
    expect(view.container.textContent).toContain("进入导演台");
    await view.unmount();
  } finally {
    if (imageConstructor) Object.defineProperty(globalThis, "Image", imageConstructor);
    else Reflect.deleteProperty(globalThis, "Image");
    dom.restore();
  }
});

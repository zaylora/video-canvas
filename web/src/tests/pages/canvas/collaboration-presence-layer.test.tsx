import { expect, mock, test } from "bun:test";
import { ReactFlowProvider, useStoreApi } from "@xyflow/react";
import { useLayoutEffect, useRef } from "react";

import {
  CanvasPresenceLayer,
  CanvasPresenceRoster,
} from "@/components/canvas-editor/collaboration-presence";
import {
  CanvasEditorApiProvider,
  type CanvasEditorApi,
} from "@/components/canvas-editor/editor-api";
import { installDom } from "@/tests/helpers/dom";
import { CanvasRoomPresence } from "@/utils/ws/canvas-presence";

test.each([
  ["小林", "小林"],
  [undefined, ""],
] as const)("远端光标显示账号名称（%s）", async (actorUsername, label) => {
  const dom = await installDom();
  const presence = new CanvasRoomPresence("canvas", 3);
  const api = { canvasId: "canvas" } as CanvasEditorApi;
  function CanvasViewport() {
    const store = useStoreApi();
    const element = useRef<HTMLDivElement>(null);
    useLayoutEffect(() => store.setState({ domNode: element.current }), [store]);
    return (
      <div ref={element}>
        <div className="react-flow__viewport-portal" />
        <CanvasPresenceLayer presence={presence} visible />
      </div>
    );
  }
  const state = { epoch: 3, actorId: "actor659C", actorUsername, cursor: { x: 10, y: 20 } };
  presence.receive(state);
  const app = await dom.mount(
    <ReactFlowProvider>
      <CanvasEditorApiProvider value={api}>
        <CanvasViewport />
      </CanvasEditorApiProvider>
    </ReactFlowProvider>,
  );
  try {
    expect(app.container.querySelector(".react-flow__viewport-portal")?.textContent).toBe(label);
  } finally {
    await app.unmount();
    dom.restore();
  }
});

test("在线列表收到用户名后可按用户名定位及跟随", async () => {
  const dom = await installDom();
  const presence = new CanvasRoomPresence("canvas", 3);
  const api = { canvasId: "canvas" } as CanvasEditorApi;
  const state = { epoch: 3, actorId: "actor659C", view: { x: 10, y: 20, zoom: 1 } };
  presence.receive(state);
  const app = await dom.mount(
    <ReactFlowProvider>
      <CanvasEditorApiProvider value={api}>
        <CanvasPresenceRoster presence={presence} />
      </CanvasEditorApiProvider>
    </ReactFlowProvider>,
  );
  try {
    expect(app.container.querySelector("button")).toBeNull();
    await dom.act(() => presence.receive({ ...state, actorUsername: "小林" }));
    expect(app.container.querySelector('[aria-label="定位小林"]')).not.toBeNull();
    await dom.click(app.container.querySelector('[aria-label="跟随小林的视角"]'));
    await dom.act(() => presence.receive(state));
    expect(app.container.querySelector('[role="status"]')?.textContent).toBe("正在跟随小林");
  } finally {
    await app.unmount();
    dom.restore();
  }
});

test.each(["退出", "切换成员"] as const)(
  "%s跟随后，同一批次到达的旧视口不能移动画布",
  async (action) => {
    const dom = await installDom();
    const presence = new CanvasRoomPresence("canvas", 3);
    const setCenter = mock(async (_x: number, _y: number, _options?: { zoom?: number }) => true);
    const api = { canvasId: "canvas" } as CanvasEditorApi;
    const receiveView = (actorId: string, x: number) =>
      presence.receive({ epoch: 3, actorId, view: { x, y: 30, zoom: 1.2 } });
    function ObserveViewport() {
      const store = useStoreApi();
      useLayoutEffect(() => store.setState({ setCenter }), [store]);
      return null;
    }
    receiveView("first", 20);
    receiveView("second", 40);
    expect(presence.follow("first")).toBe(true);
    const app = await dom.mount(
      <ReactFlowProvider>
        <CanvasEditorApiProvider value={api}>
          <ObserveViewport />
          <CanvasPresenceLayer presence={presence} visible={false} />
        </CanvasEditorApiProvider>
      </ReactFlowProvider>,
    );
    try {
      expect(setCenter.mock.calls).toEqual([[20, 30, { zoom: 1.2 }]]);
      setCenter.mockClear();
      await dom.act(async () => {
        if (action === "退出")
          window.dispatchEvent(new KeyboardEvent("keydown", { key: "Escape" }));
        else expect(presence.follow("second")).toBe(true);
        receiveView("first", 90);
      });
      expect(setCenter.mock.calls).toEqual(action === "退出" ? [] : [[40, 30, { zoom: 1.2 }]]);
    } finally {
      await app.unmount();
      dom.restore();
    }
  },
);

import { afterAll, afterEach, beforeAll, beforeEach, describe, expect, mock, test } from "bun:test";
import { useEffect } from "react";

import * as assetApi from "@/api/asset";
import type {
  NodeActions,
  registerNodeActions as RegisterNodeActions,
} from "@/components/canvas-editor/node-actions";
import { installDom, type DomEnv } from "@/tests/helpers/dom";
import type { useModelsStore as UseModelsStore } from "@/store/models";
import type { useSettingsStore as UseSettingsStore } from "@/store/settings";
import type { useStoryboardEditor as UseEditorStore } from "@/store/storyboard-editor";
import type { CanvasEdge, CanvasNode, StoryboardShot } from "@/types";

/**
 * 分镜表按行生成分镜图（AC7、AC8）：用 jsdom 挂载真实弹窗和分镜图格，节点数据放在 ReactFlowProvider 里。
 * 生成本身由图片节点组件登记的「提交」动作完成，这里换成可数的假动作，检查建了什么节点、连了什么线、提示词是什么。
 */

type UploadFn = (
  file: File,
  options?: { onProgress?: (percent: number) => void },
) => Promise<{ id: string; url: string }>;
let upload: UploadFn = async () => ({ id: "1", url: "/f/1.png" });
let uploadCalls = 0;
// 只换上传动作，其余导出原样留着：mock 少给一个导出，别的模块 import 进来就是模块加载失败
mock.module("@/api/asset", () => ({
  ...assetApi,
  uploadAsset: (file: File, options?: { onProgress?: (percent: number) => void }) => {
    uploadCalls += 1;
    return upload(file, options);
  },
}));

let dom: DomEnv;
let useModels: typeof UseModelsStore;
let useSettings: typeof UseSettingsStore;
let useEditor: typeof UseEditorStore;
let registerNodeActions: typeof RegisterNodeActions;
let Harness: (props: { shots: StoryboardShot[]; nodes?: CanvasNode[] }) => React.ReactElement;
const seen: { nodes: CanvasNode[]; edges: CanvasEdge[] } = { nodes: [], edges: [] };
const offs: Array<() => void> = [];
/** 在挂载好的画布里改某个节点的数据（Body 渲染时把 updateNodeData 放进来） */
let setNodeData: (id: string, patch: Partial<CanvasNode["data"]>) => void = () => {};
/** 在挂载好的画布里删掉一个节点（连同它的连线），和用户在画布上删节点走同一条路径 */
let removeNode: (id: string) => void = () => {};

beforeAll(async () => {
  dom = await installDom();
  const { ReactFlowProvider, useNodes, useEdges, useStoreApi, useReactFlow } =
    await import("@xyflow/react");
  const { StoryboardDialog } = await import("@/components/storyboard/storyboard-dialog");
  const { CanvasIdContext } = await import("@/utils/canvas/canvas-id");
  useModels = (await import("@/store/models")).useModelsStore;
  useSettings = (await import("@/store/settings")).useSettingsStore;
  useEditor = (await import("@/store/storyboard-editor")).useStoryboardEditor;
  registerNodeActions = (await import("@/components/canvas-editor/node-actions"))
    .registerNodeActions;

  function Body() {
    const store = useStoreApi<CanvasNode>();
    // 只有 Provider 没有 <ReactFlow> 时，store 默认不收节点和连线的改动：按画布里非受控的方式打开
    if (!store.getState().hasDefaultNodes) store.setState({ hasDefaultNodes: true });
    if (!store.getState().hasDefaultEdges) store.setState({ hasDefaultEdges: true });
    const nodes = useNodes<CanvasNode>();
    const edges = useEdges<CanvasEdge>();
    const { updateNodeData, deleteElements } = useReactFlow<CanvasNode, CanvasEdge>();
    const node = nodes.find((n) => n.id === "sb")!;
    useEffect(() => {
      seen.nodes = nodes;
      seen.edges = edges;
      setNodeData = (id, patch) => updateNodeData(id, patch);
      removeNode = (id) => void deleteElements({ nodes: [{ id }] });
    });
    return (
      <StoryboardDialog
        id="sb"
        label={node.data.label}
        board={node.data.storyboard ?? { shots: [] }}
        open
        focusShotId={null}
        onOpenChange={() => {}}
      />
    );
  }
  Harness = ({ shots, nodes = [] }) => (
    <CanvasIdContext.Provider value="c1">
      <ReactFlowProvider
        initialNodes={[
          {
            id: "sb",
            type: "canvas",
            position: { x: 700, y: 0 },
            data: { kind: "storyboard", label: "分镜表", storyboard: { shots } },
          },
          ...nodes,
        ]}
      >
        <Body />
      </ReactFlowProvider>
    </CanvasIdContext.Provider>
  );
});
afterAll(() => {
  useModels.setState({ byKind: {} });
  useSettings.setState({ defaultModels: {} });
  dom.restore();
});

const mounted: Array<() => Promise<void>> = [];
async function mount(element: React.ReactElement) {
  const m = await dom.mount(element);
  let done = false;
  const unmount = async () => {
    if (done) return;
    done = true;
    await m.unmount();
  };
  mounted.push(unmount);
  return { ...m, unmount };
}
afterEach(async () => {
  for (const unmount of mounted.splice(0)) await unmount();
  for (const off of offs.splice(0)) off();
});

const imageModels = (...keys: string[]) =>
  ({
    status: "ready",
    loadedAt: Date.now(),
    models: keys.map((key) => ({ key, label: key, vendor: "x" })),
  }) as never;

beforeEach(() => {
  upload = async () => ({ id: "1", url: "/f/1.png" });
  uploadCalls = 0;
  seen.nodes = [];
  seen.edges = [];
  useEditor.setState({ nodeId: null, focusShotId: null });
  useSettings.setState({ defaultModels: {} });
  useModels.setState({
    byKind: { agent: imageModels("agent-a"), image: imageModels("img-1", "img-2") },
  });
});

const shot = (id: string, scene: string, patch: Partial<StoryboardShot> = {}): StoryboardShot => ({
  id,
  scene,
  shot: "中景",
  camera: "固定",
  action: "",
  dialogue: "",
  duration: 3,
  ...patch,
});
const imageNode = (id: string, data: Partial<CanvasNode["data"]> = {}): CanvasNode => ({
  id,
  type: "canvas",
  position: { x: 1356, y: 0 },
  data: { kind: "image", label: id, ...data },
});

const videoNode = (id: string, data: Partial<CanvasNode["data"]> = {}): CanvasNode => ({
  id,
  type: "canvas",
  position: { x: 1356, y: 400 },
  data: { kind: "video", label: id, ...data },
});

const body = () => dom.win.document.body;
const buttonByText = (text: string, index = 0) =>
  Array.from(body().querySelectorAll<HTMLButtonElement>("button")).filter(
    (b) => b.textContent?.replace(/\s+/g, "") === text.replace(/\s+/g, ""),
  )[index] ?? null;

/** 一个可手动放行的上传，用来停在「上传中」检查界面 */
function deferredAsset() {
  let resolve!: (v: { id: string; url: string }) => void;
  const promise = new Promise<{ id: string; url: string }>((res) => {
    resolve = res;
  });
  return { promise, resolve };
}

/** 把文件拖到某个元素上：dragover 之后 drop */
async function dropOn(el: HTMLElement, files: File[]) {
  await dom.act(async () => {
    for (const type of ["dragover", "drop"] as const) {
      const event = new dom.win.Event(type, { bubbles: true, cancelable: true });
      Object.defineProperty(event, "dataTransfer", { value: { files, types: ["Files"] } });
      el.dispatchEvent(event);
    }
  });
}
const byTitle = (title: string, index = 0) =>
  body().querySelectorAll<HTMLElement>(`button[title="${title}"]`)[index] ?? null;
const board = () => seen.nodes.find((n) => n.id === "sb")!.data.storyboard!.shots;
const newImageNodes = (known: string[] = []) =>
  seen.nodes.filter((n) => n.data.kind === "image" && !known.includes(n.id));

/** 一个可数的「提交」动作：注册给指定节点，模拟图片节点挂载后登记的动作 */
function fakeActions(id: string, submitted: { n: number }, blocked: string | null = null) {
  const actions: NodeActions = {
    blockedReason: () => blocked,
    submit: async () => {
      submitted.n += 1;
      return null;
    },
  };
  offs.push(registerNodeActions(id, () => actions));
}

describe("按行生成分镜图（AC7）", () => {
  test("没有图片节点时：在分镜表右侧按镜号竖排建一个、连线过去，提示词按模板由这一行拼出，行上记下关联", async () => {
    const m = await mount(
      <Harness
        shots={[
          shot("a", "月夜瓜地"),
          shot("b", "闰土举叉", { shot: "近景", camera: "推", action: "猫腰前行" }),
        ]}
      />,
    );
    await dom.flush();
    await dom.click(byTitle("生成分镜图", 1));
    await dom.flush(30);

    const [created] = newImageNodes();
    expect(created).toBeTruthy();
    expect(created.data.prompt).toBe("近景，推。闰土举叉。猫腰前行。");
    expect(created.data.params?.prompt).toBe("近景，推。闰土举叉。猫腰前行。");
    // 第 2 镜 → 第 1 格（从 0 起）：右侧 80px，往下一个节点高度加 40 间隔
    expect(created.position).toEqual({ x: 700 + 576 + 80, y: 324 + 40 });
    expect(seen.edges.map((e) => [e.source, e.target])).toEqual([["sb", created.id]]);
    expect(board()[1].imageNodeId).toBe(created.id);
    expect(board()[0].imageNodeId).toBeUndefined();
    await m.unmount();
  });

  test("建好就自动开始生成：节点登记了提交动作后立刻提交（用节点自己的模型和参数）", async () => {
    const submitted = { n: 0 };
    const m = await mount(<Harness shots={[shot("a", "月夜瓜地")]} />);
    await dom.flush();
    await dom.click(byTitle("生成分镜图"));
    await dom.flush(30);
    fakeActions(newImageNodes()[0].id, submitted);
    await dom.flush(400);
    expect(submitted.n).toBe(1);
    await m.unmount();
  });

  test("新节点用设置里的默认图片模型；默认模型已不在清单里就不指定，让节点取第一个", async () => {
    useSettings.setState({ defaultModels: { image: "img-2" } });
    const m = await mount(<Harness shots={[shot("a", "甲"), shot("b", "乙")]} />);
    await dom.flush();
    await dom.click(byTitle("生成分镜图", 0));
    await dom.flush(30);
    expect(newImageNodes()[0].data.model).toBe("img-2");

    useSettings.setState({ defaultModels: { image: "gone" } });
    await dom.click(byTitle("生成分镜图", 1));
    await dom.flush(30);
    const second = newImageNodes().find((n) => n.data.prompt?.includes("乙"))!;
    expect(second.data.model).toBeUndefined();
    await m.unmount();
  });

  test("同一行连点只建一个节点", async () => {
    const m = await mount(<Harness shots={[shot("a", "月夜瓜地")]} />);
    await dom.flush();
    const btn = byTitle("生成分镜图");
    await dom.click(btn);
    await dom.click(btn);
    await dom.flush(30);
    expect(newImageNodes()).toHaveLength(1);
    await m.unmount();
  });

  test("画面描述是空的：不建节点，提示先写画面", async () => {
    const m = await mount(<Harness shots={[shot("a", "  ")]} />);
    await dom.flush();
    await dom.click(byTitle("生成分镜图"));
    await dom.flush(30);
    expect(newImageNodes()).toHaveLength(0);
    expect(board()[0].imageNodeId).toBeUndefined();
    await m.unmount();
  });
});

describe("重新生成复用同一个图片节点（AC8）", () => {
  test("已关联图片节点：按钮是「重新生成」，不新建节点，用这一行现在的内容重写提示词再生成", async () => {
    const submitted = { n: 0 };
    fakeActions("i1", submitted);
    const m = await mount(
      <Harness
        shots={[shot("a", "改过的画面", { imageNodeId: "i1", camera: "拉" })]}
        nodes={[
          imageNode("i1", {
            status: "done",
            src: "/f/old.png",
            prompt: "旧的",
            params: { prompt: "旧的", ratio: "16:9" },
          }),
        ]}
      />,
    );
    await dom.flush();
    expect(byTitle("生成分镜图")).toBeNull();
    await dom.click(byTitle("重新生成"));
    await dom.flush(200);
    expect(newImageNodes(["i1"])).toHaveLength(0);
    const node = seen.nodes.find((n) => n.id === "i1")!;
    expect(node.data.prompt).toBe("中景，拉。改过的画面。");
    // 用户在节点上调过的其它参数保留
    expect(node.data.params).toEqual({ prompt: "中景，拉。改过的画面。", ratio: "16:9" });
    expect(submitted.n).toBe(1);
    await m.unmount();
  });

  test("关联的图片节点被删了：打开弹窗时解除关联，这一行恢复成「生成分镜图」", async () => {
    const m = await mount(<Harness shots={[shot("a", "月夜瓜地", { imageNodeId: "deleted" })]} />);
    await dom.flush();
    expect(board()[0].imageNodeId).toBeUndefined();
    expect(byTitle("生成分镜图")).not.toBeNull();
    expect(byTitle("重新生成")).toBeNull();
    await m.unmount();
  });
});

describe("分镜图格实时显示关联节点的状态", () => {
  const withNode = (data: Partial<CanvasNode["data"]>) => (
    <Harness
      shots={[shot("a", "月夜瓜地", { imageNodeId: "i1" })]}
      nodes={[imageNode("i1", data)]}
    />
  );

  test("生成中：显示「生成中」，没有生成按钮，只能定位", async () => {
    const m = await mount(withNode({ status: "running" }));
    await dom.flush();
    expect(body().textContent).toContain("生成中");
    expect(byTitle("生成分镜图")).toBeNull();
    expect(byTitle("重新生成")).toBeNull();
    expect(byTitle("在画布中定位")).not.toBeNull();
    await m.unmount();
  });

  test("生成完：显示节点当前的图，带链接角标，可重新生成", async () => {
    const m = await mount(withNode({ status: "done", src: "/f/new.png" }));
    await dom.flush();
    expect(body().querySelector<HTMLImageElement>('img[alt="第 1 镜分镜图"]')?.src).toContain(
      "/f/new.png",
    );
    expect(byTitle("重新生成")).not.toBeNull();
    await m.unmount();
  });

  test("节点上换了一张图（生成了新版本）：这一格实时跟着变，不用重新打开弹窗", async () => {
    const m = await mount(withNode({ status: "done", src: "/f/v1.png" }));
    await dom.flush();
    const img = () => body().querySelector<HTMLImageElement>('img[alt="第 1 镜分镜图"]');
    expect(img()?.src).toContain("v1.png");
    // 和任务回填写节点走同一条路径：直接改节点数据
    await dom.act(async () => {
      setNodeData("i1", { src: "/f/v2.png" });
    });
    expect(img()?.src).toContain("v2.png");
    await m.unmount();
  });

  test("生成失败：显示「生成失败」，悬停给原因，可重新生成", async () => {
    const m = await mount(withNode({ status: "error", error: "积分不足" }));
    await dom.flush();
    expect(body().textContent).toContain("生成失败");
    expect(body().querySelector('[title="积分不足"]')).not.toBeNull();
    expect(byTitle("重新生成")).not.toBeNull();
    await m.unmount();
  });
});

describe("在画布中定位", () => {
  test("点定位：关掉分镜表弹窗", async () => {
    useEditor.setState({ nodeId: "sb", focusShotId: null });
    const m = await mount(
      <Harness
        shots={[shot("a", "月夜瓜地", { imageNodeId: "i1" })]}
        nodes={[imageNode("i1", { status: "done", src: "/f/a.png" })]}
      />,
    );
    await dom.flush();
    await dom.click(byTitle("在画布中定位"));
    expect(useEditor.getState().nodeId).toBeNull();
    await m.unmount();
  });
});

describe("手动上传分镜图（AC9）", () => {
  const png = (name = "a.png") => new File([new Uint8Array(1024)], name, { type: "image/png" });
  const cell = () => body().querySelector<HTMLElement>('[data-slot="shot-image-cell"]')!;

  test("拖入图片：没有图片节点时在分镜表右侧建一个上传素材的图片节点、连线过去，行上记下关联", async () => {
    upload = async () => ({ id: "9", url: "/files/9.png" });
    const m = await mount(<Harness shots={[shot("a", "甲"), shot("b", "乙")]} />);
    await dom.flush();
    await dropOn(body().querySelectorAll<HTMLElement>('[data-slot="shot-image-cell"]')[1], [png()]);
    await dom.flush(30);

    const [created] = newImageNodes();
    expect(created).toBeTruthy();
    expect(created.data).toMatchObject({
      kind: "image",
      label: "镜头 02",
      status: "done",
      src: "/files/9.png",
      assetId: "9",
      mediaType: "image",
      uploaded: true,
      fileName: "a.png",
    });
    // 和按行生成同一个位置：第 2 镜 → 第 1 格
    expect(created.position).toEqual({ x: 700 + 576 + 80, y: 324 + 40 });
    expect(seen.edges.map((e) => [e.source, e.target])).toEqual([["sb", created.id]]);
    expect(board()[1].imageNodeId).toBe(created.id);
    expect(board()[1].image).toBeUndefined();
    // 来自节点：有链接角标，可以在画布中定位；上传的图不自动提交生成
    expect(body().querySelector('[data-slot="shot-image-link"]')).not.toBeNull();
    expect(byTitle("在画布中定位")).not.toBeNull();
    await m.unmount();
  });

  test("拖入图片：已有图片节点时不新建，上传的图作为这个节点的新一版，旧版留在历史里", async () => {
    upload = async () => ({ id: "9", url: "/files/9.png" });
    const m = await mount(
      <Harness
        shots={[shot("a", "月夜瓜地", { imageNodeId: "i1" })]}
        nodes={[imageNode("i1", { status: "done", src: "/files/8.png", assetId: "8" })]}
      />,
    );
    await dom.flush();
    await dropOn(cell(), [png()]);
    await dom.flush(30);
    expect(newImageNodes(["i1"])).toHaveLength(0);
    const node = seen.nodes.find((n) => n.id === "i1")!;
    expect(node.data.src).toBe("/files/9.png");
    expect(node.data.assetId).toBe("9");
    expect(node.data.outputs?.map((o) => o.assetId)).toEqual(["8", "9"]);
    expect(board()[0].imageNodeId).toBe("i1");
    await m.unmount();
  });

  test("分镜图格显示缩略图；点「查看大图」打开预览看原图", async () => {
    const m = await mount(
      <Harness
        shots={[shot("a", "月夜瓜地", { imageNodeId: "i1" })]}
        nodes={[imageNode("i1", { status: "done", src: "/files/8.png", assetId: "8" })]}
      />,
    );
    await dom.flush();
    const thumb = cell().querySelector<HTMLImageElement>("img");
    expect(thumb?.getAttribute("src")).toBe("/files/8.png?v=thumb");
    expect(cell().querySelector('img[src="/files/8.png"]')).toBeNull();
    await dom.click(byTitle("查看大图"));
    await dom.flush(30);
    const popup = body().querySelector('[data-slot="media-lightbox"]');
    expect(popup).not.toBeNull();
    expect(popup!.querySelector('img[src="/files/8.png"]')).not.toBeNull();
    await m.unmount();
  });

  test("各行各传、互不阻塞：两行同时上传各显示自己的进度，各自建节点；同一行传的时候再拖进来不理", async () => {
    const pending = new Map<string, ReturnType<typeof deferredAsset>>();
    upload = (file, options) => {
      options?.onProgress?.(file.name === "a.png" ? 60 : 30);
      const d = deferredAsset();
      pending.set(file.name, d);
      return d.promise;
    };
    const m = await mount(<Harness shots={[shot("a", "甲"), shot("b", "乙")]} />);
    await dom.flush();
    const cells = () => body().querySelectorAll<HTMLElement>('[data-slot="shot-image-cell"]');
    await dropOn(cells()[0], [png("a.png")]);
    await dropOn(cells()[1], [png("b.png")]);
    await dom.flush();
    expect(uploadCalls).toBe(2);
    expect(cells()[0].textContent).toContain("上传中 60%");
    expect(cells()[1].textContent).toContain("上传中 30%");
    // 上传中的格子不再叠着空白占位图标
    expect(cells()[0].querySelector("svg.lucide-image")).toBeNull();

    // 第 1 行还在传：再拖一张到这一行不理
    await dropOn(cells()[0], [png("c.png")]);
    await dom.flush();
    expect(uploadCalls).toBe(2);

    pending.get("b.png")!.resolve({ id: "8", url: "/files/8.png" });
    await dom.flush(30);
    expect(cells()[1].textContent).not.toContain("上传中");
    expect(cells()[0].textContent).toContain("上传中 60%");
    pending.get("a.png")!.resolve({ id: "9", url: "/files/9.png" });
    await dom.flush(30);
    expect(body().textContent).not.toContain("上传中");
    const assetOf = (i: number) =>
      seen.nodes.find((n) => n.id === board()[i].imageNodeId)?.data.assetId;
    expect([assetOf(0), assetOf(1)]).toEqual(["9", "8"]);
    expect(newImageNodes()).toHaveLength(2);
    await m.unmount();
  });

  test("拖入的不是图片：没有任何反应，不上传、不提示", async () => {
    const m = await mount(<Harness shots={[shot("a", "月夜瓜地")]} />);
    await dom.flush();
    const calls = uploadCalls;
    await dropOn(cell(), [new File(["x"], "a.txt", { type: "text/plain" })]);
    await dom.flush(30);
    expect(uploadCalls).toBe(calls);
    expect(board()[0].imageNodeId).toBeUndefined();
    await m.unmount();
  });

  test("上传失败：这一格回到原状，不写回", async () => {
    upload = async () => {
      throw new Error("网络断了");
    };
    const m = await mount(<Harness shots={[shot("a", "月夜瓜地")]} />);
    await dom.flush();
    await dropOn(cell(), [png()]);
    await dom.flush(30);
    expect(board()[0].imageNodeId).toBeUndefined();
    expect(newImageNodes()).toHaveLength(0);
    expect(body().textContent).not.toContain("上传中");
    await m.unmount();
  });

  test("拖到这一行的任何位置都算拖到这一格（命中区大一点）", async () => {
    upload = async () => ({ id: "9", url: "/files/9.png" });
    const m = await mount(<Harness shots={[shot("a", "月夜瓜地")]} />);
    await dom.flush();
    await dropOn(body().querySelector<HTMLElement>('[data-shot-id="a"]')!, [png()]);
    await dom.flush(30);
    expect(seen.nodes.find((n) => n.id === board()[0].imageNodeId)?.data.assetId).toBe("9");
    await m.unmount();
  });

  test("旧数据里手传的图：照样显示缩略图，可替换、可移除；移除后回到空，按钮恢复成「生成分镜图」", async () => {
    upload = async () => ({ id: "10", url: "/files/10.png" });
    const m = await mount(
      <Harness shots={[shot("a", "月夜瓜地", { image: { assetId: "9", src: "/files/9.png" } })]} />,
    );
    await dom.flush();
    expect(cell().querySelector("img")?.getAttribute("src")).toBe("/files/9.png?v=thumb");
    expect(byTitle("替换")).not.toBeNull();
    await dom.click(byTitle("移除"));
    await dom.flush();
    expect(board()[0].image).toBeUndefined();
    expect(byTitle("生成分镜图")).not.toBeNull();
    await m.unmount();
  });

  test("没有图时有「上传分镜图」按钮；点它会弹出文件选择框", async () => {
    const m = await mount(<Harness shots={[shot("a", "月夜瓜地")]} />);
    await dom.flush();
    const clicked: string[] = [];
    const real = dom.win.document.createElement.bind(dom.win.document);
    dom.win.document.createElement = ((tag: string) => {
      const el = real(tag);
      if (tag === "input") el.click = () => void clicked.push((el as HTMLInputElement).accept);
      return el;
    }) as typeof document.createElement;
    await dom.click(byTitle("上传分镜图"));
    dom.win.document.createElement = real;
    expect(clicked).toEqual(["image/*"]);
    await m.unmount();
  });
});

describe("关联节点被删：弹窗开着时当场恢复（AC8）", () => {
  test("删掉图片节点：这一行当场变回「生成分镜图」，不用关了弹窗重新打开", async () => {
    const m = await mount(
      <Harness
        shots={[shot("a", "月夜瓜地", { imageNodeId: "i1" })]}
        nodes={[imageNode("i1", { status: "done", src: "/f/a.png" })]}
      />,
    );
    await dom.flush();
    expect(byTitle("重新生成")).not.toBeNull();
    await dom.act(async () => {
      removeNode("i1");
    });
    await dom.flush(30);
    expect(board()[0].imageNodeId).toBeUndefined();
    expect(byTitle("生成分镜图")).not.toBeNull();
    expect(byTitle("重新生成")).toBeNull();
    await m.unmount();
  });

  test("删掉视频节点：这一行当场变回「生成视频」", async () => {
    const m = await mount(
      <Harness
        shots={[shot("a", "月夜瓜地", { videoNodeId: "v1" })]}
        nodes={[videoNode("v1", { status: "idle" })]}
      />,
    );
    await dom.flush();
    expect(body().textContent).toContain("去生成");
    await dom.act(async () => {
      removeNode("v1");
    });
    await dom.flush(30);
    expect(board()[0].videoNodeId).toBeUndefined();
    expect(body().textContent).not.toContain("去生成");
    expect(buttonByText("生成视频")).not.toBeNull();
    await m.unmount();
  });
});

describe("按行生成视频（AC10）", () => {
  const videoCaps = (ops: string[], duration?: Record<string, unknown>) => ({
    key: "vid-1",
    label: "vid-1",
    vendor: "x",
    pricing: {},
    capabilities: {
      ops,
      refs: {
        image: { on: true, max: 4, max_mb: 10 },
        video: { on: false, max: 0, max_mb: 0 },
        audio: { on: false, max: 0, max_mb: 0 },
      },
      prompt: { max_length: 1000 },
      params: duration ? { duration } : undefined,
    },
  });
  const setVideoModels = (...models: ReturnType<typeof videoCaps>[]) =>
    useModels.setState({
      byKind: {
        agent: imageModels("agent-a"),
        image: imageModels("img-1"),
        video: { status: "ready", loadedAt: Date.now(), models },
      } as never,
    });
  const newVideoNodes = (known: string[] = []) =>
    seen.nodes.filter((n) => n.data.kind === "video" && !known.includes(n.id));

  test("建视频节点、从分镜表连线、提示词带对白、时长取模型最接近的值；不自动开始生成", async () => {
    setVideoModels(
      videoCaps(["t2v"], { type: "enum", label: "时长", open: true, options: [5, 10] }),
    );
    const submitted = { n: 0 };
    const m = await mount(
      <Harness shots={[shot("a", "月夜瓜地", { dialogue: "你好", duration: 4 })]} />,
    );
    await dom.flush();
    await dom.click(buttonByText("生成视频"));
    await dom.flush(300);
    const [created] = newVideoNodes();
    expect(created).toBeTruthy();
    expect(created.data.prompt).toBe("中景，固定。月夜瓜地。对白：你好。");
    expect(created.data.params?.duration).toBe(5);
    expect(created.data.model).toBeUndefined();
    expect(seen.edges.map((e) => [e.source, e.target])).toEqual([["sb", created.id]]);
    expect(board()[0].videoNodeId).toBe(created.id);
    // 视频贵，不自动开始：节点就算已经登记了提交动作，也没人去调
    fakeActions(created.id, submitted);
    await dom.flush(300);
    expect(submitted.n).toBe(0);
    await m.unmount();
  });

  test("视频节点竖排在分镜表右侧，和图片节点互不重叠", async () => {
    setVideoModels(videoCaps(["t2v"]));
    const m = await mount(<Harness shots={[shot("a", "甲"), shot("b", "乙")]} />);
    await dom.flush();
    await dom.click(byTitle("生成分镜图", 1));
    await dom.flush(30);
    await dom.click(buttonByText("生成视频", 1));
    await dom.flush(300);
    const img = newImageNodes()[0];
    const vid = newVideoNodes()[0];
    expect(vid.position.x).toBe(img.position.x);
    expect(Math.abs(vid.position.y - img.position.y)).toBeGreaterThanOrEqual(324 + 40);
    await m.unmount();
  });

  test("有已生成的分镜图、模型支持图生视频：图片节点连线进视频节点当首帧", async () => {
    setVideoModels(videoCaps(["t2v", "i2v"]));
    const m = await mount(
      <Harness
        shots={[shot("a", "月夜瓜地", { imageNodeId: "i1" })]}
        nodes={[imageNode("i1", { status: "done", src: "/f/a.png", assetId: "55" })]}
      />,
    );
    await dom.flush();
    await dom.click(buttonByText("生成视频"));
    await dom.flush(300);
    const [created] = newVideoNodes();
    const edges = seen.edges.map((e) => [e.source, e.target]);
    expect(edges).toContainEqual(["sb", created.id]);
    expect(edges).toContainEqual(["i1", created.id]);
    expect(created.data.params?.op).toBe("i2v");
    await m.unmount();
  });

  test("手传的分镜图：写成视频节点的手动参考素材", async () => {
    setVideoModels(videoCaps(["i2v"]));
    const m = await mount(
      <Harness shots={[shot("a", "月夜瓜地", { image: { assetId: "9", src: "/files/9.png" } })]} />,
    );
    await dom.flush();
    await dom.click(buttonByText("生成视频"));
    await dom.flush(300);
    const [created] = newVideoNodes();
    expect(created.data.params?.images).toEqual(["9"]);
    expect(created.data.paramAssets).toEqual({ "9": { url: "/files/9.png", mediaType: "image" } });
    expect(seen.edges.map((e) => [e.source, e.target])).toEqual([["sb", created.id]]);
    await m.unmount();
  });

  test("图片节点有预览图但还没有素材 id（没法被引用）：不当首帧", async () => {
    setVideoModels(videoCaps(["t2v", "i2v"]));
    const m = await mount(
      <Harness
        shots={[shot("a", "月夜瓜地", { imageNodeId: "i1" })]}
        nodes={[imageNode("i1", { status: "done", src: "blob:local-preview" })]}
      />,
    );
    await dom.flush();
    await dom.click(buttonByText("生成视频"));
    await dom.flush(300);
    expect(seen.edges.map((e) => e.source)).not.toContain("i1");
    await m.unmount();
  });

  test("图片节点还没出图：不当首帧，只写提示词", async () => {
    setVideoModels(videoCaps(["t2v", "i2v"]));
    const m = await mount(
      <Harness
        shots={[shot("a", "月夜瓜地", { imageNodeId: "i1" })]}
        nodes={[imageNode("i1", { status: "running" })]}
      />,
    );
    await dom.flush();
    await dom.click(buttonByText("生成视频"));
    await dom.flush(300);
    const [created] = newVideoNodes();
    expect(seen.edges.map((e) => e.source)).not.toContain("i1");
    expect(created.data.params?.op).toBeUndefined();
    await m.unmount();
  });

  test("已建视频节点：按钮变成「未生成 · 去生成」，点它定位过去，不再新建", async () => {
    setVideoModels(videoCaps(["t2v"]));
    useEditor.setState({ nodeId: "sb", focusShotId: null });
    const m = await mount(
      <Harness
        shots={[shot("a", "月夜瓜地", { videoNodeId: "v1" })]}
        nodes={[videoNode("v1", { status: "idle" })]}
      />,
    );
    await dom.flush();
    await dom.click(buttonByText("去生成"));
    expect(useEditor.getState().nodeId).toBeNull();
    expect(newVideoNodes(["v1"])).toHaveLength(0);
    await m.unmount();
  });

  test("四种状态：生成中带进度、已完成可播放、失败给原因", async () => {
    setVideoModels(videoCaps(["t2v"]));
    const m1 = await mount(
      <Harness
        shots={[shot("a", "甲", { videoNodeId: "v1" })]}
        nodes={[videoNode("v1", { status: "running" })]}
      />,
    );
    await dom.flush();
    expect(body().textContent).toContain("生成中");
    await m1.unmount();

    const m2 = await mount(
      <Harness
        shots={[shot("a", "甲", { videoNodeId: "v1" })]}
        nodes={[videoNode("v1", { status: "done", src: "/f/v.mp4" })]}
      />,
    );
    await dom.flush();
    expect(body().textContent).toContain("已完成");
    await m2.unmount();

    const m3 = await mount(
      <Harness
        shots={[shot("a", "甲", { videoNodeId: "v1" })]}
        nodes={[videoNode("v1", { status: "error", error: "模型超时" })]}
      />,
    );
    await dom.flush();
    expect(body().textContent).toContain("失败");
    expect(body().querySelector('[title="模型超时"]')).not.toBeNull();
    expect(buttonByText("去处理")).not.toBeNull();
    await m3.unmount();
  });

  test("时长被模型改了：时长格下面显示黄色「实际 5s」，悬停说明模型支持哪些；一致就没有", async () => {
    setVideoModels(
      videoCaps(["t2v"], { type: "enum", label: "时长", open: true, options: [5, 10] }),
    );
    const m = await mount(
      <Harness
        shots={[shot("a", "甲", { videoNodeId: "v1", duration: 4 })]}
        nodes={[videoNode("v1", { status: "idle", model: "vid-1", params: { duration: 5 } })]}
      />,
    );
    await dom.flush();
    expect(body().textContent).toContain("实际 5s");
    expect(body().querySelector('[title="所选视频模型只支持 5s / 10s"]')).not.toBeNull();
    await m.unmount();

    const m2 = await mount(
      <Harness
        shots={[shot("a", "甲", { videoNodeId: "v1", duration: 5 })]}
        nodes={[videoNode("v1", { status: "idle", model: "vid-1", params: { duration: 5 } })]}
      />,
    );
    await dom.flush();
    expect(body().textContent).not.toContain("实际");
    await m2.unmount();
  });

  test("画面描述为空：不建节点，提示先写画面", async () => {
    setVideoModels(videoCaps(["t2v"]));
    const m = await mount(<Harness shots={[shot("a", "  ")]} />);
    await dom.flush();
    await dom.click(buttonByText("生成视频"));
    await dom.flush(100);
    expect(newVideoNodes()).toHaveLength(0);
    await m.unmount();
  });

  test("同一行连点只建一个节点", async () => {
    setVideoModels(videoCaps(["t2v"]));
    const m = await mount(<Harness shots={[shot("a", "月夜瓜地")]} />);
    await dom.flush();
    const btn = buttonByText("生成视频");
    await dom.click(btn);
    await dom.click(btn);
    await dom.flush(300);
    expect(newVideoNodes()).toHaveLength(1);
    await m.unmount();
  });
});

describe("行菜单的视频项", () => {
  test("「生成视频」「重新生成视频」都在行菜单里，键盘用户不靠悬停也能用", async () => {
    const m = await mount(
      <Harness
        shots={[shot("a", "甲"), shot("b", "乙", { videoNodeId: "v1" })]}
        nodes={[videoNode("v1", { status: "idle" })]}
      />,
    );
    await dom.flush();
    await dom.click(body().querySelector<HTMLElement>('[aria-label="第 1 镜的操作"]'));
    await dom.flush();
    expect(buttonByText("生成视频")).not.toBeNull();
    await m.unmount();
  });
});

describe("批量生成分镜图", () => {
  const rows = () => [
    shot("a", "月夜瓜地"),
    shot("b", "已经出图", { imageNodeId: "i1" }),
    shot("c", "手传的", { image: { assetId: "9", src: "/files/9.png" } }),
    shot("d", "上次失败", { imageNodeId: "i2" }),
    shot("e", "  "),
  ];
  const nodes = () => [
    imageNode("i1", { status: "done", src: "/f/1.png" }),
    imageNode("i2", { status: "error", error: "超时" }),
  ];
  const dialogText = () => body().querySelector('[role="alertdialog"]')?.textContent ?? "";

  test("表头有批量按钮；点了先确认，写明要生成几张、哪些行跳过", async () => {
    const m = await mount(<Harness shots={rows()} nodes={nodes()} />);
    await dom.flush();
    await dom.click(byTitle("批量生成分镜图"));
    await dom.flush();
    expect(dialogText()).toContain("生成 2 张分镜图");
    expect(dialogText()).toContain("1 镜没写画面描述");
    // 确认前什么都不建
    expect(newImageNodes(["i1", "i2"])).toHaveLength(0);
    await m.unmount();
  });

  test("确认后：没图的行新建图片节点并生成，失败的行复用原节点重新生成，其余不动", async () => {
    const submitted = { n: 0 };
    fakeActions("i2", submitted);
    const m = await mount(<Harness shots={rows()} nodes={nodes()} />);
    await dom.flush();
    await dom.click(byTitle("批量生成分镜图"));
    await dom.flush();
    await dom.click(buttonByText("开始生成"));
    await dom.flush(30);

    const created = newImageNodes(["i1", "i2"]);
    expect(created).toHaveLength(1);
    expect(created[0].data.prompt).toBe("中景，固定。月夜瓜地。");
    expect(board().map((s) => s.imageNodeId)).toEqual([
      created[0].id,
      "i1",
      undefined,
      "i2",
      undefined,
    ]);
    fakeActions(created[0].id, submitted);
    await dom.flush(400);
    expect(submitted.n).toBe(2);
    await m.unmount();
  });

  test("取消：不建节点、不生成", async () => {
    const submitted = { n: 0 };
    fakeActions("i2", submitted);
    const m = await mount(<Harness shots={rows()} nodes={nodes()} />);
    await dom.flush();
    await dom.click(byTitle("批量生成分镜图"));
    await dom.flush();
    await dom.click(buttonByText("取消"));
    await dom.flush(200);
    expect(newImageNodes(["i1", "i2"])).toHaveLength(0);
    expect(submitted.n).toBe(0);
    await m.unmount();
  });

  test("每一行都有图了：不弹确认，也不建节点", async () => {
    const m = await mount(
      <Harness
        shots={[shot("b", "已经出图", { imageNodeId: "i1" })]}
        nodes={[imageNode("i1", { status: "done", src: "/f/1.png" })]}
      />,
    );
    await dom.flush();
    await dom.click(byTitle("批量生成分镜图"));
    await dom.flush(30);
    expect(dialogText()).toBe("");
    expect(newImageNodes(["i1"])).toHaveLength(0);
    await m.unmount();
  });
});

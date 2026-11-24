import { afterAll, beforeAll, describe, expect, spyOn, test } from "bun:test";
import { useEffect } from "react";

import type { CanvasNode, StoryboardShot } from "@/types";
import type * as ShotsModule from "@/utils/storyboard/shots";
import { installDom, type DomEnv } from "@/tests/helpers/dom";

/**
 * 分镜表编辑弹窗：用 jsdom 挂载真实组件，节点数据放在 ReactFlowProvider 里，
 * 检查改单元格、时长修正、行菜单增删和 50 镜时只重渲染被改的那一行（AC5、AC6、AC13）。
 */

let dom: DomEnv;
let shotsModule: typeof ShotsModule;
let Harness: (props: { shots: StoryboardShot[]; nodes?: CanvasNode[] }) => React.ReactElement;
/** 测试里读节点最新的镜头 */
let latest: () => StoryboardShot[];

beforeAll(async () => {
  dom = await installDom();
  const { ReactFlowProvider, useNodes, useStoreApi } = await import("@xyflow/react");
  const { StoryboardDialog } = await import("@/components/storyboard/storyboard-dialog");
  shotsModule = await import("@/utils/storyboard/shots");
  // Body 每次提交后把节点最新的镜头放进来，测试里读它
  const seen: { shots: StoryboardShot[] } = { shots: [] };
  latest = () => seen.shots;

  function Body() {
    // 只有 Provider 没有 <ReactFlow> 时，store 默认不收节点改动；按画布里非受控的方式打开
    const store = useStoreApi<CanvasNode>();
    if (!store.getState().hasDefaultNodes) store.setState({ hasDefaultNodes: true });
    const node = useNodes<CanvasNode>()[0];
    const board = node.data.storyboard ?? { shots: [] };
    useEffect(() => {
      seen.shots = board.shots;
    });
    return (
      <StoryboardDialog
        id={node.id}
        label={node.data.label}
        board={board}
        open
        focusShotId={null}
        onOpenChange={() => {}}
      />
    );
  }
  Harness = ({ shots, nodes = [] }) => (
    <ReactFlowProvider
      initialNodes={[
        {
          id: "sb1",
          type: "canvas",
          position: { x: 0, y: 0 },
          data: { kind: "storyboard", label: "分镜表", storyboard: { shots } },
        },
        ...nodes,
      ]}
    >
      <Body />
    </ReactFlowProvider>
  );
});
afterAll(() => dom.restore());

const makeShots = (n: number): StoryboardShot[] =>
  Array.from({ length: n }, (_, i) => ({
    id: `s${i + 1}`,
    scene: `画面${i + 1}`,
    shot: "中景",
    camera: "固定",
    action: "",
    dialogue: "",
    duration: 3,
  }));

const body = () => dom.win.document.body;
const all = (selector: string) => Array.from(body().querySelectorAll<HTMLElement>(selector));
const byLabel = (label: string) => body().querySelector<HTMLElement>(`[aria-label="${label}"]`);
const buttonByText = (text: string) =>
  all("button").find((b) => b.textContent?.trim() === text) ?? null;
/** 忽略空白和前后图标，按包含关系找按钮（按钮里有图标时 textContent 会粘在一起） */
const buttonIncl = (text: string) => {
  const want = text.replace(/\s+/g, "");
  return (
    all("button").find((b) => (b.textContent ?? "").replace(/\s+/g, "").includes(want)) ?? null
  );
};
const blur = (el: HTMLElement | null) =>
  dom.act(async () => {
    el?.dispatchEvent(new dom.win.FocusEvent("focusout", { bubbles: true }));
    el?.dispatchEvent(new dom.win.FocusEvent("blur"));
  });

/** 画布上的视频节点：y 决定导入顺序 */
const videoNode = (id: string, y: number): CanvasNode => ({
  id,
  type: "canvas",
  position: { x: 800, y },
  data: { kind: "video", label: id, src: `/f/${id}.mp4`, assetId: id },
});

/** 画布上的图片节点 */
const imageNode = (id: string, y: number): CanvasNode => ({
  id,
  type: "canvas",
  position: { x: 400, y },
  data: { kind: "image", label: id, src: `/f/${id}.png`, assetId: id },
});

describe("分镜表编辑弹窗", () => {
  test("50 镜：表头统计、镜号从 01 连续到 50", async () => {
    const m = await dom.mount(<Harness shots={makeShots(50)} />);
    await dom.flush();
    expect(body().textContent).toContain("50 镜 · 02:30");
    expect(all('[aria-label$="的画面描述"]')).toHaveLength(50);
    expect(byLabel("第 50 镜的画面描述")).not.toBeNull();
    await m.unmount();
  });

  test("50 镜时改一格只重渲染那一行，别的行跳过（AC13）", async () => {
    const m = await dom.mount(<Harness shots={makeShots(50)} />);
    await dom.flush();
    // 每渲染一行调一次 formatShotNumber，拿它数行的渲染次数
    const spy = spyOn(shotsModule, "formatShotNumber");
    const cell = byLabel("第 3 镜的画面描述");
    await dom.type(cell, "新的画面");
    await blur(cell);
    await dom.flush();
    expect(latest()[2].scene).toBe("新的画面");
    expect(latest()[0]).toEqual(makeShots(1)[0]);
    // 只有第 3 镜重渲染：恰好 1 次（为 0 说明计数没生效，大于 1 说明别的行没跳过）
    expect(spy.mock.calls.length).toBe(1);
    spy.mockRestore();
    await m.unmount();
  });

  test("打字时不写节点，失焦才写回", async () => {
    const m = await dom.mount(<Harness shots={makeShots(2)} />);
    await dom.flush();
    const cell = byLabel("第 1 镜的对白");
    await dom.type(cell, "你好");
    expect(latest()[0].dialogue).toBe("");
    await blur(cell);
    expect(latest()[0].dialogue).toBe("你好");
    await m.unmount();
  });

  test("时长越界失焦后修正到 60 并闪黄框；非数字恢复原值（AC5）", async () => {
    const m = await dom.mount(<Harness shots={makeShots(2)} />);
    await dom.flush();
    const input = all('input[aria-label="时长（秒）"]')[0] as HTMLInputElement;
    await dom.type(input, "75");
    await blur(input);
    expect(latest()[0].duration).toBe(60);
    expect(input.value).toBe("60");
    expect(input.parentElement?.className).toContain("ring-amber-500/60");
    await dom.flush(350);
    expect(input.parentElement?.className).not.toContain("ring-amber-500/60");

    await dom.type(input, "abc");
    await blur(input);
    expect(input.value).toBe("60");
    expect(latest()[0].duration).toBe(60);
    expect(body().textContent).toContain("2 镜 · 01:03");
    await m.unmount();
  });

  test("景别、运镜是下拉，不能自由输入（AC5）", async () => {
    const m = await dom.mount(<Harness shots={makeShots(1)} />);
    await dom.flush();
    for (const label of ["景别", "运镜"]) {
      const trigger = all(`[aria-label="${label}"]`)[0];
      expect(trigger.tagName).not.toBe("INPUT");
      expect(trigger.tagName).not.toBe("TEXTAREA");
    }
    expect(all('[aria-label="景别"]')[0].textContent).toContain("中景");
    expect(all('[aria-label="运镜"]')[0].textContent).toContain("固定");
    await m.unmount();
  });

  test("添加镜头：末尾加一行，镜号接着排、总时长跟着变（AC6）", async () => {
    const m = await dom.mount(<Harness shots={makeShots(2)} />);
    await dom.flush();
    await dom.click(buttonByText("添加镜头"));
    await dom.flush();
    expect(latest()).toHaveLength(3);
    expect(latest()[2]).toMatchObject({ shot: "中景", camera: "固定", duration: 3 });
    expect(byLabel("第 3 镜的画面描述")).not.toBeNull();
    expect(body().textContent).toContain("3 镜 · 00:09");
    await m.unmount();
  });

  test("行菜单删除要二次确认，删后镜号重排（AC6）", async () => {
    const m = await dom.mount(<Harness shots={makeShots(3)} />);
    await dom.flush();
    await dom.click(byLabel("第 2 镜的操作"));
    await dom.flush();
    await dom.click(buttonByText("删除镜头"));
    await dom.flush();
    expect(body().textContent).toContain("删除第 2 镜？");
    expect(latest()).toHaveLength(3);
    await dom.click(buttonByText("删除"));
    await dom.flush();
    expect(latest().map((s) => s.id)).toEqual(["s1", "s3"]);
    expect(byLabel("第 3 镜的画面描述")).toBeNull();
    expect((byLabel("第 2 镜的画面描述") as HTMLTextAreaElement).value).toBe("画面3");
    await m.unmount();
  });

  test("行菜单插入、复制", async () => {
    const m = await dom.mount(<Harness shots={makeShots(2)} />);
    await dom.flush();
    await dom.click(byLabel("第 1 镜的操作"));
    await dom.flush();
    await dom.click(buttonByText("复制镜头"));
    await dom.flush();
    expect(latest().map((s) => s.scene)).toEqual(["画面1", "画面1", "画面2"]);
    await dom.click(byLabel("第 3 镜的操作"));
    await dom.flush();
    await dom.click(buttonByText("在上方插入"));
    await dom.flush();
    expect(latest().map((s) => s.scene)).toEqual(["画面1", "画面1", "", "画面2"]);
    await m.unmount();
  });

  test("没有镜头时显示空状态，点添加镜头加第 1 行", async () => {
    const m = await dom.mount(<Harness shots={[]} />);
    await dom.flush();
    expect(body().textContent).toContain("还没有镜头");
    await dom.click(buttonByText("添加镜头"));
    await dom.flush();
    expect(latest()).toHaveLength(1);
    expect(byLabel("第 1 镜的画面描述")).not.toBeNull();
    await m.unmount();
  });
});

/**
 * 导入画布素材：分镜表是后来加的、或者素材不是从这张表生成的时，
 * 把画布上已有的视频 / 图片节点挂到镜头行上。
 */
describe("导入画布素材", () => {
  test("批量：按画布顺序挂到还没有视频的镜头", async () => {
    // 位置上 v1 在 v2 上面，所以 v1 先挂
    const m = await dom.mount(
      <Harness shots={makeShots(3)} nodes={[videoNode("v2", 600), videoNode("v1", 100)]} />,
    );
    await dom.flush();
    await dom.click(buttonByText("导入素材"));
    await dom.flush();
    expect(body().textContent).toContain("导入画布素材");
    expect(byLabel("选择 v1")).not.toBeNull();
    // 两个都没被用过，默认都勾上
    expect(buttonIncl("导入2段")).not.toBeNull();
    await dom.click(buttonIncl("导入2段"));
    await dom.flush();
    expect(latest().map((s) => s.videoNodeId)).toEqual(["v1", "v2", undefined]);
    await m.unmount();
  });

  test("单行：点一项只挂这一镜，别的行不动", async () => {
    const m = await dom.mount(<Harness shots={makeShots(2)} nodes={[videoNode("v1", 0)]} />);
    await dom.flush();
    await dom.click(byLabel("导入画布上已有的视频"));
    await dom.flush();
    expect(body().textContent).toContain("导入视频到第 01 镜");
    await dom.click(buttonIncl("v1"));
    await dom.flush();
    expect(latest().map((s) => s.videoNodeId)).toEqual(["v1", undefined]);
    await m.unmount();
  });

  test("空表：默认追加为镜头，画面描述先取节点标题", async () => {
    const m = await dom.mount(
      <Harness shots={[]} nodes={[videoNode("S1", 0), videoNode("S2", 400)]} />,
    );
    await dom.flush();
    await dom.click(buttonByText("导入素材"));
    await dom.flush();
    expect(body().textContent).toContain("多出的 2 段追加为镜头");
    await dom.click(buttonIncl("导入2段"));
    await dom.flush();
    expect(latest().map((s) => [s.scene, s.videoNodeId])).toEqual([
      ["S1", "S1"],
      ["S2", "S2"],
    ]);
    await m.unmount();
  });

  test("已经挂过的节点默认不勾，避免把现有安排覆盖一遍", async () => {
    const shots = makeShots(2).map((s, i) => (i === 0 ? { ...s, videoNodeId: "v1" } : s));
    const m = await dom.mount(
      <Harness shots={shots} nodes={[videoNode("v1", 0), videoNode("v2", 300)]} />,
    );
    await dom.flush();
    await dom.click(buttonByText("导入素材"));
    await dom.flush();
    expect(body().textContent).toContain("挂到第 02 镜");
    await dom.click(buttonIncl("导入1段"));
    await dom.flush();
    expect(latest().map((s) => s.videoNodeId)).toEqual(["v1", "v2"]);
    await m.unmount();
  });

  test("切到分镜图：只列图片节点，导入写 imageNodeId", async () => {
    const m = await dom.mount(
      <Harness shots={makeShots(1)} nodes={[videoNode("v1", 0), imageNode("i1", 200)]} />,
    );
    await dom.flush();
    await dom.click(buttonByText("导入素材"));
    await dom.flush();
    await dom.click(buttonByText("分镜图"));
    await dom.flush();
    expect(byLabel("选择 i1")).not.toBeNull();
    expect(byLabel("选择 v1")).toBeNull();
    await dom.click(buttonIncl("导入1张"));
    await dom.flush();
    expect(latest()[0].imageNodeId).toBe("i1");
    expect(latest()[0].videoNodeId).toBeUndefined();
    await m.unmount();
  });

  test("单行也能切分镜图：从视频格进去，改成挂这一镜的分镜图", async () => {
    const m = await dom.mount(
      <Harness shots={makeShots(2)} nodes={[videoNode("v1", 0), imageNode("i1", 200)]} />,
    );
    await dom.flush();
    await dom.click(byLabel("导入画布上已有的视频"));
    await dom.flush();
    expect(body().textContent).toContain("导入视频到第 01 镜");
    // 单行原来只有视频，现在两种都能选：切过去标题跟着变，挂的是 imageNodeId
    await dom.click(buttonByText("分镜图"));
    await dom.flush();
    expect(body().textContent).toContain("导入分镜图到第 01 镜");
    await dom.click(buttonIncl("i1"));
    await dom.flush();
    expect(latest()[0].imageNodeId).toBe("i1");
    expect(latest()[0].videoNodeId).toBeUndefined();
    expect(latest()[1].imageNodeId).toBeUndefined();
    await m.unmount();
  });

  test("分镜图格自己就能导入：不用绕行菜单", async () => {
    const m = await dom.mount(<Harness shots={makeShots(1)} nodes={[imageNode("i1", 0)]} />);
    await dom.flush();
    await dom.click(byLabel("用画布上已有的分镜图"));
    await dom.flush();
    expect(body().textContent).toContain("导入分镜图到第 01 镜");
    await dom.click(buttonIncl("i1"));
    await dom.flush();
    expect(latest()[0].imageNodeId).toBe("i1");
    await m.unmount();
  });
});

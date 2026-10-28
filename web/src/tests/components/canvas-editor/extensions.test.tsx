import { describe, expect, test } from "bun:test";
import { renderToStaticMarkup } from "react-dom/server";

import { NodeActionBar } from "@/components/canvas/node-action-bar";
import { TooltipProvider } from "@/components/ui/tooltip";
import {
  CanvasEditorApiProvider,
  useCanvasEditor,
  type CanvasEditorApi,
} from "@/components/canvas-editor/editor-api";
import {
  applyNodeDefaults,
  NodeExtensionsProvider,
  resolveToolbar,
  useNodeExtensions,
} from "@/components/canvas-editor/extensions";
import { selectOnly } from "@/components/canvas-editor/use-focus-node";
import { NODE_TOOLBAR } from "@/constants/node-toolbar";
import type { CanvasNode } from "@/types";

const node = (id: string, patch: Partial<CanvasNode["data"]> = {}): CanvasNode => ({
  id,
  type: "canvas",
  position: { x: 0, y: 0 },
  data: { kind: "image", label: id, ...patch },
});

describe("新建节点默认数据钩子 applyNodeDefaults", () => {
  const mark = (n: CanvasNode): CanvasNode => ({ ...n, data: { ...n.data, label: "新建" } });

  test("只改写之前不存在的节点，已有节点原样保留", () => {
    const previous = [node("a")];
    const next = [node("a"), node("b")];
    const result = applyNodeDefaults(previous, next, mark);
    expect(result[0]).toBe(next[0]);
    expect(result[1].data.label).toBe("新建");
  });

  test("没有新节点时返回原数组，引用不变", () => {
    const previous = [node("a")];
    const next = [{ ...previous[0], data: { ...previous[0].data, prompt: "x" } }];
    expect(applyNodeDefaults(previous, next, mark)).toBe(next);
  });

  test("没给钩子时原样返回", () => {
    const next = [node("a"), node("b")];
    expect(applyNodeDefaults([node("a")], next, undefined)).toBe(next);
  });

  test("删除节点不触发钩子", () => {
    const next = [node("a")];
    expect(applyNodeDefaults([node("a"), node("b")], next, mark)).toBe(next);
  });
});

describe("选中节点 selectOnly", () => {
  test("只留目标选中，别的取消；没变化的节点保持原引用", () => {
    const a = { id: "a", selected: true };
    const b = { id: "b" };
    const c = { id: "c", selected: false };
    const result = selectOnly([a, b, c], "b");
    expect(result[0]).toEqual({ id: "a", selected: false });
    expect(result[1]).toEqual({ id: "b", selected: true });
    expect(result[2]).toBe(c);
  });
});

describe("节点功能区追加按钮", () => {
  const gate = { visible: true, actionsDisabled: false };
  const render = (extra?: React.ReactNode) =>
    renderToStaticMarkup(
      <TooltipProvider>
        <NodeActionBar config={NODE_TOOLBAR.image} label="图" gate={gate} extra={extra} />
      </TooltipProvider>,
    );

  test("没给追加内容时功能区不变", () => {
    expect(render()).not.toContain("出现集数");
  });

  test("追加内容出现在功能区里", () => {
    const out = render(<button type="button">出现集数</button>);
    expect(out).toContain("出现集数");
    expect(out.indexOf("出现集数")).toBeGreaterThan(out.indexOf('role="toolbar"'));
  });

  test("节点读到页面传下来的 toolbarExtra", () => {
    function Probe() {
      const { toolbarExtra } = useNodeExtensions();
      return <>{toolbarExtra?.({ id: "n1", data: node("n1").data })}</>;
    }
    const out = renderToStaticMarkup(
      <NodeExtensionsProvider value={{ toolbarExtra: ({ id }) => <i>extra-{id}</i> }}>
        <Probe />
      </NodeExtensionsProvider>,
    );
    expect(out).toContain("extra-n1");
    expect(renderToStaticMarkup(<Probe />)).toBe("");
  });
});

describe("useCanvasEditor", () => {
  function Probe() {
    const editor = useCanvasEditor();
    return (
      <span>
        {editor.canvasId}:{editor.getNodes().length}:{String(editor.focusNode("x"))}
      </span>
    );
  }

  test("编辑器之外使用会明确报错", () => {
    expect(() => renderToStaticMarkup(<Probe />)).toThrow("useCanvasEditor");
  });

  test("插槽里能拿到状态和命令", () => {
    const api = {
      canvasId: "c1",
      getNodes: () => [node("a")],
      focusNode: () => false,
    } as unknown as CanvasEditorApi;
    const out = renderToStaticMarkup(
      <CanvasEditorApiProvider value={api}>
        <Probe />
      </CanvasEditorApiProvider>,
    );
    expect(out).toContain("c1:1:false");
  });
});

describe("功能区是否显示 resolveToolbar", () => {
  const emptyGate = { visible: false, actionsDisabled: true };
  const fullGate = { visible: true, actionsDisabled: false };
  const data = node("e").data;

  test("没给判定回调：沿用默认规则", () => {
    expect(resolveToolbar(emptyGate, data, undefined)).toBe("hidden");
    expect(resolveToolbar(fullGate, data, undefined)).toBe("full");
  });

  test("默认规则显示的节点不受回调影响", () => {
    expect(resolveToolbar(fullGate, data, () => false)).toBe("full");
  });

  test("默认规则隐藏（空节点）而回调放行：只显示追加内容", () => {
    expect(resolveToolbar(emptyGate, data, () => true)).toBe("extra-only");
    expect(resolveToolbar(emptyGate, data, () => false)).toBe("hidden");
  });
});

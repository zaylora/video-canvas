import { describe, expect, test } from "bun:test";

import type { CanvasNodeData, NodeOutput } from "@/types";
import { toolbarGate } from "@/utils/canvas/node-toolbar-state";

const output = (id: string): NodeOutput => ({
  id,
  src: `/files/${id}.mp4`,
  mediaType: "video",
  assetId: id,
  createdAt: 1,
});

const video = (over: Partial<CanvasNodeData> = {}): CanvasNodeData => ({
  kind: "video",
  label: "镜头7",
  ...over,
});

describe("功能区按钮的可用性", () => {
  test("已有成片：加工类和放大、下载可用，历史带出版本数", () => {
    const gate = toolbarGate(
      video({
        src: "/files/a.mp4",
        mediaType: "video",
        assetId: "a",
        status: "done",
        outputs: [output("a"), output("b")],
      }),
    );
    expect(gate).toEqual({
      visible: true,
      actionsDisabled: false,
      historyDisabled: false,
      historyCount: 2,
    });
  });

  test("空节点（还没素材也没有历史）：不显示功能区", () => {
    const gate = toolbarGate(video());
    expect(gate).toEqual({
      visible: false,
      actionsDisabled: true,
      historyDisabled: true,
      historyCount: 0,
    });
  });

  test("第一次生成中、还没出过结果：同样不显示", () => {
    expect(toolbarGate(video({ status: "running" })).visible).toBe(false);
  });

  test("生成中：加工类禁用，但已有的历史版本仍可展开查看", () => {
    const gate = toolbarGate(
      video({ src: "/files/a.mp4", assetId: "a", status: "running", outputs: [output("a")] }),
    );
    expect(gate.visible).toBe(true);
    expect(gate.actionsDisabled).toBe(true);
    expect(gate.historyDisabled).toBe(false);
  });

  test("素材还是本地 blob（没传完）：不算有素材", () => {
    const gate = toolbarGate(video({ src: "blob:http://localhost/x", mediaType: "video" }));
    expect(gate.actionsDisabled).toBe(true);
    expect(gate.visible).toBe(false);
  });

  test("文本节点：有正文才算有素材，且没有历史", () => {
    const empty = toolbarGate({ kind: "script", label: "文本 1" });
    expect(empty.actionsDisabled).toBe(true);
    expect(empty.visible).toBe(false);
    const filled = toolbarGate({ kind: "script", label: "文本 1", text: "一段脚本" });
    expect(filled.visible).toBe(true);
    expect(filled.actionsDisabled).toBe(false);
    expect(filled.historyDisabled).toBe(true);
  });
});

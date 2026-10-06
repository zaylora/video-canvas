import { describe, expect, test } from "bun:test";

import type { NodeKind } from "@/types";
import { canConnectKinds, getModelOptions, pruneRemoteDefaults } from "@/utils/canvas/canvas";

const options = [{ id: "gpt-x", label: "GPT X", credits: 1 }];

describe("pruneRemoteDefaults：远程种类默认模型核对", () => {
  test("清单加载成功且含该模型：保留", () => {
    const defaults = { script: "gpt-x", image: "lib-image-2.5" };
    const result = pruneRemoteDefaults(defaults, { script: { status: "ready", options } });
    expect(result).toBe(defaults);
  });

  test("旧演示 id（清单里找不到）：去掉，其他种类不动", () => {
    const result = pruneRemoteDefaults(
      { script: "gvlm-3.1", image: "lib-image-2.5" },
      { script: { status: "ready", options } },
    );
    expect(result).toEqual({ image: "lib-image-2.5" });
  });

  test("清单加载中 / 失败：也不往新节点上写", () => {
    for (const status of ["idle", "loading", "error"] as const) {
      expect(pruneRemoteDefaults({ script: "gpt-x" }, { script: { status, options } })).toEqual({});
    }
  });

  test("没存默认值时原样返回，不复制", () => {
    const defaults = { image: "a" };
    expect(pruneRemoteDefaults(defaults, { script: { status: "ready", options } })).toBe(defaults);
  });
});

describe("getModelOptions：远程种类只认服务端清单", () => {
  test("文本节点不再混本地演示与自定义模型", () => {
    const custom = [
      {
        id: "c1",
        kind: "script",
        label: "自定义",
        credits: 1,
        endpoint: "",
        modelId: "m",
        apiKey: "",
      },
    ];
    expect(getModelOptions("script", custom as never, options)).toEqual(options);
    expect(getModelOptions("script", custom as never)).toEqual([]);
  });
});

describe("canConnectKinds：对方没有种类（组节点）时不报错", () => {
  // 组节点的 data 里没有 kind，拉线松手时会被拿来和每个节点比对
  const noKind = undefined as unknown as NodeKind;

  test("从输入口（target 端）拉线，压到没有种类的节点上：接不上，不抛异常", () => {
    expect(canConnectKinds("video", "target", noKind)).toBe(false);
  });

  test("从输出口（source 端）拉线同理", () => {
    expect(canConnectKinds("video", "source", noKind)).toBe(false);
  });
});

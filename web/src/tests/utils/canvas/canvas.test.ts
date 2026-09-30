import { describe, expect, test } from "bun:test";

import { getModelOptions, pruneRemoteDefaults } from "@/utils/canvas/canvas";

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
    const custom = [{ id: "c1", kind: "script", label: "自定义", credits: 1, endpoint: "", modelId: "m", apiKey: "" }];
    expect(getModelOptions("script", custom as never, options)).toEqual(options);
    expect(getModelOptions("script", custom as never)).toEqual([]);
  });
});

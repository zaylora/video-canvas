import { describe, expect, test } from "bun:test";

import {
  isRemoteModelKind,
  MODEL_LIBRARY,
  REMOTE_MODEL_KINDS,
  remoteKindOf,
} from "@/constants/canvas/model-library";

describe("节点种类与后端 kind 的映射", () => {
  test("文本节点（script）对应后端 text，其余种类同名", () => {
    expect(remoteKindOf("script")).toBe("text");
    expect(remoteKindOf("image")).toBe("image");
    expect(remoteKindOf("video")).toBe("video");
    expect(remoteKindOf("audio")).toBe("audio");
  });

  test("不是节点种类的名字没有后端 kind", () => {
    expect(remoteKindOf("toString")).toBeUndefined();
  });

  test("四种节点的清单都由服务端下发，本地不再放演示模型", () => {
    expect([...REMOTE_MODEL_KINDS].sort()).toEqual(["audio", "image", "script", "video"]);
    expect(isRemoteModelKind("image")).toBe(true);
    for (const kind of REMOTE_MODEL_KINDS) expect(MODEL_LIBRARY[kind]).toHaveLength(0);
  });
});

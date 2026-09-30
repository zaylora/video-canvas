import { describe, expect, test } from "bun:test";

import {
  isRemoteModelKind,
  MODEL_LIBRARY,
  REMOTE_MODEL_KINDS,
  remoteKindOf,
} from "@/constants/canvas/model-library";

describe("节点种类与后端 kind 的映射", () => {
  test("文本节点（script）对应后端 text，视频仍是 video", () => {
    expect(remoteKindOf("script")).toBe("text");
    expect(remoteKindOf("video")).toBe("video");
  });

  test("不走服务端清单的种类没有后端 kind", () => {
    expect(remoteKindOf("image")).toBeUndefined();
    expect(remoteKindOf("audio")).toBeUndefined();
    expect(remoteKindOf("toString")).toBeUndefined();
  });

  test("远程种类清单含 script 与 video，且本地不再放演示模型", () => {
    expect([...REMOTE_MODEL_KINDS].sort()).toEqual(["script", "video"]);
    expect(isRemoteModelKind("script")).toBe(true);
    expect(isRemoteModelKind("image")).toBe(false);
    expect(MODEL_LIBRARY.script).toHaveLength(0);
  });
});

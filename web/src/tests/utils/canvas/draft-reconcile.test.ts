import { describe, expect, test } from "bun:test";

import type { CanvasGraphDto } from "@/api/canvas/type";
import type { Draft } from "@/utils/canvas/draft-store";
import { canonicalJson, reconcileDraft, sameContent } from "@/utils/canvas/draft-reconcile";

const graph = (ids: string[], viewport = { x: 0, y: 0, zoom: 1 }) =>
  ({ nodes: ids.map((id) => ({ id })), edges: [], viewport }) as unknown as CanvasGraphDto;

const draftOf = (over: Partial<Draft> = {}): Draft => ({
  schemaVersion: 1,
  graph: graph(["a", "b"]),
  baseVersion: 5,
  cloudDirty: true,
  savedAt: 1,
  ...over,
});

describe("canonicalJson / sameContent", () => {
  test("键顺序不同也算相同：服务端 JSONB 会重排键", () => {
    expect(canonicalJson({ a: 1, b: { c: 2, d: 3 } })).toBe(
      canonicalJson({ b: { d: 3, c: 2 }, a: 1 }),
    );
  });

  test("数组顺序不同算不同", () => {
    expect(canonicalJson([1, 2])).not.toBe(canonicalJson([2, 1]));
  });

  test("内容比较忽略视口", () => {
    expect(
      sameContent(graph(["a"], { x: 1, y: 1, zoom: 1 }), graph(["a"], { x: 9, y: 9, zoom: 2 })),
    ).toBe(true);
    expect(sameContent(graph(["a"]), graph(["a", "b"]))).toBe(false);
  });
});

describe("reconcileDraft", () => {
  const cloud = { cloudVersion: 5, cloudGraph: graph(["a"]) };

  test("没有草稿：什么都不做", () => {
    expect(reconcileDraft({ draft: null, ...cloud })).toEqual({ kind: "none" });
  });

  test("草稿已同步：丢弃，用云端版本", () => {
    expect(reconcileDraft({ draft: draftOf({ cloudDirty: false }), ...cloud })).toEqual({
      kind: "discard",
    });
  });

  test("未同步且基准版本和云端一致：自动恢复", () => {
    const draft = draftOf();
    expect(reconcileDraft({ draft, ...cloud })).toEqual({ kind: "restore", graph: draft.graph });
  });

  test("未同步但云端已被改过（版本对不上）：冲突，带上草稿内容", () => {
    const draft = draftOf({ baseVersion: 4 });
    expect(reconcileDraft({ draft, ...cloud })).toEqual({ kind: "conflict", graph: draft.graph });
  });

  test("草稿版本比云端还新（云端被回滚等）：同样按冲突处理，不静默覆盖", () => {
    const draft = draftOf({ baseVersion: 9 });
    expect(reconcileDraft({ draft, ...cloud }).kind).toBe("conflict");
  });

  test("版本对不上但内容和云端一样（自己的上传成功了、草稿还没来得及删）：丢弃，不误报冲突", () => {
    const draft = draftOf({ baseVersion: 4, graph: graph(["a"], { x: 7, y: 7, zoom: 2 }) });
    expect(reconcileDraft({ draft, ...cloud })).toEqual({ kind: "discard" });
  });

  test("版本一致但内容和云端一样：丢弃，不必恢复", () => {
    const draft = draftOf({ graph: graph(["a"]) });
    expect(reconcileDraft({ draft, ...cloud })).toEqual({ kind: "discard" });
  });
});

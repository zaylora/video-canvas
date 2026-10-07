import { describe, expect, test } from "bun:test";

import type { CanvasPatchDto } from "@/api/agent/type";
import { publishCanvasPatch, subscribeCanvasPatch } from "@/utils/agent/patch-bus";

const patch = (canvasId: string): CanvasPatchDto => ({
  mutation_id: "m",
  run_id: "r",
  canvas_id: canvasId,
  kind: "apply_ops",
  revision_before: 1,
  revision_after: 2,
  changes: [],
});

describe("patch-bus：按画布分发 Agent 的改动", () => {
  test("只发给订阅了这张画布的人；取消订阅后不再收到", () => {
    const got: string[] = [];
    const off = subscribeCanvasPatch("c1", (p) => got.push("c1:" + p.canvas_id));
    const other = subscribeCanvasPatch("c2", (p) => got.push("c2:" + p.canvas_id));
    publishCanvasPatch(patch("c1"));
    publishCanvasPatch(patch("c3"));
    expect(got).toEqual(["c1:c1"]);
    off();
    publishCanvasPatch(patch("c1"));
    expect(got).toEqual(["c1:c1"]);
    other();
  });

  test("没人订阅时丢弃，不抛错", () => {
    expect(() => publishCanvasPatch(patch("nobody"))).not.toThrow();
  });
});

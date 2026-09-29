import { describe, expect, test } from "bun:test";

import type { CanvasNodeData } from "@/types";

import { applyBackfill, planBackfill } from "@/utils/tasks/backfill";
import { makeData, makeTask, succeeded } from "./fixtures";

const node = (id: string, data: Partial<CanvasNodeData>) => ({ id, data: makeData(data) });

describe("planBackfill：终态回填节点", () => {
  test("succeeded：done + 产物地址 + assetId（字符串）+ 视频", () => {
    const patch = planBackfill(makeData({ status: "running", taskId: "1" }), succeeded());
    expect(patch).toMatchObject({
      status: "done",
      src: "/files/a.mp4",
      assetId: "77",
      mediaType: "video",
      error: null,
    });
  });

  test("幂等：节点 assetId 已等于结果就跳过", () => {
    const data = makeData({ status: "done", taskId: "1", assetId: "77", src: "/files/a.mp4" });
    expect(planBackfill(data, succeeded())).toBeNull();
  });

  test("taskId 与任务不一致不回填（重新生成后旧任务的推送不能覆盖新任务）", () => {
    const data = makeData({ status: "running", taskId: "2" });
    expect(planBackfill(data, succeeded({ id: 1 }))).toBeNull();
    expect(planBackfill(makeData({ status: "running" }), succeeded())).toBeNull();
  });

  test("非终态不回填", () => {
    const data = makeData({ status: "running", taskId: "1" });
    for (const status of ["pending", "queued", "running", "finalizing"] as const) {
      expect(planBackfill(data, makeTask({ status }))).toBeNull();
    }
  });

  test("failed：error + 后端文案；没有文案时给默认；重复应用幂等", () => {
    const data = makeData({ status: "running", taskId: "1" });
    const failed = makeTask({ status: "failed", error_message: "内容未通过审核" });
    const patch = planBackfill(data, failed);
    expect(patch).toMatchObject({ status: "error", error: "内容未通过审核" });
    expect(planBackfill({ ...data, ...patch }, failed)).toBeNull();
    expect(planBackfill(data, makeTask({ status: "failed", error_message: "" }))).toMatchObject({
      error: "生成失败",
    });
  });

  test("expired：给超时文案", () => {
    const patch = planBackfill(
      makeData({ status: "running", taskId: "1" }),
      makeTask({ status: "expired", error_message: null }),
    );
    expect(patch).toMatchObject({ status: "error", error: "生成超时" });
  });

  test("canceled：回到 idle 并清掉 taskId", () => {
    const patch = planBackfill(
      makeData({ status: "running", taskId: "1" }),
      makeTask({ status: "canceled" }),
    );
    expect(patch).toMatchObject({ status: "idle", taskId: undefined, error: null });
  });

  test("成功但没有产物：按失败处理，别留一个空的 done", () => {
    const patch = planBackfill(
      makeData({ status: "running", taskId: "1" }),
      succeeded({ outputs: [] }),
    );
    expect(patch).toMatchObject({ status: "error" });
  });
});

describe("applyBackfill：批量回填", () => {
  test("只改 taskId 匹配的节点，其余节点保持同一引用", () => {
    const a = node("a", { status: "running", taskId: "1" });
    const b = node("b", { status: "running", taskId: "2" });
    const c = node("c", { kind: "image" });
    const { nodes, events } = applyBackfill([a, b, c], { "1": succeeded() });
    expect(nodes[0].data.status).toBe("done");
    expect(nodes[1]).toBe(b);
    expect(nodes[2]).toBe(c);
    expect(events.map((event) => event.nodeId)).toEqual(["a"]);
  });

  test("没有变化时原样返回同一个数组，不触发多余的保存", () => {
    const nodes = [node("a", { status: "running", taskId: "1" })];
    const result = applyBackfill(nodes, { "1": makeTask({ status: "running" }) });
    expect(result.nodes).toBe(nodes);
    expect(result.events).toEqual([]);
  });

  test("已回填过的结果再应用一次是空操作", () => {
    const first = applyBackfill([node("a", { status: "running", taskId: "1" })], { "1": succeeded() });
    const second = applyBackfill(first.nodes, { "1": succeeded() });
    expect(second.nodes).toBe(first.nodes);
  });

  test("节点已被删除（任务还在 store 里）不报错", () => {
    expect(() => applyBackfill([], { "1": succeeded() })).not.toThrow();
    expect(applyBackfill([], { "1": succeeded() }).events).toEqual([]);
  });
});

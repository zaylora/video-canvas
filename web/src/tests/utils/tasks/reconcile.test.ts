import { describe, expect, test } from "bun:test";

import { collectRunningTaskIds, findStaleActiveIds, markTasksGone } from "@/utils/tasks/reconcile";
import { makeData, makeTask, succeeded } from "./fixtures";

describe("findStaleActiveIds：断线期间错过终态的任务", () => {
  test("本地还在进行、服务端进行中列表里没有的，需要补查", () => {
    const local = {
      "1": makeTask({ id: 1, status: "running" }),
      "2": makeTask({ id: 2, status: "queued" }),
      "3": succeeded({ id: 3 }),
    };
    const active = [makeTask({ id: 2, status: "running", version: 9 })];
    expect(findStaleActiveIds(local, active)).toEqual(["1"]);
  });

  test("已经是终态的、以及服务端仍在进行的都不补查", () => {
    const local = { "1": succeeded({ id: 1 }), "2": makeTask({ id: 2 }) };
    expect(findStaleActiveIds(local, [makeTask({ id: 2 })])).toEqual([]);
  });
});

describe("collectRunningTaskIds：打开画布时要对账的任务", () => {
  test("只收 running 且有 taskId 的节点，去重", () => {
    const nodes = [
      { data: makeData({ status: "running", taskId: "1" }) },
      { data: makeData({ status: "running", taskId: "1" }) },
      { data: makeData({ status: "running", taskId: "2" }) },
      { data: makeData({ status: "running" }) },
      { data: makeData({ status: "done", taskId: "3" }) },
      { data: makeData({ status: "error", taskId: "4" }) },
    ];
    expect(collectRunningTaskIds(nodes)).toEqual(["1", "2"]);
  });

  test("本地已经有终态快照的直接由回填处理，不再发请求", () => {
    const nodes = [
      { data: makeData({ status: "running", taskId: "1" }) },
      { data: makeData({ status: "running", taskId: "2" }) },
    ];
    expect(collectRunningTaskIds(nodes, { "1": succeeded({ id: 1 }) })).toEqual(["2"]);
  });
});

describe("collectRunningTaskIds：批次里的任务也要对账", () => {
  test("已经出了第一张的节点（done）仍要对账它还没结清的任务", () => {
    const nodes = [
      { data: makeData({ status: "done", taskId: "1", pendingTaskIds: ["2", "3"] }) },
      { data: makeData({ status: "running", taskId: "4", pendingTaskIds: ["4", "5"] }) },
    ];
    expect(collectRunningTaskIds(nodes)).toEqual(["2", "3", "4", "5"]);
  });

  test("本地已经有终态快照的不再发请求", () => {
    const nodes = [{ data: makeData({ status: "done", taskId: "1", pendingTaskIds: ["2", "3"] }) }];
    expect(collectRunningTaskIds(nodes, { "2": succeeded({ id: 2 }) })).toEqual(["3"]);
  });
});

describe("markTasksGone：服务端确认没有这些任务", () => {
  const node = (over = {}) => makeData({ kind: "image", status: "running", taskId: "1", ...over });

  test("单任务：节点标成失败，不再转圈（与原行为一致）", () => {
    expect(markTasksGone(node(), new Set(["1"]))).toMatchObject({
      status: "error",
      taskId: undefined,
      error: "任务状态已失效，请重新生成",
    });
  });

  test("批次里只有部分任务没了：只移出集合；taskId 指向还活着的第一个", () => {
    const patch = markTasksGone(node({ pendingTaskIds: ["1", "2", "3"] }), new Set(["1", "2"]));
    expect(patch).toEqual({ taskId: "3", pendingTaskIds: ["3"] });
  });

  test("批次里全没了且一张都没出：节点失败；出过结果的节点保持 done", () => {
    expect(markTasksGone(node({ pendingTaskIds: ["1", "2"] }), new Set(["1", "2"]))).toMatchObject({
      status: "error",
    });
    const done = node({ status: "done", pendingTaskIds: ["2"] });
    const patch = markTasksGone(done, new Set(["2"]));
    expect(patch).toEqual({ pendingTaskIds: undefined });
  });

  test("没有受影响的任务返回 null", () => {
    expect(markTasksGone(node({ pendingTaskIds: ["1", "2"] }), new Set(["9"]))).toBeNull();
    expect(markTasksGone(makeData({ status: "done" }), new Set(["1"]))).toBeNull();
  });
});

import { describe, expect, test } from "bun:test";

import { collectRunningTaskIds, findStaleActiveIds } from "@/utils/tasks/reconcile";
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

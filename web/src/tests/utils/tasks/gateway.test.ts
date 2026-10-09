import { beforeEach, describe, expect, mock, test } from "bun:test";

import type { CreateTaskRequest, TaskView } from "@/api/generation-task/type";
import { makeTask } from "./fixtures";

/** 记录统一入口调用了哪些接口，以及传了什么幂等键 */
const calls = {
  create: [] as Array<{ data: CreateTaskRequest; key: string }>,
  record: [] as Array<{ target: string; key: string }>,
  cancel: [] as Array<string | number>,
  credits: 0,
};
let createImpl: (data: CreateTaskRequest, key: string) => Promise<unknown>;
let cancelImpl: (id: string | number) => Promise<unknown>;

mock.module("@/api/generation-task", () => ({
  createGenerationTask: (data: CreateTaskRequest, key: string) => {
    calls.create.push({ data, key });
    return createImpl(data, key);
  },
  cancelGenerationTask: (id: string | number) => {
    calls.cancel.push(id);
    return cancelImpl(id);
  },
  getActiveGenerationTasks: async () => [],
  getGenerationTasksByIds: async () => [],
}));
mock.module("@/api/conversation", () => ({
  submitConversationRecord: async (target: string, _data: unknown, key: string) => {
    calls.record.push({ target, key });
    return {
      conversationId: "c1",
      record: {
        id: "r1",
        conversationId: "c1",
        kind: "image",
        modelId: "m",
        prompt: "p",
        input: {},
        count: 2,
        tasks: [makeTask({ id: 11, node_id: "rec:1:0" }), null],
        submitErrors: [{ index: 1, status: 402, code: 40001, message: "积分不足" }],
        quoteCredits: 3,
        createdAt: "2026-10-09T08:00:00Z",
      },
    };
  },
}));
mock.module("@/api/credit", () => ({
  getCredits: async () => {
    calls.credits += 1;
    return { balance: 10, frozen: 0, available: 10 };
  },
}));

const { acceptTasks, cancelTasks, submitCanvasTasks, submitRecordTasks } =
  await import("@/utils/tasks/gateway");
const { useTasksStore } = await import("@/store/tasks");
const { useCreditsStore } = await import("@/store/credits");

const settle = () => new Promise((resolve) => setTimeout(resolve, 0));

beforeEach(() => {
  calls.create.length = 0;
  calls.record.length = 0;
  calls.cancel.length = 0;
  calls.credits = 0;
  useTasksStore.setState({ tasks: {} });
  useCreditsStore.setState({ credits: null });
  createImpl = async () => ({ items: [] });
  cancelImpl = async () => null;
});

const request = (over: Partial<CreateTaskRequest> = {}): CreateTaskRequest => ({
  kind: "image",
  model_id: "m",
  canvas_id: "c",
  node_id: "n1",
  node_ids: ["n1", "n2"],
  input: { prompt: "p" },
  ...over,
});

describe("acceptTasks：任务快照入库并刷新余额", () => {
  test("快照进任务库，null / undefined 跳过；余额总会刷新", async () => {
    acceptTasks([makeTask({ id: 1 }), null, undefined]);
    await settle();
    expect(Object.keys(useTasksStore.getState().tasks)).toEqual(["1"]);
    expect(calls.credits).toBe(1);
  });

  test("一批里一个任务都没有也刷新余额（冻结的积分可能已经变了）", async () => {
    acceptTasks([null]);
    await settle();
    expect(calls.credits).toBe(1);
  });
});

describe("submitCanvasTasks：画布节点的提交", () => {
  test("每次提交一个新的幂等键；成功的任务入库，错误项原样返回", async () => {
    const task1 = makeTask({ id: 21, node_id: "n1" });
    createImpl = async () => ({
      items: [
        { node_id: "n1", task: task1 },
        { node_id: "n2", error: { status: 402, code: 40001, message: "积分不足" } },
      ],
    });

    const items = await submitCanvasTasks(request());
    await submitCanvasTasks(request());
    expect(items).toHaveLength(2);
    expect(items[1].error?.code).toBe(40001);
    expect(useTasksStore.getState().tasks["21"]).toBeDefined();
    expect(calls.create[0].key).not.toBe(calls.create[1].key);
    expect(calls.create[0].key).toHaveLength(36);
  });

  test("网络类失败沿用同一个幂等键重试，4xx 不重试", async () => {
    let attempts = 0;
    createImpl = async () => {
      attempts += 1;
      if (attempts < 2) throw { code: "NETWORK_ERROR", status: 0 };
      return { items: [] };
    };
    await submitCanvasTasks(request());
    expect(calls.create.map((call) => call.key)).toHaveLength(2);
    expect(calls.create[0].key).toBe(calls.create[1].key);

    calls.create.length = 0;
    createImpl = async () => {
      throw { code: 40003, status: 400 };
    };
    await expect(submitCanvasTasks(request())).rejects.toMatchObject({ code: 40003 });
    expect(calls.create).toHaveLength(1);
  });
});

describe("submitRecordTasks：首页对话的提交", () => {
  test("记录里已创建的任务入库，提交失败的格子不影响其他；同样刷新余额", async () => {
    const result = await submitRecordTasks("new", {
      kind: "image",
      modelId: "m",
      prompt: "p",
      input: {},
      count: 2,
    });
    await settle();
    expect(result.record.submitErrors).toHaveLength(1);
    expect(Object.keys(useTasksStore.getState().tasks)).toEqual(["11"]);
    expect(calls.record[0].target).toBe("new");
    expect(calls.record[0].key).toHaveLength(36);
    expect(calls.credits).toBe(1);
  });
});

describe("cancelTasks：取消", () => {
  test("逐个取消，返回的最新快照入库；没有返回快照的等推送", async () => {
    const canceled: TaskView = makeTask({ id: 1, status: "canceled", version: 9 });
    cancelImpl = async (id) => (String(id) === "1" ? canceled : null);
    const failed = await cancelTasks([1, 2]);
    expect(failed).toEqual([]);
    expect(calls.cancel).toEqual([1, 2]);
    expect(useTasksStore.getState().tasks["1"].status).toBe("canceled");
    expect(useTasksStore.getState().tasks["2"]).toBeUndefined();
  });

  test("某个失败不影响其他，返回失败的 id；仍然刷新余额", async () => {
    cancelImpl = async (id) => {
      if (String(id) === "2") throw new Error("已结束");
      return makeTask({ id: 1, status: "canceled", version: 9 });
    };
    const failed = await cancelTasks([1, 2, 3]);
    await settle();
    expect(failed).toEqual(["2"]);
    expect(calls.cancel).toEqual([1, 2, 3]);
    expect(useTasksStore.getState().tasks["1"].status).toBe("canceled");
    expect(calls.credits).toBeGreaterThan(0);
  });
});

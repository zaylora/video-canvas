import { describe, expect, test } from "bun:test";

import { createInternalsBatcher } from "@/utils/canvas/internals-batcher";

/** 等微任务队列走完一轮 */
const nextMicrotask = () => new Promise<void>((resolve) => queueMicrotask(resolve));

describe("createInternalsBatcher", () => {
  test("同一轮里 200 个节点的请求只发出一次，带上全部 id", async () => {
    const calls: string[][] = [];
    const request = createInternalsBatcher((ids) => calls.push(ids));

    const ids = Array.from({ length: 200 }, (_, index) => `n${index}`);
    for (const id of ids) request(id);
    await nextMicrotask();

    expect(calls).toHaveLength(1);
    expect(calls[0]).toEqual(ids);
  });

  test("调用之后、微任务之前不会提前发出", () => {
    const calls: string[][] = [];
    const request = createInternalsBatcher((ids) => calls.push(ids));

    request("a");

    expect(calls).toHaveLength(0);
  });

  test("同一轮里重复请求同一个节点只算一次", async () => {
    const calls: string[][] = [];
    const request = createInternalsBatcher((ids) => calls.push(ids));

    request("a");
    request("b");
    request("a");
    await nextMicrotask();

    expect(calls).toEqual([["a", "b"]]);
  });

  test("发出之后再请求会开一批新的，不混进上一批", async () => {
    const calls: string[][] = [];
    const request = createInternalsBatcher((ids) => calls.push(ids));

    request("a");
    await nextMicrotask();
    request("b");
    await nextMicrotask();

    expect(calls).toEqual([["a"], ["b"]]);
  });
});

import { describe, expect, test } from "bun:test";

import { createPagedList, type PagedPage } from "@/utils/assets/paged-list";

type Row = { id: string };

/** 可手动放行的假请求：每次调用记下游标，返回的 promise 由测试决定何时成功或失败 */
function fakeFetch() {
  const calls: Array<{
    cursor: string | undefined;
    resolve: (page: PagedPage<Row>) => void;
    reject: (error: Error) => void;
  }> = [];
  const fetchPage = (cursor: string | undefined) =>
    new Promise<PagedPage<Row>>((resolve, reject) => {
      calls.push({ cursor, resolve, reject });
    });
  return { calls, fetchPage };
}

const rows = (...ids: string[]): Row[] => ids.map((id) => ({ id }));
const tick = () => new Promise((resolve) => setTimeout(resolve, 0));

describe("createPagedList：游标分页", () => {
  test("初始是加载中，首页到达后进入 ready 并带出 next_cursor", async () => {
    const { calls, fetchPage } = fakeFetch();
    const list = createPagedList(fetchPage);
    expect(list.getState().status).toBe("loading");
    list.load();
    expect(calls[0]?.cursor).toBeUndefined();
    calls[0]!.resolve({ items: rows("a", "b"), nextCursor: "c1" });
    await tick();
    expect(list.getState()).toMatchObject({ status: "ready", nextCursor: "c1" });
    expect(list.getState().items).toEqual(rows("a", "b"));
  });

  test("loadMore 带着游标请求下一页并追加；游标为空串后不再请求", async () => {
    const { calls, fetchPage } = fakeFetch();
    const list = createPagedList(fetchPage);
    list.load();
    calls[0]!.resolve({ items: rows("a"), nextCursor: "c1" });
    await tick();

    list.loadMore();
    expect(list.getState().loadingMore).toBe(true);
    expect(calls[1]?.cursor).toBe("c1");
    calls[1]!.resolve({ items: rows("b"), nextCursor: "" });
    await tick();
    expect(list.getState().items).toEqual(rows("a", "b"));
    expect(list.getState().loadingMore).toBe(false);

    list.loadMore();
    expect(calls.length).toBe(2);
  });

  test("加载下一页期间重复触发不会重复请求", async () => {
    const { calls, fetchPage } = fakeFetch();
    const list = createPagedList(fetchPage);
    list.load();
    calls[0]!.resolve({ items: rows("a"), nextCursor: "c1" });
    await tick();
    list.loadMore();
    list.loadMore();
    list.loadMore();
    expect(calls.length).toBe(2);
  });

  test("首页还没回来时 loadMore 不请求", () => {
    const { calls, fetchPage } = fakeFetch();
    const list = createPagedList(fetchPage);
    list.load();
    list.loadMore();
    expect(calls.length).toBe(1);
  });

  test("首页失败进入 error，retry 重新从头请求", async () => {
    const { calls, fetchPage } = fakeFetch();
    const list = createPagedList(fetchPage);
    list.load();
    calls[0]!.reject(new Error("boom"));
    await tick();
    expect(list.getState().status).toBe("error");

    list.retry();
    expect(list.getState().status).toBe("loading");
    expect(calls[1]?.cursor).toBeUndefined();
    calls[1]!.resolve({ items: rows("a"), nextCursor: "" });
    await tick();
    expect(list.getState()).toMatchObject({ status: "ready" });
  });

  test("下一页失败保留已有内容并标记 moreFailed，再触发 loadMore 重试同一页", async () => {
    const { calls, fetchPage } = fakeFetch();
    const list = createPagedList(fetchPage);
    list.load();
    calls[0]!.resolve({ items: rows("a"), nextCursor: "c1" });
    await tick();
    list.loadMore();
    calls[1]!.reject(new Error("net"));
    await tick();
    expect(list.getState()).toMatchObject({
      status: "ready",
      loadingMore: false,
      moreFailed: true,
    });
    expect(list.getState().items).toEqual(rows("a"));

    list.loadMore();
    expect(list.getState().moreFailed).toBe(false);
    expect(calls[2]?.cursor).toBe("c1");
  });

  test("重新 load 之后，旧请求晚到的结果被丢弃", async () => {
    const { calls, fetchPage } = fakeFetch();
    const list = createPagedList(fetchPage);
    list.load();
    list.load();
    calls[1]!.resolve({ items: rows("new"), nextCursor: "" });
    await tick();
    calls[0]!.resolve({ items: rows("old"), nextCursor: "" });
    await tick();
    expect(list.getState().items).toEqual(rows("new"));
  });

  test("cancel 之后到达的结果被丢弃", async () => {
    const { calls, fetchPage } = fakeFetch();
    const list = createPagedList(fetchPage);
    list.load();
    list.cancel();
    calls[0]!.resolve({ items: rows("a"), nextCursor: "" });
    await tick();
    expect(list.getState().status).toBe("loading");
  });

  test("订阅者在状态变化时被通知，取消订阅后不再通知", async () => {
    const { calls, fetchPage } = fakeFetch();
    const list = createPagedList(fetchPage);
    let count = 0;
    const off = list.subscribe(() => count++);
    list.load();
    calls[0]!.resolve({ items: rows("a"), nextCursor: "" });
    await tick();
    expect(count).toBeGreaterThan(0);
    const before = count;
    off();
    list.update((items) => items.slice(1));
    expect(count).toBe(before);
  });

  test("update 改列表内容并保持其它状态", async () => {
    const { calls, fetchPage } = fakeFetch();
    const list = createPagedList(fetchPage);
    list.load();
    calls[0]!.resolve({ items: rows("a", "b"), nextCursor: "c1" });
    await tick();
    list.update((items) => items.filter((row) => row.id !== "a"));
    expect(list.getState().items).toEqual(rows("b"));
    expect(list.getState().nextCursor).toBe("c1");
  });
});

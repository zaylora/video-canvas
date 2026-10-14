import { describe, expect, test } from "bun:test";

import type { CanvasNodeData, NodeOutput } from "@/types";
import { applyBackfill, failedBatchMembers, planBackfill } from "@/utils/tasks/backfill";
import { makeData, makeTask } from "./fixtures";

/** 一个图片任务成功，产出 assets 里的每个素材（一个任务可以返回多张） */
const imageDone = (id: number, assets: number[], over = {}) =>
  makeTask({
    id,
    kind: "image",
    status: "succeeded",
    outputs: assets.map((asset) => ({
      asset_id: asset,
      url: `/files/${asset}.png`,
      media_type: "image",
    })),
    ...over,
  });

const out = (id: string): NodeOutput => ({
  id,
  src: `/files/${id}.png`,
  mediaType: "image",
  assetId: id,
  taskId: "1",
  createdAt: 1,
});

/** 提交了 3 个任务、还没有任何一张落下来的图片节点 */
const batch = (over: Partial<CanvasNodeData> = {}) =>
  makeData({
    kind: "image",
    status: "running",
    taskId: "1",
    pendingTaskIds: ["1", "2", "3"],
    ...over,
  });

/** 第一张已经落下来、其余还在跑的节点 */
const landed = (over: Partial<CanvasNodeData> = {}) =>
  batch({
    status: "done",
    pendingTaskIds: ["2", "3"],
    outputs: [out("11")],
    activeOutputId: "11",
    src: "/files/11.png",
    assetId: "11",
    mediaType: "image",
    ...over,
  });

describe("批次回填：先到先显示，本次第一张自动成主图", () => {
  test("第一张到达：成为主图、节点 done，任务移出 pendingTaskIds", () => {
    const patch = planBackfill(batch(), imageDone(1, [11]));
    expect(patch).toMatchObject({
      status: "done",
      activeOutputId: "11",
      src: "/files/11.png",
      assetId: "11",
      pendingTaskIds: ["2", "3"],
    });
    expect(patch?.outputs?.map((item) => item.id)).toEqual(["11"]);
  });

  test("后到的只追加进历史，不抢主图、不动 src 镜像", () => {
    const patch = planBackfill(landed(), imageDone(2, [12]));
    expect(patch?.outputs?.map((item) => item.id)).toEqual(["11", "12"]);
    expect(patch).toMatchObject({ activeOutputId: "11", pendingTaskIds: ["3"] });
    expect(patch).not.toHaveProperty("src");
    expect(patch).not.toHaveProperty("status");
  });

  test("用户已经把主图改成别的版本，后到的仍不抢", () => {
    const data = landed({
      outputs: [out("11"), out("12")],
      activeOutputId: "12",
      src: "/files/12.png",
      assetId: "12",
      pendingTaskIds: ["3"],
    });
    const patch = planBackfill(data, imageDone(3, [13]));
    expect(patch).toMatchObject({ activeOutputId: "12" });
    expect(patch?.outputs?.map((item) => item.id)).toEqual(["11", "12", "13"]);
  });

  test("最后一个任务到达后 pendingTaskIds 被清掉", () => {
    const data = landed({ pendingTaskIds: ["3"] });
    const patch = planBackfill(data, imageDone(3, [13]));
    expect(patch && "pendingTaskIds" in patch).toBe(true);
    expect(patch?.pendingTaskIds).toBeUndefined();
  });

  test("幂等：已经追加过的产物再来一次，不重复、不再改动", () => {
    const data = landed({ pendingTaskIds: ["3"] });
    // 任务 1 是 taskId，但它早就从集合里移走、产物也在历史里
    expect(planBackfill(data, imageDone(1, [11]))).toBeNull();
    // 任务 2 已不在集合里
    expect(planBackfill(data, imageDone(2, [12]))).toBeNull();
  });

  test("不属于这个节点的任务（既不是 taskId 也不在集合里）不回填", () => {
    expect(planBackfill(batch(), imageDone(9, [19]))).toBeNull();
  });

  test("历史是旧画布的单版本时，后到的追加不会让当前版本意外换成新的", () => {
    const data = makeData({
      kind: "image",
      status: "done",
      taskId: "1",
      pendingTaskIds: ["2"],
      src: "/files/5.png",
      assetId: "5",
      mediaType: "image",
    });
    const patch = planBackfill(data, imageDone(2, [12]));
    expect(patch?.activeOutputId).toBe("5");
    expect(patch?.outputs?.map((item) => item.id)).toEqual(["5", "12"]);
  });
});

describe("一个任务返回多张：全部进历史", () => {
  test("本次第一个任务返回 2 张：第一张成主图，第二张只追加", () => {
    const patch = planBackfill(batch(), imageDone(1, [11, 12]));
    expect(patch?.outputs?.map((item) => item.id)).toEqual(["11", "12"]);
    expect(patch).toMatchObject({ status: "done", activeOutputId: "11", assetId: "11" });
    expect(patch?.outputs?.every((item) => item.taskId === "1")).toBe(true);
  });

  test("后到的任务返回多张：全部追加，不动主图", () => {
    const patch = planBackfill(landed(), imageDone(2, [12, 13]));
    expect(patch?.outputs?.map((item) => item.id)).toEqual(["11", "12", "13"]);
    expect(patch).toMatchObject({ activeOutputId: "11" });
  });

  test("单任务（没有 pendingTaskIds）返回多张也都进历史", () => {
    const data = makeData({ kind: "image", status: "running", taskId: "1" });
    const patch = planBackfill(data, imageDone(1, [11, 12, 13]));
    expect(patch?.outputs?.map((item) => item.id)).toEqual(["11", "12", "13"]);
    expect(patch).toMatchObject({ status: "done", activeOutputId: "11" });
  });

  test("个别成员没有地址（组图里某张失败）跳过，其余照常入列", () => {
    const view = makeTask({
      id: 1,
      kind: "image",
      status: "succeeded",
      outputs: [
        { asset_id: 11, url: "/files/11.png", media_type: "image" },
        { asset_id: 0, url: "", media_type: "image" },
        { asset_id: 13, url: "/files/13.png", media_type: "image" },
      ],
    });
    const patch = planBackfill(batch(), view);
    expect(patch?.outputs?.map((item) => item.id)).toEqual(["11", "13"]);
  });
});

describe("批次失败 / 取消", () => {
  test("一个任务失败：只移出集合，节点仍在生成中，别的任务照常", () => {
    const patch = planBackfill(batch(), makeTask({ id: 1, status: "failed", error_message: "x" }));
    expect(patch).toEqual({ pendingTaskIds: ["2", "3"] });
  });

  test("最后一个任务也失败、而且一张都没成功：节点失败态并清掉集合", () => {
    const data = batch({ pendingTaskIds: ["3"] });
    const patch = planBackfill(
      data,
      makeTask({ id: 3, status: "failed", error_message: "审核未通过" }),
    );
    expect(patch).toMatchObject({ status: "error", error: "审核未通过", src: null });
    expect(patch?.pendingTaskIds).toBeUndefined();
  });

  test("已有结果时某个任务失败：节点保持 done，只移出集合", () => {
    const patch = planBackfill(landed(), makeTask({ id: 2, status: "failed", error_message: "x" }));
    expect(patch).toEqual({ pendingTaskIds: ["3"] });
  });

  test("失败的任务重复推送：已经不在集合里、节点 done 时不再改节点", () => {
    const data = landed({ pendingTaskIds: ["3"] });
    expect(
      planBackfill(data, makeTask({ id: 1, status: "failed", error_message: "x" })),
    ).toBeNull();
  });

  test("成功但没有可用产物，按失败处理（最后一个时节点失败）", () => {
    const data = batch({ pendingTaskIds: ["3"] });
    const view = makeTask({ id: 3, kind: "image", status: "succeeded", outputs: [] });
    expect(planBackfill(data, view)).toMatchObject({
      status: "error",
      error: "生成结果为空，请重试",
    });
  });

  test("取消其中一个：只移出集合", () => {
    const patch = planBackfill(batch(), makeTask({ id: 2, status: "canceled" }));
    expect(patch).toEqual({ pendingTaskIds: ["1", "3"] });
  });

  test("全部取消、没有历史：回到 idle，并清掉 taskId 与集合", () => {
    const data = batch({ pendingTaskIds: ["3"] });
    const patch = planBackfill(data, makeTask({ id: 3, status: "canceled" }));
    expect(patch).toMatchObject({ status: "idle", taskId: undefined, src: null });
    expect(patch?.pendingTaskIds).toBeUndefined();
  });

  test("全部取消、有历史：退回当前那一版", () => {
    const data = batch({
      pendingTaskIds: ["3"],
      outputs: [out("5")],
      activeOutputId: "5",
    });
    const patch = planBackfill(data, makeTask({ id: 3, status: "canceled" }));
    expect(patch).toMatchObject({ status: "done", activeOutputId: "5", src: "/files/5.png" });
  });

  test("已有结果时取消剩余任务：节点保持 done，只移出集合", () => {
    const patch = planBackfill(landed(), makeTask({ id: 2, status: "canceled" }));
    expect(patch).toEqual({ pendingTaskIds: ["3"] });
  });
});

describe("applyBackfill：一个节点有多个任务", () => {
  test("多个任务一起到（重开画布后对账）：第一个成主图，其余追加，全部结清", () => {
    const nodes = [{ id: "a", data: batch() }];
    const tasks = {
      "1": imageDone(1, [11]),
      "2": imageDone(2, [12]),
      "3": makeTask({ id: 3, status: "failed", error_message: "x" }),
    };
    const { nodes: next, events } = applyBackfill(nodes, tasks);
    expect(next[0].data).toMatchObject({ status: "done", activeOutputId: "11" });
    expect(next[0].data.outputs?.map((item) => item.id)).toEqual(["11", "12"]);
    expect(next[0].data.pendingTaskIds).toBeUndefined();
    expect(events.map((event) => event.view.id)).toEqual([1, 2, 3]);
    // 再跑一遍：没有变化，引用不变
    expect(applyBackfill(next, tasks).nodes).toBe(next);
  });

  test("只到了一部分：其余仍在集合里等", () => {
    const nodes = [{ id: "a", data: batch() }];
    const { nodes: next } = applyBackfill(nodes, { "2": imageDone(2, [12]) });
    expect(next[0].data).toMatchObject({ status: "done", activeOutputId: "12" });
    expect(next[0].data.pendingTaskIds).toEqual(["1", "3"]);
  });

  test("没有批次的节点保持原来的行为和引用", () => {
    const nodes = [{ id: "a", data: makeData({ status: "running", taskId: "1" }) }];
    expect(applyBackfill(nodes, {}).nodes).toBe(nodes);
  });
});

describe("failedBatchMembers：批次里失败了几个（用来弹一条提示）", () => {
  test("节点没有因此失败（还有结果）时，按节点统计失败的任务", () => {
    const nodes = [{ id: "a", data: landed() }];
    const tasks = {
      "2": makeTask({ id: 2, status: "failed", error_message: "审核未通过" }),
      "3": makeTask({ id: 3, status: "expired", error_message: "" }),
    };
    const { events } = applyBackfill(nodes, tasks);
    expect(failedBatchMembers(events)).toEqual({ a: ["审核未通过", "生成超时"] });
  });

  test("节点整体失败（status 变 error）时不重复提示，由节点自己展示", () => {
    const nodes = [{ id: "a", data: batch({ pendingTaskIds: ["3"] }) }];
    const { events } = applyBackfill(nodes, {
      "3": makeTask({ id: 3, status: "failed", error_message: "x" }),
    });
    expect(failedBatchMembers(events)).toEqual({});
  });
});

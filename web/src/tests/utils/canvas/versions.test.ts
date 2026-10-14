import { describe, expect, test } from "bun:test";

import type { CanvasNode, NodeOutput } from "@/types";
import {
  buildVersionNodes,
  canSetMain,
  recentFailedTasks,
  showVersionBadge,
} from "@/utils/canvas/versions";

import { makeData, makeTask } from "../tasks/fixtures";

const out = (id: string, over: Partial<NodeOutput> = {}): NodeOutput => ({
  id,
  src: `/files/${id}.png`,
  mediaType: "image",
  assetId: id,
  createdAt: 1,
  ...over,
});

const source = (data = {}): CanvasNode => ({
  id: "src",
  type: "canvas",
  position: { x: 0, y: 0 },
  measured: { width: 200, height: 100 },
  data: makeData({ kind: "image", label: "图片 7", ...data }),
});

describe("showVersionBadge：节点右上角的多版本徽标", () => {
  test("2 版及以上显示，数字是历史版本总数", () => {
    expect(showVersionBadge(makeData({ outputs: [out("1"), out("2")] }))).toEqual({
      count: 2,
      generating: false,
    });
  });

  test("只有 1 版且没有任务在跑：不显示（也就没有历史入口）", () => {
    expect(showVersionBadge(makeData({ outputs: [out("1")] }))).toBeNull();
    expect(showVersionBadge(makeData({}))).toBeNull();
  });

  test("一提交就显示真实数量：4 个任务刚提交、一张都没出，数字就是 4，带转圈", () => {
    const data = makeData({ status: "running", taskId: "1", pendingTaskIds: ["1", "2", "3", "4"] });
    expect(showVersionBadge(data)).toEqual({ count: 4, generating: true });
  });

  test("陆续出图时总数不变（已出 + 还在跑），不是只数成功的", () => {
    const data = makeData({ status: "done", outputs: [out("1")], pendingTaskIds: ["2", "3", "4"] });
    expect(showVersionBadge(data)).toEqual({ count: 4, generating: true });
  });

  test("重新生成 1 张：历史里已有旧版本，数字 = 旧版本 + 正在生成的 1 张", () => {
    const data = makeData({ status: "running", outputs: [out("1"), out("2")] });
    expect(showVersionBadge(data)).toEqual({ count: 3, generating: true });
    expect(showVersionBadge(makeData({ status: "running", outputs: [out("1")] }))).toEqual({
      count: 2,
      generating: true,
    });
  });

  test("没有历史、只生成 1 张：不显示", () => {
    expect(showVersionBadge(makeData({ status: "running", taskId: "1" }))).toBeNull();
  });

  test("文本节点永远没有", () => {
    expect(
      showVersionBadge(makeData({ kind: "script", outputs: [out("1"), out("2")] })),
    ).toBeNull();
  });

  test("旧画布只有 src + assetId 的单版本不显示", () => {
    expect(
      showVersionBadge(makeData({ src: "/files/a.png", assetId: "5", mediaType: "image" })),
    ).toBeNull();
  });
});

describe("canSetMain：单击设为主图的条件", () => {
  test("生成中（还没出第一张）不能设，给出原因", () => {
    expect(canSetMain(makeData({ status: "running", outputs: [out("1")] }), "1")).toEqual({
      ok: false,
      reason: "生成中，出第一张后可切换",
    });
  });

  test("已经是主图且节点正常：什么也不用做", () => {
    const data = makeData({ status: "done", outputs: [out("1"), out("2")], activeOutputId: "2" });
    expect(canSetMain(data, "2")).toEqual({ ok: false, reason: null });
  });

  test("节点失败后点当前那一版：放行（把它找回来）", () => {
    const data = makeData({ status: "error", outputs: [out("1"), out("2")], activeOutputId: "2" });
    expect(canSetMain(data, "2")).toEqual({ ok: true });
  });

  test("一批任务还有在跑、但第一张已出：可以设", () => {
    const data = makeData({
      status: "done",
      outputs: [out("1"), out("2")],
      activeOutputId: "1",
      pendingTaskIds: ["3"],
    });
    expect(canSetMain(data, "2")).toEqual({ ok: true });
  });

  test("不存在的版本不能设", () => {
    expect(canSetMain(makeData({ status: "done", outputs: [out("1")] }), "9")).toEqual({
      ok: false,
      reason: null,
    });
  });
});

describe("buildVersionNodes：把历史版本拆成独立节点（添加到画布）", () => {
  let n = 0;
  const newId = () => `id${++n}`;

  test("每一版一个节点：done、带素材、自己的单版本历史，没有连线字段与任务", () => {
    n = 0;
    const nodes = buildVersionNodes(source(), [source()], [out("1"), out("2")], newId);
    expect(nodes).toHaveLength(2);
    expect(nodes[0]).toMatchObject({
      id: "id1",
      type: "canvas",
      selected: true,
      data: {
        kind: "image",
        label: "图片 7 · 版本1",
        status: "done",
        src: "/files/1.png",
        assetId: "1",
        mediaType: "image",
        activeOutputId: "1",
      },
    });
    expect(nodes[0].data.outputs).toEqual([out("1")]);
    expect(nodes[1].data.label).toBe("图片 7 · 版本2");
    expect(nodes[0].data.taskId).toBeUndefined();
  });

  test("摆在原节点右侧，互相不重叠、避开已有节点", () => {
    n = 0;
    const blocker: CanvasNode = { ...source(), id: "b", position: { x: 240, y: 0 } };
    const nodes = buildVersionNodes(source(), [source(), blocker], [out("1"), out("2")], newId);
    for (const node of nodes) expect(node.position.x).toBeGreaterThan(0);
    expect(nodes[0].position).not.toEqual(blocker.position);
    expect(nodes[0].position).not.toEqual(nodes[1].position);
  });

  test("版本序号按在历史里的位置，不是传进来的顺序", () => {
    n = 0;
    const all = [out("a"), out("b"), out("c")];
    const nodes = buildVersionNodes(source({ outputs: all }), [source()], [all[2]], newId);
    expect(nodes[0].data.label).toBe("图片 7 · 版本3");
  });
});

describe("recentFailedTasks：弹窗里的「生成失败」格", () => {
  const task = (id: number, over = {}) =>
    makeTask({
      id,
      node_id: "n1",
      status: "failed",
      error_message: "审核未通过",
      created_at: "2026-10-15T10:00:00Z",
      ...over,
    });

  test("只取这个节点、失败或过期的任务，带上原因", () => {
    const tasks = {
      "1": task(1),
      "2": task(2, { node_id: "other" }),
      "3": task(3, { status: "succeeded" }),
      "4": task(4, { status: "expired", error_message: "" }),
    };
    expect(recentFailedTasks(tasks, "n1").map((item) => [item.id, item.message])).toEqual([
      ["1", "审核未通过"],
      ["4", "生成超时"],
    ]);
  });

  test("只算最近一批：比这个节点最新任务早 30 秒以上的旧失败不再显示", () => {
    const tasks = {
      "1": task(1, { created_at: "2026-10-15T09:00:00Z" }),
      "2": task(2, { created_at: "2026-10-15T10:00:00Z" }),
      "3": task(3, { created_at: "2026-10-15T10:00:05Z", status: "succeeded" }),
    };
    expect(recentFailedTasks(tasks, "n1").map((item) => item.id)).toEqual(["2"]);
  });

  test("没有失败返回空数组", () => {
    expect(recentFailedTasks({ "1": task(1, { status: "succeeded" }) }, "n1")).toEqual([]);
  });
});

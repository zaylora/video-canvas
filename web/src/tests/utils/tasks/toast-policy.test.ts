import { describe, expect, test } from "bun:test";

import {
  currentCanvasIdFromPath,
  isFreshTerminalTransition,
  planTaskToast,
} from "@/utils/tasks/toast-policy";
import { makeTask, succeeded } from "./fixtures";

describe("currentCanvasIdFromPath", () => {
  test("只在画布页返回 id", () => {
    expect(currentCanvasIdFromPath("/canvas/12")).toBe("12");
    expect(currentCanvasIdFromPath("/canvas/12/")).toBe("12");
    expect(currentCanvasIdFromPath("/")).toBeNull();
    expect(currentCanvasIdFromPath("/admin/ai")).toBeNull();
  });
});

describe("planTaskToast：画布外的完成提示", () => {
  test("成功：《画布名》中的视频已生成，点击跳转到该画布", () => {
    const plan = planTaskToast(succeeded({ canvas_id: "10" }), null, "分镜一");
    expect(plan).toMatchObject({
      tone: "success",
      title: "《分镜一》中的视频已生成",
      href: "/canvas/10",
    });
  });

  test("查不到画布名用「画布」", () => {
    expect(planTaskToast(succeeded(), "99", undefined)?.title).toBe("《画布》中的视频已生成");
  });

  test("失败与超时：带原因和退款说明", () => {
    const failed = planTaskToast(
      makeTask({ status: "failed", error_message: "内容未通过审核" }),
      null,
      "A",
    );
    expect(failed).toMatchObject({ tone: "error", title: "《A》中的视频生成失败" });
    expect(failed?.description).toContain("内容未通过审核");
    expect(failed?.description).toContain("积分已退回");
    expect(planTaskToast(makeTask({ status: "expired" }), null, "A")?.title).toContain("超时");
  });

  test("失败提示带任务编号（画布与首页对话都一样），成功不带，后端没给就不带", () => {
    const canvas = planTaskToast(
      makeTask({ status: "failed", error_message: "平台繁忙", task_ref: "ab12" }),
      null,
      "A",
    );
    expect(canvas?.description).toBe("平台繁忙，积分已退回 · 任务 ID：ab12");
    const conversation = planTaskToast(
      makeTask({
        status: "failed",
        canvas_id: null,
        node_id: "rec:7:1",
        error_message: "平台繁忙",
        task_ref: "cd34",
      }),
      null,
      undefined,
      "/",
    );
    expect(conversation?.description).toBe("平台繁忙，积分已退回 · 任务 ID：cd34");
    expect(planTaskToast(succeeded(), null, "A")?.description).toBeUndefined();
    expect(
      planTaskToast(makeTask({ status: "failed", error_message: "平台繁忙" }), null, "A")
        ?.description,
    ).toBe("平台繁忙，积分已退回");
  });

  test("正看着这张画布时不弹（节点自己会变）；取消不弹", () => {
    expect(planTaskToast(succeeded({ canvas_id: "10" }), "10", "A")).toBeNull();
    expect(planTaskToast(makeTask({ status: "canceled" }), null, "A")).toBeNull();
  });
});

describe("planTaskToast：首页生成的对话任务（node_id 以 rec: 开头，没有画布）", () => {
  const conv = (over = {}) =>
    succeeded({ canvas_id: null, node_id: "rec:7:0", kind: "image", ...over });

  test("正在对话页看着：不弹（格子自己会变）", () => {
    expect(planTaskToast(conv(), null, undefined, "/conversations/abc")).toBeNull();
    expect(planTaskToast(conv(), null, undefined, "/conversations/new")).toBeNull();
  });

  test("在别的页面：用不带画布名的说法，没有跳转", () => {
    const plan = planTaskToast(conv(), null, undefined, "/");
    expect(plan).toMatchObject({ tone: "success", title: "图片已生成" });
    expect(plan?.href).toBeUndefined();
    const failed = planTaskToast(
      makeTask({
        status: "failed",
        canvas_id: null,
        node_id: "rec:7:1",
        kind: "video",
        error_message: "审核未通过",
      }),
      null,
      undefined,
      "/assets",
    );
    expect(failed).toMatchObject({ tone: "error", title: "视频生成失败" });
    expect(failed?.description).toBe("审核未通过，积分已退回");
  });

  test("取消不弹", () => {
    expect(planTaskToast(conv({ status: "canceled" }), null, undefined, "/")).toBeNull();
  });
});

describe("isFreshTerminalTransition：只在刚进入终态时提醒", () => {
  test("进行中 -> 终态提醒；终态 -> 终态、进行中 -> 进行中不提醒", () => {
    expect(isFreshTerminalTransition(makeTask({ status: "running" }), succeeded(), "live")).toBe(
      true,
    );
    expect(
      isFreshTerminalTransition(succeeded({ version: 5 }), succeeded({ version: 6 }), "live"),
    ).toBe(false);
    expect(
      isFreshTerminalTransition(
        makeTask({ status: "queued" }),
        makeTask({ status: "running" }),
        "live",
      ),
    ).toBe(false);
  });

  test("本地没见过的任务：实时推送算新完成，对账拉回来的陈年任务不算", () => {
    expect(isFreshTerminalTransition(undefined, succeeded(), "live")).toBe(true);
    expect(isFreshTerminalTransition(undefined, succeeded(), "reconcile")).toBe(false);
  });
});

import { describe, expect, test } from "bun:test";

import { countCanvasRunningTasks } from "@/store/tasks";
import type { TaskStatus } from "@/api/generation-task/type";
import type { TaskMap } from "@/utils/tasks/merge";
import { makeTask } from "../utils/tasks/fixtures";

const mapOf = (...tasks: ReturnType<typeof makeTask>[]): TaskMap =>
  Object.fromEntries(tasks.map((t) => [String(t.id), t]));

describe("countCanvasRunningTasks", () => {
  test("统计本画布里 pending / queued / running / finalizing 的任务", () => {
    const statuses: TaskStatus[] = ["pending", "queued", "running", "finalizing"];
    const tasks = mapOf(...statuses.map((status, i) => makeTask({ id: i + 1, status })));
    expect(countCanvasRunningTasks(tasks, "10")).toBe(4);
  });

  test("终态不算：succeeded / failed / canceled / expired", () => {
    const statuses: TaskStatus[] = ["succeeded", "failed", "canceled", "expired"];
    const tasks = mapOf(...statuses.map((status, i) => makeTask({ id: i + 1, status })));
    expect(countCanvasRunningTasks(tasks, "10")).toBe(0);
  });

  test("别的画布、没有画布的任务不算", () => {
    const tasks = mapOf(
      makeTask({ id: 1, canvas_id: "10" }),
      makeTask({ id: 2, canvas_id: "11" }),
      makeTask({ id: 3, canvas_id: null }),
    );
    expect(countCanvasRunningTasks(tasks, "10")).toBe(1);
  });

  test("没有 canvasId（还没进画布）：0", () => {
    expect(countCanvasRunningTasks(mapOf(makeTask()), undefined)).toBe(0);
    expect(countCanvasRunningTasks(mapOf(makeTask()), "")).toBe(0);
  });

  test("空任务库：0", () => {
    expect(countCanvasRunningTasks({}, "10")).toBe(0);
  });
});

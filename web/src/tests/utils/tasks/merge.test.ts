import { describe, expect, test } from "bun:test";

import { MAX_TERMINAL_TASKS, mergeTask, mergeTasks, pruneTasks } from "@/utils/tasks/merge";
import { makeTask, succeeded } from "./fixtures";

describe("mergeTask：只在 version 更大时更新", () => {
  test("首次写入生效", () => {
    const view = makeTask({ version: 1 });
    const result = mergeTask({}, view);
    expect(result.applied).toBe(true);
    expect(result.tasks["1"]).toBe(view);
    expect(result.prev).toBeUndefined();
  });

  test("更大的版本覆盖，并带出旧快照", () => {
    const old = makeTask({ version: 2, status: "queued" });
    const next = makeTask({ version: 3, status: "running" });
    const result = mergeTask({ "1": old }, next);
    expect(result.applied).toBe(true);
    expect(result.prev).toBe(old);
    expect(result.tasks["1"].status).toBe("running");
  });

  test("相同版本（重复推送）与更小版本（乱序）都被吸收，且不产生新对象", () => {
    const current = makeTask({ version: 5, status: "succeeded" });
    const tasks = { "1": current };
    const dup = mergeTask(tasks, makeTask({ version: 5, status: "running" }));
    const stale = mergeTask(tasks, makeTask({ version: 4, status: "queued" }));
    expect(dup.applied).toBe(false);
    expect(stale.applied).toBe(false);
    expect(dup.tasks).toBe(tasks);
    expect(stale.tasks).toBe(tasks);
  });

  test("字符串 id 与数字 id 视为同一个任务", () => {
    const first = mergeTask({}, makeTask({ id: "9", version: 1 }));
    const second = mergeTask(first.tasks, makeTask({ id: 9, version: 2 }));
    expect(second.applied).toBe(true);
    expect(Object.keys(second.tasks)).toEqual(["9"]);
  });
});

describe("mergeTasks：乱序批量合并只留最大版本", () => {
  test("同一个任务的多份快照，乱序到达也收敛到最大版本", () => {
    const views = [
      makeTask({ version: 3, status: "finalizing" }),
      makeTask({ version: 1, status: "queued" }),
      succeeded({ version: 5 }),
      makeTask({ version: 2, status: "running" }),
    ];
    const { tasks, applied } = mergeTasks({}, views);
    expect(tasks["1"].version).toBe(5);
    expect(tasks["1"].status).toBe("succeeded");
    // 3 生效、1 被吸收、5 生效、2 被吸收
    expect(applied.map((item) => item.view.version)).toEqual([3, 5]);
  });
});

describe("pruneTasks：终态任务有上限，进行中的永远保留", () => {
  test("超出上限时丢掉最早结束的终态任务", () => {
    let tasks = {};
    for (let i = 1; i <= MAX_TERMINAL_TASKS + 5; i++) {
      tasks = mergeTask(
        tasks,
        succeeded({
          id: i,
          finished_at: new Date(2026, 8, 29, 10, 0, i).toISOString(),
        }),
      ).tasks;
    }
    expect(Object.keys(tasks).length).toBe(MAX_TERMINAL_TASKS);
    expect("1" in tasks).toBe(false);
    expect(`${MAX_TERMINAL_TASKS + 5}` in tasks).toBe(true);
  });

  test("进行中的任务不受上限影响", () => {
    const running = makeTask({ id: 999, status: "running" });
    const tasks: Record<string, ReturnType<typeof makeTask>> = { "999": running };
    for (let i = 1; i <= 10; i++) tasks[String(i)] = succeeded({ id: i });
    const pruned = pruneTasks(tasks, 3);
    expect(pruned["999"]).toBe(running);
    expect(Object.keys(pruned).length).toBe(4);
  });
});

import { describe, expect, test } from "bun:test";

import { isFreshTerminalTransition } from "@/utils/tasks/toast-policy";
import { makeTask, succeeded } from "./fixtures";

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

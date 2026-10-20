import { describe, expect, test } from "bun:test";

import { undoAfterSave } from "@/utils/agent/undo-after-save";

/**
 * 撤销本轮是在服务端按「Agent 写入的样子」判断你有没有改过：服务端看到的节点和 Agent 写的一样就删掉。
 * 所以撤销前必须先把本地还没保存的改动存上去，否则你刚改的提示词、刚连的线服务端不知道，会被一起删掉。
 */
describe("undoAfterSave：先保存，再撤销", () => {
  test("保存成功：保存完才调撤销，返回撤销的结果", async () => {
    const calls: string[] = [];
    const result = await undoAfterSave({
      save: async () => {
        calls.push("save-start");
        await Promise.resolve();
        calls.push("save-end");
        return true;
      },
      undo: async () => {
        calls.push("undo");
        return { reverted: 3 };
      },
    });
    expect(calls).toEqual(["save-start", "save-end", "undo"]);
    expect(result).toEqual({ ok: true, value: { reverted: 3 } });
  });

  test("保存没成功（离线、冲突）：不撤销，说明原因，免得删掉你没存上的改动", async () => {
    let undone = false;
    const result = await undoAfterSave({
      save: async () => false,
      undo: async () => {
        undone = true;
        return { reverted: 1 };
      },
    });
    expect(undone).toBe(false);
    expect(result).toEqual({ ok: false, reason: "save-failed" });
  });

  test("保存抛错：同样不撤销", async () => {
    let undone = false;
    const result = await undoAfterSave({
      save: async () => {
        throw new Error("网络断了");
      },
      undo: async () => {
        undone = true;
        return null;
      },
    });
    expect(undone).toBe(false);
    expect(result).toEqual({ ok: false, reason: "save-failed" });
  });

  test("撤销接口自己失败（返回 null）：如实返回，不当成成功", async () => {
    const result = await undoAfterSave({ save: async () => true, undo: async () => null });
    expect(result).toEqual({ ok: false, reason: "undo-failed" });
  });
});

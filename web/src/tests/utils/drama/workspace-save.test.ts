import { describe, expect, test } from "bun:test";

import { submitNodeById, hasNodeActions } from "@/components/canvas-editor/node-actions";
import { summarizeSave } from "@/pages/drama/workspace/use-workspace";

describe("顶栏保存状态汇总", () => {
  test("全部已保存", () => {
    expect(summarizeSave({})).toBe("saved");
    expect(summarizeSave({ outline: "saved", ep1: "saved" })).toBe("saved");
  });

  test("有待保存 / 保存中：显示保存中", () => {
    expect(summarizeSave({ outline: "dirty" })).toBe("saving");
    expect(summarizeSave({ outline: "saved", ep1: "saving" })).toBe("saving");
  });

  test("失败优先于保存中，冲突优先于失败", () => {
    expect(summarizeSave({ a: "saving", b: "error" })).toBe("error");
    expect(summarizeSave({ a: "error", b: "conflict", c: "saving" })).toBe("conflict");
  });
});

describe("节点生成动作登记", () => {
  test("没登记的节点：给出原因，不会抛错", async () => {
    expect(hasNodeActions("nope")).toBe(false);
    expect(await submitNodeById("nope")).toBe("节点还没准备好");
  });
});

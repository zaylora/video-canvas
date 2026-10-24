import { describe, expect, test } from "bun:test";

import { createTransferCancel } from "@/utils/canvas/transfer-cancel";

describe("createTransferCancel：一次导入 / 导出的取消控制器", () => {
  test("刚创建：没取消，信号未中止", () => {
    const c = createTransferCancel();
    expect(c.cancelled()).toBe(false);
    expect(c.signal.aborted).toBe(false);
  });

  test("cancel() 中止信号并标记已取消", () => {
    const c = createTransferCancel();
    c.cancel();
    expect(c.cancelled()).toBe(true);
    expect(c.signal.aborted).toBe(true);
  });

  test("重复 cancel() 只生效一次，不抛错", () => {
    const c = createTransferCancel();
    let aborts = 0;
    c.signal.addEventListener("abort", () => (aborts += 1));
    c.cancel();
    c.cancel();
    expect(aborts).toBe(1);
  });

  test("每次创建都是新的控制器：上一次取消不影响下一次", () => {
    const first = createTransferCancel();
    first.cancel();
    const second = createTransferCancel();
    expect(second.cancelled()).toBe(false);
    expect(second.signal.aborted).toBe(false);
  });
});

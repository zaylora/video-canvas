import { describe, expect, test } from "bun:test";

import { dialogEntry, readDialogEntry } from "@/utils/admin/dialog-entry";

describe("dialogEntry / readDialogEntry：别的页面跳过来时带的“要打开的弹窗”", () => {
  test("带进去的目标能原样读出来", () => {
    const state = dialogEntry({ kind: "edit", key: "newapi" });
    expect(readDialogEntry(state)).toEqual({ kind: "edit", key: "newapi" });
  });

  test("没有路由 state、或 state 里没有这一项时读到 null", () => {
    expect(readDialogEntry(null)).toBeNull();
    expect(readDialogEntry(undefined)).toBeNull();
    expect(readDialogEntry({})).toBeNull();
    expect(readDialogEntry("x")).toBeNull();
  });
});

import { describe, expect, test } from "bun:test";

import { FOLDER_NAME_MAX, validateFolderName } from "@/utils/assets/folder-name";

describe("validateFolderName：文件夹名称", () => {
  test("去掉首尾空白后返回", () => {
    expect(validateFolderName("  角色图  ")).toEqual({ ok: true, name: "角色图" });
  });

  test("空串和纯空白不合法", () => {
    expect(validateFolderName("")).toMatchObject({ ok: false });
    expect(validateFolderName("   \n ")).toMatchObject({ ok: false });
  });

  test("最多 30 个字，按字符而不是 UTF-16 单元数", () => {
    expect(FOLDER_NAME_MAX).toBe(30);
    expect(validateFolderName("字".repeat(30))).toMatchObject({ ok: true });
    expect(validateFolderName("字".repeat(31))).toMatchObject({ ok: false });
    // 每个 emoji 是两个 UTF-16 单元，30 个仍然合法
    expect(validateFolderName("😀".repeat(30))).toMatchObject({ ok: true });
  });

  test("首尾空白不占字数", () => {
    expect(validateFolderName(` ${"字".repeat(30)} `)).toMatchObject({ ok: true });
  });
});

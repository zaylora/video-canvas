import { describe, expect, test } from "bun:test";

import { copyToClipboard } from "@/utils/admin/clipboard";

describe("复制到剪贴板", () => {
  test("剪贴板写入成功：返回 true，写入的就是原文", async () => {
    const written: string[] = [];
    const ok = await copyToClipboard("修复信息", {
      write: async (text) => void written.push(text),
    });
    expect(ok).toBe(true);
    expect(written).toEqual(["修复信息"]);
  });

  test("剪贴板 API 不存在（非 https 页面里 navigator.clipboard 是 undefined）：不抛错，改用兜底复制", async () => {
    const fallback: string[] = [];
    const ok = await copyToClipboard("修复信息", {
      write: undefined,
      fallback: (text) => {
        fallback.push(text);
        return true;
      },
    });
    expect(ok).toBe(true);
    expect(fallback).toEqual(["修复信息"]);
  });

  test("剪贴板被拒绝（权限、焦点）：同样走兜底", async () => {
    const ok = await copyToClipboard("x", {
      write: async () => {
        throw new Error("NotAllowedError");
      },
      fallback: () => true,
    });
    expect(ok).toBe(true);
  });

  test("两条路都失败：返回 false，不抛错", async () => {
    const ok = await copyToClipboard("x", {
      write: async () => {
        throw new Error("denied");
      },
      fallback: () => false,
    });
    expect(ok).toBe(false);
    expect(await copyToClipboard("x", { write: undefined, fallback: undefined })).toBe(false);
  });

  test("兜底复制自己抛错也算失败，不向外抛", async () => {
    const ok = await copyToClipboard("x", {
      write: undefined,
      fallback: () => {
        throw new Error("execCommand 不可用");
      },
    });
    expect(ok).toBe(false);
  });
});

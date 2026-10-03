import { describe, expect, test } from "bun:test";

import {
  errorMessage,
  isStorageFieldLocked,
  isStorageVersionConflict,
  isChannelKeyExists,
  isRunnerDown,
  isSecretMissing,
  isTooLarge,
} from "@/utils/admin/errors";

describe("后端错误识别", () => {
  test("runner 不可用：503 或 50021", () => {
    expect(isRunnerDown({ status: 503 })).toBe(true);
    expect(isRunnerDown({ code: 50021 })).toBe(true);
    expect(isRunnerDown({ status: 500, code: 50000 })).toBe(false);
    expect(isRunnerDown(undefined)).toBe(false);
  });

  test("Key 未设置只认业务码 50015，别的 409 不算", () => {
    expect(isSecretMissing({ status: 409, code: 50015 })).toBe(true);
    expect(isSecretMissing({ status: 409, code: 50012 })).toBe(false);
  });

  test("文件超限与渠道 key 冲突", () => {
    expect(isTooLarge({ status: 413 })).toBe(true);
    expect(isTooLarge({ code: 50007 })).toBe(true);
    expect(isChannelKeyExists({ code: 50012 })).toBe(true);
    expect(isChannelKeyExists({ code: 50013 })).toBe(false);
  });

  test("errorMessage 取 message，没有就用兜底", () => {
    expect(errorMessage({ message: "boom" })).toBe("boom");
    expect(errorMessage({ message: "" }, "兜底")).toBe("兜底");
    expect(errorMessage(null)).toBe("请求失败");
  });
});

describe("存储配置错误识别", () => {
  test("版本冲突：业务码 51009（只看 409 会把别的冲突也算进来）", () => {
    expect(isStorageVersionConflict({ status: 409, code: 51009 })).toBe(true);
    expect(isStorageVersionConflict({ status: 409, code: 51005 })).toBe(false);
    expect(isStorageVersionConflict({ status: 409 })).toBe(false);
  });

  test("定位字段被锁：业务码 51005", () => {
    expect(isStorageFieldLocked({ code: 51005 })).toBe(true);
    expect(isStorageFieldLocked({ code: 51009 })).toBe(false);
    expect(isStorageFieldLocked(undefined)).toBe(false);
  });
});

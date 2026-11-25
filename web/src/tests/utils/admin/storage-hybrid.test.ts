import { describe, expect, test } from "bun:test";

import type { StorageHybridStatus } from "@/api/admin/storage/type.d";
import { hybridStorageLocks, parseHybridLimit } from "@/utils/admin/storage-hybrid";

describe("混合策略", () => {
  test("以字节存储限额，0 不限额", () => {
    expect(parseHybridLimit("0")).toBe(0);
    expect(parseHybridLimit("1.25")).toBe(1_250_000_000);
    for (const input of ["", "-1", "0x10", "1.2.3", "0.00000000001", "9999999999"]) {
      expect(parseHybridLimit(input)).toBeNull();
    }
  });

  test("只锁入选云端的删除，但锁住全部设为默认操作", () => {
    const status: StorageHybridStatus = {
      state: "active",
      cloud_id: 5,
      local_id: 1,
      limit_bytes: 0,
      error: "",
      has_cloud: true,
    };
    expect(hybridStorageLocks(status, 5).deleteReason).toBeTruthy();
    expect(hybridStorageLocks(status, 3).deleteReason).toBe("");
    expect(hybridStorageLocks(status, 3).defaultReason).toBeTruthy();
    expect(hybridStorageLocks({ ...status, state: "off" }, 5)).toEqual({
      defaultReason: "",
      deleteReason: "",
    });
  });
});

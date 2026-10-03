import { describe, expect, test } from "bun:test";

import type { StoragePreset, StorageView } from "@/api/admin-storage/type";
import {
  accessLabel,
  checkStatus,
  defaultBlockReason,
  deleteBlockReason,
  formatTtl,
  isFieldLocked,
  lockReason,
  providerLabel,
  storageLocation,
  ttlOptions,
} from "@/utils/admin/storage-rules";

const view = (patch: Partial<StorageView> = {}): StorageView => ({
  id: 2,
  name: "OSS 杭州",
  provider: "aliyun_oss",
  builtin: false,
  account_id: "",
  endpoint: "oss-cn-hangzhou.aliyuncs.com",
  region: "cn-hangzhou",
  bucket: "vc-prod",
  path_prefix: "",
  addressing: "virtual",
  use_ssl: true,
  access_key_id: "LTAI******",
  secret_set: true,
  public_base_url: "",
  access: "private",
  signed_ttl_sec: 3600,
  direct_upload: false,
  direct_method: "post_policy",
  is_default: false,
  asset_count: 0,
  locked: false,
  check: { ok: true, at: "2026-10-03T08:00:00Z", error: "" },
  version: 1,
  updated_at: "",
  created_at: "",
  ...patch,
});

const local = view({
  id: 1,
  name: "本地磁盘",
  provider: "local",
  builtin: true,
  is_default: true,
  local_dir: "./data/assets",
  bucket: "",
  region: "",
  endpoint: "",
  check: null,
});

describe("isFieldLocked：已有素材引用时哪些字段不能改", () => {
  const locked = { editing: true, locked: true };

  test("定位字段全部锁定", () => {
    for (const field of [
      "region",
      "accountId",
      "endpoint",
      "bucket",
      "pathPrefix",
      "addressing",
    ] as const) {
      expect(isFieldLocked(field, locked)).toBe(true);
    }
  });

  test("名称、访问方式、有效期、直传、HTTPS 仍可改", () => {
    for (const field of [
      "name",
      "access",
      "publicBaseUrl",
      "signedTtlSec",
      "directUpload",
      "useSSL",
    ] as const) {
      expect(isFieldLocked(field, locked)).toBe(false);
    }
  });

  test("新建或没有素材引用时都不锁", () => {
    expect(isFieldLocked("bucket", { editing: false, locked: true })).toBe(false);
    expect(isFieldLocked("bucket", { editing: true, locked: false })).toBe(false);
  });
});

describe("lockReason", () => {
  test("写明素材数与后果，并引导新建存储", () => {
    expect(lockReason(1284)).toBe(
      "已有 1,284 个素材引用，修改会导致它们无法访问；如需换桶请新建存储",
    );
  });
});

describe("defaultBlockReason：设为默认", () => {
  test("正常的对象存储可以设为默认", () => {
    expect(defaultBlockReason(view())).toBeNull();
  });

  test("已是默认", () => {
    expect(defaultBlockReason(view({ is_default: true }))).toBe("已是默认存储");
  });

  test("最近一次测试未通过不能设为默认", () => {
    expect(defaultBlockReason(view({ check: { ok: false, at: "", error: "AccessDenied" } }))).toBe(
      "最近一次测试未通过，不能设为默认",
    );
  });

  test("还没测试过也不能设为默认", () => {
    expect(defaultBlockReason(view({ check: null }))).toBe("还没有测试通过，请先测试连接");
  });

  test("内置本地磁盘不受测试结果限制", () => {
    expect(defaultBlockReason({ ...local, is_default: false })).toBeNull();
  });
});

describe("deleteBlockReason：删除", () => {
  test("没有任何限制时可删", () => {
    expect(deleteBlockReason(view())).toBeNull();
  });

  test("内置不可删", () => {
    expect(deleteBlockReason(local)).toBe("内置存储不可删除");
  });

  test("默认不可删", () => {
    expect(deleteBlockReason(view({ is_default: true }))).toBe(
      "默认存储不可删除，请先把其他存储设为默认",
    );
  });

  test("被素材引用不可删，写出数量", () => {
    expect(deleteBlockReason(view({ asset_count: 1284 }))).toBe("被 1,284 个素材引用，不可删除");
  });

  test("内置优先于默认，默认优先于引用", () => {
    expect(deleteBlockReason({ ...local, asset_count: 3 })).toBe("内置存储不可删除");
    expect(deleteBlockReason(view({ is_default: true, asset_count: 3 }))).toContain("默认存储");
  });
});

describe("展示用的小函数", () => {
  test("formatTtl", () => {
    expect(formatTtl(900)).toBe("15 分钟");
    expect(formatTtl(3600)).toBe("1 小时");
    expect(formatTtl(21600)).toBe("6 小时");
    expect(formatTtl(86400)).toBe("24 小时");
    expect(formatTtl(7200)).toBe("2 小时");
    expect(formatTtl(90)).toBe("90 秒");
  });

  test("ttlOptions 固定 4 档，当前值不在档里时补上，不丢用户已存的值", () => {
    expect(ttlOptions(3600).map((o) => o.value)).toEqual([900, 3600, 21600, 86400]);
    const extra = ttlOptions(7200);
    expect(extra.map((o) => o.value)).toEqual([900, 3600, 7200, 21600, 86400]);
    expect(extra[2].label).toBe("2 小时");
  });

  test("accessLabel：本地、公开、私有", () => {
    expect(accessLabel(local)).toBe("/files 直出");
    expect(accessLabel(view({ access: "public" }))).toBe("公开 · CDN");
    expect(accessLabel(view({ signed_ttl_sec: 3600 }))).toBe("私有 · 签名 1h");
    expect(accessLabel(view({ signed_ttl_sec: 900 }))).toBe("私有 · 签名 15m");
  });

  test("storageLocation：本地显示目录，对象存储显示桶与地域，R2 写 auto", () => {
    expect(storageLocation(local)).toEqual({ primary: "./data/assets", secondary: "" });
    expect(storageLocation(view({ path_prefix: "assets" }))).toEqual({
      primary: "vc-prod",
      secondary: "cn-hangzhou · /assets",
    });
    expect(storageLocation(view({ provider: "r2", region: "auto" })).secondary).toBe("R2 · auto");
  });

  test("providerLabel：优先用预设名称，本地磁盘固定文案，找不到预设就回退到服务商 id", () => {
    const presets: StoragePreset[] = [
      {
        provider: "aliyun_oss",
        name: "阿里云 OSS",
        direct_method: "post_policy",
        force_ssl: true,
        addressing: "virtual",
        regions: [],
      },
    ];
    expect(providerLabel("aliyun_oss", presets)).toBe("阿里云 OSS");
    expect(providerLabel("local", presets)).toBe("本地磁盘");
    expect(providerLabel("s3", presets)).toBe("s3");
  });

  test("checkStatus：未测试、正常、失败（带原因）", () => {
    expect(checkStatus(view({ check: null }))).toEqual({
      tone: "neutral",
      label: "未测试",
      detail: "",
    });
    expect(checkStatus(local)).toEqual({ tone: "neutral", label: "无需测试", detail: "" });
    expect(checkStatus(view())).toMatchObject({ tone: "success", label: "正常" });
    expect(
      checkStatus(
        view({
          check: { ok: false, at: "2026-10-03T08:00:00Z", error: "第 1 步失败：密钥无权访问该桶" },
        }),
      ),
    ).toMatchObject({ tone: "danger", label: "失败", detail: "第 1 步失败：密钥无权访问该桶" });
  });
});

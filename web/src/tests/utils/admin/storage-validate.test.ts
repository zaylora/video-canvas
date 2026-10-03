import { describe, expect, test } from "bun:test";

import {
  bucketError,
  emptyStorageForm,
  validateStorageForm,
  type StorageFormState,
} from "@/utils/admin/storage-form";

const ACCOUNT = "0123456789abcdef0123456789abcdef";

const form = (patch: Partial<StorageFormState> = {}): StorageFormState => ({
  ...emptyStorageForm([]),
  name: "OSS 杭州",
  provider: "aliyun_oss",
  region: "cn-hangzhou",
  bucket: "video-canvas",
  accessKeyId: "ak",
  secretKey: "sk",
  ...patch,
});

describe("bucketError", () => {
  test("通用规则：小写字母、数字、- 和 .，3 到 63 位，首尾是字母或数字", () => {
    expect(bucketError("aliyun_oss", "video-canvas")).toBe("");
    expect(bucketError("s3", "my.bucket-1")).toBe("");
    expect(bucketError("s3", "abc")).toBe("");
    expect(bucketError("s3", "ab")).toContain("3–63");
    expect(bucketError("s3", "a".repeat(64))).toContain("3–63");
    expect(bucketError("s3", "a".repeat(63))).toBe("");
    expect(bucketError("s3", "Video")).toContain("小写字母");
    expect(bucketError("s3", "a_b_c")).toContain("小写字母");
    expect(bucketError("s3", "-abc")).toContain("小写字母");
    expect(bucketError("s3", "abc-")).toContain("小写字母");
  });

  test("没填返回空串：是否必填由整表校验决定", () => {
    expect(bucketError("s3", "")).toBe("");
  });

  test("COS 桶名必须带 APPID 后缀（10 位数字）", () => {
    expect(bucketError("tencent_cos", "canvas-1250000000")).toBe("");
    expect(bucketError("tencent_cos", "canvas")).toBe(
      "COS 桶名需要带 APPID 后缀，例如 canvas-1250000000",
    );
    expect(bucketError("tencent_cos", "canvas-125")).toContain("APPID");
  });

  test("其他服务商不要求 APPID 后缀", () => {
    expect(bucketError("r2", "canvas")).toBe("");
  });
});

describe("validateStorageForm", () => {
  test("合法表单没有错误", () => {
    expect(validateStorageForm(form())).toEqual({});
  });

  test("名称：必填，按字符数算最多 64 个（中文一个字算一个）", () => {
    expect(validateStorageForm(form({ name: "   " })).name).toBe("请填写名称");
    expect(validateStorageForm(form({ name: "存".repeat(64) })).name).toBeUndefined();
    expect(validateStorageForm(form({ name: "存".repeat(65) })).name).toBe(
      "名称不能超过 64 个字符",
    );
  });

  test("桶名：必填并套用 bucketError 的规则", () => {
    expect(validateStorageForm(form({ bucket: "" })).bucket).toBe("请填写 Bucket");
    expect(validateStorageForm(form({ bucket: "AB" })).bucket).toContain("小写字母");
    expect(
      validateStorageForm(
        form({ provider: "tencent_cos", region: "ap-guangzhou", bucket: "canvas" }),
      ).bucket,
    ).toContain("APPID");
  });

  test("R2 的 Account ID：必填，32 位小写十六进制；其他服务商不校验", () => {
    const r2 = (accountId: string) => form({ provider: "r2", region: "", accountId });
    expect(validateStorageForm(r2("")).accountId).toBe("请填写 Account ID");
    expect(validateStorageForm(r2("ABCDEF0123456789abcdef0123456789")).accountId).toContain(
      "32 位小写十六进制",
    );
    expect(validateStorageForm(r2("abc")).accountId).toContain("32 位小写十六进制");
    expect(validateStorageForm(r2(ACCOUNT)).accountId).toBeUndefined();
    expect(validateStorageForm(form({ accountId: "abc" })).accountId).toBeUndefined();
  });

  test("R2 不需要选地域，OSS 必须选", () => {
    expect(
      validateStorageForm(form({ provider: "r2", region: "", accountId: ACCOUNT })).region,
    ).toBeUndefined();
    expect(validateStorageForm(form({ region: "" })).region).toBe("请选择地域");
  });

  test("S3 填了自定义 endpoint 时可以不选地域", () => {
    expect(validateStorageForm(form({ provider: "s3", region: "" })).region).toBe("请选择地域");
    expect(
      validateStorageForm(form({ provider: "s3", region: "", endpoint: "minio.internal:9000" }))
        .region,
    ).toBeUndefined();
  });

  test("签名有效期：60 到 604800 秒的整数", () => {
    const ttl = (signedTtlSec: number) => validateStorageForm(form({ signedTtlSec })).signedTtlSec;
    expect(ttl(60)).toBeUndefined();
    expect(ttl(604800)).toBeUndefined();
    expect(ttl(59)).toBe("签名有效期需要在 1 分钟到 7 天之间");
    expect(ttl(604801)).toBe("签名有效期需要在 1 分钟到 7 天之间");
    expect(ttl(1.5)).toBe("签名有效期需要在 1 分钟到 7 天之间");
    expect(ttl(Number.NaN)).toBe("签名有效期需要在 1 分钟到 7 天之间");
  });

  test("公开访问：公开域名必填，且必须是 http(s):// 开头", () => {
    const pub = (publicBaseUrl: string) =>
      validateStorageForm(form({ access: "public", publicBaseUrl })).publicBaseUrl;
    expect(pub("")).toBe("请填写公开访问域名");
    expect(pub("cdn.example.com")).toBe("公开访问域名必须是 http:// 或 https:// 开头的地址");
    expect(pub("https://cdn.example.com")).toBeUndefined();
    expect(
      validateStorageForm(form({ access: "private", publicBaseUrl: "bad" })).publicBaseUrl,
    ).toBeUndefined();
  });

  test("路径前缀：只能含字母、数字、. _ - 和 /，路径段不能以 . 开头", () => {
    const prefix = (pathPrefix: string) => validateStorageForm(form({ pathPrefix })).pathPrefix;
    expect(prefix("")).toBeUndefined();
    expect(prefix("assets/2026")).toBeUndefined();
    expect(prefix("/assets/")).toBeUndefined();
    expect(prefix("资源")).toContain("路径前缀");
    expect(prefix("a b")).toContain("路径前缀");
    expect(prefix("a/.hidden")).toContain("路径前缀");
  });

  test("新建时 AccessKey ID 与 Secret 必填；编辑时不校验（凭证走替换）", () => {
    const created = validateStorageForm(form({ accessKeyId: " ", secretKey: "" }));
    expect(created.accessKeyId).toBe("请填写 AccessKey ID");
    expect(created.secretKey).toBe("请填写 Secret");
    const edited = validateStorageForm(form({ accessKeyId: "", secretKey: "" }), { editing: true });
    expect(edited.accessKeyId).toBeUndefined();
    expect(edited.secretKey).toBeUndefined();
  });

  test("编辑已锁定的存储：定位字段不再校验，名称等照常校验", () => {
    const errors = validateStorageForm(form({ bucket: "", region: "", name: "" }), {
      editing: true,
      locked: true,
    });
    expect(errors.bucket).toBeUndefined();
    expect(errors.region).toBeUndefined();
    expect(errors.name).toBe("请填写名称");
  });
});

import { describe, expect, test } from "bun:test";

import { corsRules, deriveEndpoint } from "@/utils/admin/storage-endpoint";

describe("deriveEndpoint（仅用于展示，真正以后端为准）", () => {
  test("阿里云 OSS 与腾讯云 COS 按地域推导", () => {
    expect(deriveEndpoint("aliyun_oss", { region: "cn-hangzhou" })).toBe(
      "oss-cn-hangzhou.aliyuncs.com",
    );
    expect(deriveEndpoint("tencent_cos", { region: "ap-guangzhou" })).toBe(
      "cos.ap-guangzhou.myqcloud.com",
    );
  });

  test("S3 按地域推导", () => {
    expect(deriveEndpoint("s3", { region: "us-east-1" })).toBe("s3.us-east-1.amazonaws.com");
  });

  test("R2 按 Account ID 推导，没填时用占位符提示要填什么", () => {
    const account = "0123456789abcdef0123456789abcdef";
    expect(deriveEndpoint("r2", { accountId: account })).toBe(
      `${account}.r2.cloudflarestorage.com`,
    );
    expect(deriveEndpoint("r2", {})).toBe("<account_id>.r2.cloudflarestorage.com");
  });

  test("没选地域时返回空串，不拼出半截地址", () => {
    expect(deriveEndpoint("aliyun_oss", { region: "" })).toBe("");
    expect(deriveEndpoint("s3", {})).toBe("");
  });

  test("首尾空白会被去掉", () => {
    expect(deriveEndpoint("aliyun_oss", { region: " cn-beijing " })).toBe(
      "oss-cn-beijing.aliyuncs.com",
    );
  });

  test("本地磁盘没有 endpoint", () => {
    expect(deriveEndpoint("local", { region: "x" })).toBe("");
  });
});

describe("corsRules", () => {
  test("POST Policy 直传要放行 POST、PUT、GET、HEAD", () => {
    const text = corsRules("post_policy", "https://canvas.example.com");
    expect(text).toContain("AllowedOrigin: https://canvas.example.com");
    expect(text).toContain("AllowedMethod: POST, PUT, GET, HEAD");
    expect(text).toContain("AllowedHeader: *");
    expect(text).toContain("ExposeHeader:  ETag");
  });

  test("预签名 PUT 直传不需要 POST", () => {
    const text = corsRules("presigned_put", "https://canvas.example.com");
    expect(text).toContain("AllowedMethod: PUT, GET, HEAD");
    expect(text).not.toContain("POST");
  });
});

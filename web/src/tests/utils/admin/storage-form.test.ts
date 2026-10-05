import { describe, expect, test } from "bun:test";

import type { StoragePreset, StorageView } from "@/api/admin/storage/type";
import {
  buildCreateBody,
  buildTestBody,
  buildUpdateBody,
  changeProvider,
  emptyStorageForm,
  storageFormFromView,
  type StorageFormState,
} from "@/utils/admin/storage-form";

const PRESETS: StoragePreset[] = [
  {
    provider: "aliyun_oss",
    name: "阿里云 OSS",
    direct_method: "post_policy",
    force_ssl: true,
    addressing: "virtual",
    regions: [
      { id: "cn-hangzhou", name: "华东1（杭州）" },
      { id: "cn-shanghai", name: "华东2（上海）" },
    ],
  },
  {
    provider: "s3",
    name: "AWS S3 / 兼容",
    direct_method: "post_policy",
    force_ssl: false,
    addressing: "auto",
    regions: [{ id: "us-east-1", name: "美国东部" }],
  },
  {
    provider: "r2",
    name: "Cloudflare R2",
    direct_method: "presigned_put",
    force_ssl: true,
    addressing: "path",
    regions: [],
  },
];

const ACCOUNT = "0123456789abcdef0123456789abcdef";

const filled = (patch: Partial<StorageFormState>): StorageFormState => ({
  ...emptyStorageForm(PRESETS),
  name: "  OSS 杭州  ",
  bucket: " video-canvas ",
  accessKeyId: " LTAIxxxx ",
  secretKey: "secret",
  ...patch,
});

const view = (patch: Partial<StorageView>): StorageView => ({
  id: 7,
  name: "OSS 杭州",
  provider: "aliyun_oss",
  builtin: false,
  account_id: "",
  endpoint: "oss-cn-hangzhou.aliyuncs.com",
  region: "cn-hangzhou",
  bucket: "video-canvas",
  path_prefix: "assets",
  addressing: "virtual",
  use_ssl: true,
  access_key_id: "LTAI5t******Qm8",
  secret_set: true,
  public_base_url: "",
  access: "private",
  signed_ttl_sec: 3600,
  direct_upload: false,
  direct_method: "post_policy",
  is_default: false,
  asset_count: 0,
  locked: false,
  check: null,
  version: 3,
  updated_at: "2026-10-03T00:00:00Z",
  created_at: "2026-10-03T00:00:00Z",
  ...patch,
});

describe("emptyStorageForm / changeProvider", () => {
  test("默认选第一个预设，地域取它的第一个，签名有效期 1 小时", () => {
    const form = emptyStorageForm(PRESETS);
    expect(form).toMatchObject({
      provider: "aliyun_oss",
      region: "cn-hangzhou",
      access: "private",
      signedTtlSec: 3600,
      directUpload: false,
      useSSL: true,
    });
  });

  test("没有预设（接口还没回来）时也能生成空表单", () => {
    expect(emptyStorageForm([])).toMatchObject({ provider: "aliyun_oss", region: "" });
  });

  test("切换服务商：地域重置为新预设的第一个，R2 没有地域，寻址跟着预设", () => {
    const base = filled({ region: "cn-shanghai", accountId: ACCOUNT, endpoint: "minio:9000" });
    const toS3 = changeProvider(base, "s3", PRESETS);
    expect(toS3).toMatchObject({ provider: "s3", region: "us-east-1", addressing: "auto" });
    expect(toS3.endpoint).toBe("");
    const toR2 = changeProvider(base, "r2", PRESETS);
    expect(toR2).toMatchObject({ provider: "r2", region: "", addressing: "path" });
  });

  test("切换服务商保留名称、桶、凭证这些通用字段", () => {
    const next = changeProvider(filled({}), "s3", PRESETS);
    expect(next.name).toBe("  OSS 杭州  ");
    expect(next.bucket).toBe(" video-canvas ");
    expect(next.accessKeyId).toBe(" LTAIxxxx ");
  });
});

describe("buildCreateBody", () => {
  test("OSS：去空白，不带 endpoint 与 account_id，公开域名为空", () => {
    const body = buildCreateBody(filled({ pathPrefix: " assets/ " }));
    expect(body).toEqual({
      name: "OSS 杭州",
      provider: "aliyun_oss",
      region: "cn-hangzhou",
      bucket: "video-canvas",
      path_prefix: "assets/",
      use_ssl: true,
      access_key_id: "LTAIxxxx",
      secret_key: "secret",
      public_base_url: "",
      signed_ttl_sec: 3600,
      direct_upload: false,
    });
  });

  test("R2：带 account_id，region 固定 auto，寻址固定 path，不带 endpoint", () => {
    const body = buildCreateBody(
      filled({
        provider: "r2",
        region: "cn-hangzhou",
        accountId: ` ${ACCOUNT} `,
        addressing: "auto",
      }),
    );
    expect(body.account_id).toBe(ACCOUNT);
    expect(body.region).toBe("auto");
    expect(body.addressing).toBe("path");
    expect(body.endpoint).toBeUndefined();
  });

  test("非 R2 不会带 account_id，哪怕表单里残留着", () => {
    const body = buildCreateBody(filled({ provider: "tencent_cos", accountId: ACCOUNT }));
    expect(body.account_id).toBeUndefined();
  });

  test("S3：只有填了自定义 endpoint 才带 endpoint，寻址与 use_ssl 取表单值", () => {
    const plain = buildCreateBody(
      filled({ provider: "s3", region: "us-east-1", addressing: "auto" }),
    );
    expect(plain.endpoint).toBeUndefined();
    expect(plain.addressing).toBe("auto");

    const custom = buildCreateBody(
      filled({
        provider: "s3",
        region: "us-east-1",
        endpoint: " minio.internal:9000 ",
        addressing: "path",
        useSSL: false,
      }),
    );
    expect(custom.endpoint).toBe("minio.internal:9000");
    expect(custom.addressing).toBe("path");
    expect(custom.use_ssl).toBe(false);
  });

  test("OSS / COS / R2 的 use_ssl 一律是 true，不受表单残留值影响", () => {
    expect(buildCreateBody(filled({ useSSL: false })).use_ssl).toBe(true);
    expect(buildCreateBody(filled({ provider: "r2", useSSL: false })).use_ssl).toBe(true);
  });

  test("公开访问时才带公开域名，私有时清空", () => {
    const pub = buildCreateBody(
      filled({ access: "public", publicBaseUrl: " https://cdn.example.com " }),
    );
    expect(pub.public_base_url).toBe("https://cdn.example.com");
    const priv = buildCreateBody(filled({ access: "private", publicBaseUrl: "https://cdn.x.com" }));
    expect(priv.public_base_url).toBe("");
  });

  test("直传开关与签名有效期原样带上", () => {
    const body = buildCreateBody(filled({ directUpload: true, signedTtlSec: 900 }));
    expect(body.direct_upload).toBe(true);
    expect(body.signed_ttl_sec).toBe(900);
  });
});

describe("buildTestBody", () => {
  test("连接与凭证同创建，但不含名称、有效期、直传开关", () => {
    const body = buildTestBody(filled({ directUpload: true }));
    expect(body.provider).toBe("aliyun_oss");
    expect(body.bucket).toBe("video-canvas");
    expect(body.secret_key).toBe("secret");
    expect("name" in body).toBe(false);
    expect("signed_ttl_sec" in body).toBe(false);
    expect("direct_upload" in body).toBe(false);
  });
});

describe("storageFormFromView / buildUpdateBody", () => {
  test("编辑表单取自视图，Secret 留空", () => {
    const form = storageFormFromView(
      view({ public_base_url: "https://cdn.x.com", access: "public" }),
    );
    expect(form).toMatchObject({
      name: "OSS 杭州",
      provider: "aliyun_oss",
      region: "cn-hangzhou",
      bucket: "video-canvas",
      pathPrefix: "assets",
      access: "public",
      publicBaseUrl: "https://cdn.x.com",
      signedTtlSec: 3600,
      secretKey: "",
    });
  });

  test("S3 的 endpoint 和按地域推导的一致时视为没填自定义 endpoint", () => {
    const derived = storageFormFromView(
      view({ provider: "s3", region: "us-east-1", endpoint: "s3.us-east-1.amazonaws.com" }),
    );
    expect(derived.endpoint).toBe("");
    const custom = storageFormFromView(
      view({
        provider: "s3",
        region: "us-east-1",
        endpoint: "minio.internal:9000",
        use_ssl: false,
      }),
    );
    expect(custom.endpoint).toBe("minio.internal:9000");
    expect(custom.useSSL).toBe(false);
  });

  test("更新请求体：整份表单加 version，不含服务商与凭证", () => {
    const body = buildUpdateBody(storageFormFromView(view({})), 3);
    expect(body).toEqual({
      version: 3,
      name: "OSS 杭州",
      account_id: "",
      region: "cn-hangzhou",
      endpoint: "",
      bucket: "video-canvas",
      path_prefix: "assets",
      addressing: "",
      use_ssl: true,
      public_base_url: "",
      signed_ttl_sec: 3600,
      direct_upload: false,
    });
    expect("provider" in body).toBe(false);
    expect("access_key_id" in body).toBe(false);
    expect("secret_key" in body).toBe(false);
  });

  test("R2 更新：带 account_id 与 auto，S3 更新带寻址方式", () => {
    const r2 = buildUpdateBody(
      storageFormFromView(
        view({ provider: "r2", account_id: ACCOUNT, region: "auto", addressing: "path" }),
      ),
      1,
    );
    expect(r2).toMatchObject({ account_id: ACCOUNT, region: "auto" });
    const s3 = buildUpdateBody(
      storageFormFromView(
        view({
          provider: "s3",
          region: "us-east-1",
          addressing: "path",
          endpoint: "s3.us-east-1.amazonaws.com",
        }),
      ),
      1,
    );
    expect(s3).toMatchObject({ addressing: "path", endpoint: "" });
  });
});

import type {
  CloudProvider,
  StorageAccess,
  StorageAddressing,
  StorageCreateRequest,
  StoragePreset,
  StorageTestRequest,
  StorageUpdateRequest,
  StorageView,
} from "@/api/admin-storage/type";

import { deriveEndpoint } from "./storage-endpoint";
import { isFieldLocked } from "./storage-rules";

/** 名称最长字符数，与后端一致 */
export const STORAGE_NAME_MAX = 64;
/** 签名有效期下限（秒）：1 分钟 */
export const STORAGE_TTL_MIN = 60;
/** 签名有效期上限（秒）：7 天，S3 V4 预签名的上限 */
export const STORAGE_TTL_MAX = 604800;
/** 签名有效期默认值（秒）：1 小时 */
export const STORAGE_TTL_DEFAULT = 3600;

/** 表单字段名，锁定判断与校验错误都按它索引 */
export type StorageFormField =
  | "name"
  | "provider"
  | "region"
  | "accountId"
  | "endpoint"
  | "bucket"
  | "pathPrefix"
  | "addressing"
  | "useSSL"
  | "accessKeyId"
  | "secretKey"
  | "access"
  | "publicBaseUrl"
  | "signedTtlSec"
  | "directUpload";

/** 存储抽屉里的表单状态；文本字段保持用户输入的原样，提交时才去空白 */
export type StorageFormState = {
  /** 显示名称 */
  name: string;
  /** 服务商 */
  provider: CloudProvider;
  /** 地域 ID；R2 没有地域，为空串 */
  region: string;
  /** Cloudflare Account ID，只有 R2 用 */
  accountId: string;
  /** 自定义 endpoint，只有 S3 的“高级”里用；空串表示按地域推导 */
  endpoint: string;
  /** 桶名 */
  bucket: string;
  /** 路径前缀 */
  pathPrefix: string;
  /** 寻址方式，只有 S3 可选 */
  addressing: StorageAddressing;
  /** 是否使用 HTTPS，只有 S3 自定义 endpoint 可以关 */
  useSSL: boolean;
  /** AccessKey ID；编辑时是脱敏值，仅展示，不会提交 */
  accessKeyId: string;
  /** Secret 明文，只写不读；编辑时恒为空串 */
  secretKey: string;
  /** 访问方式 */
  access: StorageAccess;
  /** 公开访问域名，访问方式为公开时才提交 */
  publicBaseUrl: string;
  /** 签名有效期（秒） */
  signedTtlSec: number;
  /** 是否允许浏览器直传 */
  directUpload: boolean;
};

/** 表单校验错误：字段名 → 错误文案，没有错误的字段不出现 */
export type StorageFormErrors = Partial<Record<StorageFormField, string>>;

const presetOf = (presets: StoragePreset[], provider: CloudProvider) =>
  presets.find((item) => item.provider === provider);

/**
 * 新建时的空表单：默认选预设里的第一个服务商和它的第一个地域
 * @param presets 预设列表；接口还没返回时传空数组
 */
export function emptyStorageForm(presets: StoragePreset[]): StorageFormState {
  const first = presets[0];
  return {
    name: "",
    provider: first?.provider ?? "aliyun_oss",
    region: first?.regions[0]?.id ?? "",
    accountId: "",
    endpoint: "",
    bucket: "",
    pathPrefix: "",
    addressing: first?.addressing ?? "auto",
    useSSL: true,
    accessKeyId: "",
    secretKey: "",
    access: "private",
    publicBaseUrl: "",
    signedTtlSec: STORAGE_TTL_DEFAULT,
    directUpload: false,
  };
}

/**
 * 切换服务商：地域、寻址、自定义 endpoint、Account ID 属于上一个服务商，一律重置；名称、桶、凭证等通用字段保留。
 * @param form 当前表单
 * @param provider 新的服务商
 * @param presets 预设列表
 */
export function changeProvider(
  form: StorageFormState,
  provider: CloudProvider,
  presets: StoragePreset[],
): StorageFormState {
  const preset = presetOf(presets, provider);
  return {
    ...form,
    provider,
    region: preset?.regions[0]?.id ?? "",
    addressing: preset?.addressing ?? "auto",
    endpoint: "",
    accountId: "",
    useSSL: true,
  };
}

/**
 * 编辑时的表单：取自存储视图；Secret 恒为空，凭证走“替换”。
 * S3 的 endpoint 与按地域推导的一致时，视为没有填自定义 endpoint。
 * @param view 存储视图（不能是内置本地磁盘）
 */
export function storageFormFromView(view: StorageView): StorageFormState {
  const provider = view.provider as CloudProvider;
  const customEndpoint =
    provider === "s3" &&
    view.endpoint !== "" &&
    view.endpoint !== deriveEndpoint("s3", { region: view.region });
  return {
    name: view.name,
    provider,
    region: provider === "r2" ? "" : view.region,
    accountId: view.account_id,
    endpoint: customEndpoint ? view.endpoint : "",
    bucket: view.bucket,
    pathPrefix: view.path_prefix,
    addressing: view.addressing,
    useSSL: view.use_ssl,
    accessKeyId: view.access_key_id,
    secretKey: "",
    access: view.access,
    publicBaseUrl: view.public_base_url,
    signedTtlSec: view.signed_ttl_sec,
    directUpload: view.direct_upload,
  };
}

/** 各服务商在请求里的差异：R2 带 Account ID、region 固定 auto、寻址 path；S3 才有自定义 endpoint 与寻址；其余由后端按地域推导 */
function connectionFields(form: StorageFormState) {
  const r2 = form.provider === "r2";
  const s3 = form.provider === "s3";
  const endpoint = form.endpoint.trim();
  return {
    account_id: r2 ? form.accountId.trim() : "",
    region: r2 ? "auto" : form.region.trim(),
    endpoint: s3 ? endpoint : "",
    addressing: r2 ? ("path" as const) : s3 ? form.addressing : undefined,
    use_ssl: s3 ? form.useSSL : true,
    public_base_url: form.access === "public" ? form.publicBaseUrl.trim() : "",
  };
}

/** 去掉值为空串的可选字段，请求体里只留有意义的键 */
const dropEmpty = <T extends Record<string, unknown>>(body: T, keys: (keyof T)[]): T => {
  const next = { ...body };
  for (const key of keys) {
    if (next[key] === "" || next[key] === undefined) delete next[key];
  }
  return next;
};

/**
 * 测试一份未保存配置的请求体：连接与凭证，不含名称、有效期、直传开关
 * @param form 表单
 */
export function buildTestBody(form: StorageFormState): StorageTestRequest {
  const conn = connectionFields(form);
  return dropEmpty(
    {
      provider: form.provider,
      account_id: conn.account_id,
      region: conn.region,
      endpoint: conn.endpoint,
      bucket: form.bucket.trim(),
      path_prefix: form.pathPrefix.trim(),
      addressing: conn.addressing,
      use_ssl: conn.use_ssl,
      access_key_id: form.accessKeyId.trim(),
      secret_key: form.secretKey.trim(),
      public_base_url: conn.public_base_url,
    } satisfies StorageTestRequest,
    ["account_id", "endpoint", "addressing"],
  );
}

/**
 * 新建存储的请求体：在测试请求体上加名称、签名有效期与直传开关
 * @param form 表单
 */
export function buildCreateBody(form: StorageFormState): StorageCreateRequest {
  return {
    name: form.name.trim(),
    ...buildTestBody(form),
    signed_ttl_sec: form.signedTtlSec,
    direct_upload: form.directUpload,
  };
}

/**
 * 更新存储的请求体：整份表单加 version；服务商、AccessKey 与 Secret 不能在这里改，所以不带
 * @param form 表单
 * @param version 打开表单时读到的版本号，乐观锁
 */
export function buildUpdateBody(form: StorageFormState, version: number): StorageUpdateRequest {
  const conn = connectionFields(form);
  return {
    version,
    name: form.name.trim(),
    account_id: conn.account_id,
    region: conn.region,
    endpoint: conn.endpoint,
    bucket: form.bucket.trim(),
    path_prefix: form.pathPrefix.trim(),
    addressing: conn.addressing ?? "",
    use_ssl: conn.use_ssl,
    public_base_url: conn.public_base_url,
    signed_ttl_sec: form.signedTtlSec,
    direct_upload: form.directUpload,
  };
}

// ---------------------------------------------------------------- 校验

/** 通用桶名：小写字母、数字、- 和 .，3 到 63 位，首尾是字母或数字 */
const BUCKET_RE = /^[a-z0-9][a-z0-9.-]{1,61}[a-z0-9]$/;
/** COS 桶名必须带 -<APPID>（10 位数字）后缀 */
const COS_BUCKET_RE = /^[a-z0-9][a-z0-9-]*-\d{10}$/;
/** R2 的 Account ID：32 位小写十六进制 */
const ACCOUNT_ID_RE = /^[0-9a-f]{32}$/;
/** 路径前缀里一段的合法字符 */
const PREFIX_SEGMENT_RE = /^[A-Za-z0-9._-]+$/;

/**
 * 桶名规则错误；没填返回空串（是否必填由整表校验决定）。给输入时的即时提示用。
 * @param provider 服务商
 * @param bucket 桶名
 */
export function bucketError(provider: CloudProvider, bucket: string): string {
  const value = bucket.trim();
  if (!value) return "";
  if (!BUCKET_RE.test(value)) return "桶名只能包含小写字母、数字、- 和 .，长度 3–63";
  if (provider === "tencent_cos" && !COS_BUCKET_RE.test(value)) {
    return "COS 桶名需要带 APPID 后缀，例如 canvas-1250000000";
  }
  return "";
}

/**
 * R2 的 Account ID 规则错误；没填返回空串。给输入时的即时提示用。
 * @param accountId Account ID
 */
export function accountIdError(accountId: string): string {
  const value = accountId.trim();
  if (!value) return "";
  return ACCOUNT_ID_RE.test(value) ? "" : "Account ID 应为 32 位小写十六进制";
}

/** 公开域名是否是 http(s) 地址 */
const isHttpUrl = (text: string) => {
  try {
    const url = new URL(text);
    return (url.protocol === "http:" || url.protocol === "https:") && url.host !== "";
  } catch {
    return false;
  }
};

/** 路径前缀是否合法：去掉首尾 / 后，每段只含字母、数字、. _ -，且不以 . 开头 */
const isValidPrefix = (prefix: string) => {
  const trimmed = prefix.trim().replace(/^\/+|\/+$/g, "");
  if (!trimmed) return true;
  return trimmed
    .split("/")
    .every((segment) => PREFIX_SEGMENT_RE.test(segment) && !segment.startsWith("."));
};

/**
 * 整份表单的前端校验：和后端规则一致，目的是在提交前就指出问题；后端仍会再校验一遍。
 * @param form 表单
 * @param opts editing 编辑已有存储（凭证走替换，不校验）；locked 已有素材引用（定位字段不会提交修改，不校验）
 * @returns 字段 → 错误文案；空对象表示通过
 */
export function validateStorageForm(
  form: StorageFormState,
  opts: { editing?: boolean; locked?: boolean } = {},
): StorageFormErrors {
  const ctx = { editing: !!opts.editing, locked: !!opts.locked };
  const errors: StorageFormErrors = {};
  const check = (field: StorageFormField, message: string) => {
    if (message && !isFieldLocked(field, ctx)) errors[field] = message;
  };

  const name = form.name.trim();
  if (!name) check("name", "请填写名称");
  else if ([...name].length > STORAGE_NAME_MAX) {
    check("name", `名称不能超过 ${STORAGE_NAME_MAX} 个字符`);
  }

  if (!form.bucket.trim()) check("bucket", "请填写 Bucket");
  else check("bucket", bucketError(form.provider, form.bucket));

  if (form.provider === "r2") {
    check(
      "accountId",
      form.accountId.trim() ? accountIdError(form.accountId) : "请填写 Account ID",
    );
  } else if (!form.region.trim() && !(form.provider === "s3" && form.endpoint.trim())) {
    check("region", "请选择地域");
  }

  check(
    "pathPrefix",
    isValidPrefix(form.pathPrefix)
      ? ""
      : "路径前缀只能包含字母、数字、. _ - 和 /，且路径段不能以 . 开头",
  );

  if (
    !Number.isInteger(form.signedTtlSec) ||
    form.signedTtlSec < STORAGE_TTL_MIN ||
    form.signedTtlSec > STORAGE_TTL_MAX
  ) {
    check("signedTtlSec", "签名有效期需要在 1 分钟到 7 天之间");
  }

  if (form.access === "public") {
    const url = form.publicBaseUrl.trim();
    if (!url) check("publicBaseUrl", "请填写公开访问域名");
    else if (!isHttpUrl(url))
      check("publicBaseUrl", "公开访问域名必须是 http:// 或 https:// 开头的地址");
  }

  if (!opts.editing) {
    if (!form.accessKeyId.trim()) check("accessKeyId", "请填写 AccessKey ID");
    if (!form.secretKey.trim()) check("secretKey", "请填写 Secret");
  }
  return errors;
}

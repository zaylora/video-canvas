/**
 * 存储配置管理接口的类型，契约见 docs/design/存储配置设计/存储配置设计.md 第 8.3 节。
 * 与 admin-ai 一样直接沿用后端 snake_case 字段与数字 id，不做映射。
 */

/** 服务商：local 是内置本地磁盘，其余是后台可新建的对象存储 */
export type StorageProvider = "local" | "aliyun_oss" | "tencent_cos" | "s3" | "r2";

/** 后台可新建的对象存储服务商（不含内置本地磁盘） */
export type CloudProvider = Exclude<StorageProvider, "local">;

/** 寻址方式：auto 交给客户端自动判断，virtual 是 bucket.endpoint，path 是 endpoint/bucket */
export type StorageAddressing = "auto" | "virtual" | "path";

/** 浏览器直传方式：POST 表单（桶侧限制大小）或预签名 PUT（R2 只支持这种） */
export type DirectMethod = "post_policy" | "presigned_put";

/** 访问方式：private 走签名地址，public 走公开域名或 CDN */
export type StorageAccess = "private" | "public";

/** 最近一次连接测试的结果 */
export interface StorageCheck {
  /** 是否通过 */
  ok: boolean;
  /** 测试时间（ISO 时间串） */
  at: string;
  /** 失败原因；通过时为空串 */
  error: string;
}

/** 管理端的存储视图。Secret 只写不读，只告诉前端有没有设置 */
export interface StorageView {
  /** 存储 ID */
  id: number;
  /** 显示名称 */
  name: string;
  /** 服务商 */
  provider: StorageProvider;
  /** 是否内置（本地磁盘，配置来自 config.yaml，页面只读） */
  builtin: boolean;
  /** 本地目录；只有内置本地存储有 */
  local_dir?: string;
  /** Cloudflare Account ID；只有 R2 有，其余为空串 */
  account_id: string;
  /** 实际使用的 endpoint（不含协议头） */
  endpoint: string;
  /** 地域；R2 固定为 auto */
  region: string;
  /** 桶名 */
  bucket: string;
  /** 路径前缀，不带首尾斜杠；空串表示没有 */
  path_prefix: string;
  /** 寻址方式 */
  addressing: StorageAddressing;
  /** 是否使用 HTTPS */
  use_ssl: boolean;
  /** AccessKey ID，已脱敏 */
  access_key_id: string;
  /** Secret 是否已设置 */
  secret_set: boolean;
  /** 公开访问域名；空串表示私有桶走签名 */
  public_base_url: string;
  /** 访问方式 */
  access: StorageAccess;
  /** 私有桶签名地址的有效期（秒） */
  signed_ttl_sec: number;
  /** 是否允许浏览器直传 */
  direct_upload: boolean;
  /** 直传方式，跟着服务商预设走；本地存储为空串 */
  direct_method: DirectMethod | "";
  /** 是否为默认存储（新上传和新生成的素材写到这里） */
  is_default: boolean;
  /** 已有多少素材存放在这套存储里 */
  asset_count: number;
  /** 已有素材引用：定位字段不可修改 */
  locked: boolean;
  /** 最近一次连接测试；没测过为 null */
  check: StorageCheck | null;
  /** 乐观锁版本号，更新时必须带上 */
  version: number;
  /** 最近更新时间（ISO 时间串） */
  updated_at: string;
  /** 创建时间（ISO 时间串） */
  created_at: string;
}

/** 预设里的一个地域 */
export interface StoragePresetRegion {
  /** 地域 ID，如 cn-hangzhou */
  id: string;
  /** 地域中文名 */
  name: string;
}

/** 服务商预设：服务商名称、地域列表、直传方式都以接口为准，前端不写死 */
export interface StoragePreset {
  /** 服务商 */
  provider: CloudProvider;
  /** 服务商显示名 */
  name: string;
  /** 浏览器直传方式 */
  direct_method: DirectMethod;
  /** 是否强制 HTTPS */
  force_ssl: boolean;
  /** 固定的寻址方式；auto 表示不固定，可由管理员选择 */
  addressing: StorageAddressing;
  /** 可选地域；R2 没有地域，为空数组 */
  regions: StoragePresetRegion[];
}

/** 探针里一次失败的可读说明 */
export interface ProbeIssue {
  /** 发生了什么 */
  title: string;
  /** 怎么处理 */
  hint: string;
  /** 云厂商的原始错误，供展开查看 */
  raw: string;
}

/** 探针的一个步骤 */
export interface ProbeStep {
  /** 步骤序号，从 1 开始 */
  index: number;
  /** 步骤名称 */
  name: string;
  /** 是否通过；ok=false 且 skipped=true 表示前面的步骤失败所以没执行 */
  ok: boolean;
  /** 是否跳过；ok=true 且 skipped=true 表示按配置主动跳过 */
  skipped: boolean;
  /** 耗时（毫秒） */
  duration_ms: number;
  /** 失败说明；通过或跳过时没有 */
  issue?: ProbeIssue;
}

/** 一次连接测试（探针）的结果 */
export interface ProbeResult {
  /** 是否全部通过 */
  ok: boolean;
  /** 各步骤，按执行顺序 */
  steps: ProbeStep[];
}

/** 测试一份未保存配置的请求体（也是新建请求体的连接与凭证部分） */
export interface StorageTestRequest {
  /** 服务商 */
  provider: CloudProvider;
  /** Cloudflare Account ID；只有 R2 需要 */
  account_id?: string;
  /** 地域；R2 固定 auto */
  region?: string;
  /** 自定义 endpoint；只有 S3 需要，其余服务商由后端按地域推导 */
  endpoint?: string;
  /** 桶名 */
  bucket: string;
  /** 路径前缀 */
  path_prefix?: string;
  /** 寻址方式；只有 S3 可选 */
  addressing?: StorageAddressing;
  /** 是否使用 HTTPS；OSS / COS / R2 强制开启 */
  use_ssl?: boolean;
  /** AccessKey ID */
  access_key_id: string;
  /** Secret，只写不读 */
  secret_key: string;
  /** 公开访问域名；留空表示私有桶 */
  public_base_url?: string;
}

/** 新建存储的请求体 */
export interface StorageCreateRequest extends StorageTestRequest {
  /** 显示名称，1 到 64 个字符 */
  name: string;
  /** 签名有效期（秒），范围 60 到 604800；0 表示用默认的 3600 */
  signed_ttl_sec?: number;
  /** 是否允许浏览器直传 */
  direct_upload?: boolean;
}

/** 更新存储的请求体：整份表单提交。服务商、AccessKey 与 Secret 不能在这里改 */
export interface StorageUpdateRequest {
  /** 读到的版本号，乐观锁；冲突时后端返回 409 / 51009 */
  version: number;
  /** 显示名称 */
  name: string;
  /** Cloudflare Account ID */
  account_id: string;
  /** 地域 */
  region: string;
  /** 自定义 endpoint */
  endpoint: string;
  /** 桶名 */
  bucket: string;
  /** 路径前缀 */
  path_prefix: string;
  /** 寻址方式 */
  addressing: StorageAddressing | "";
  /** 是否使用 HTTPS */
  use_ssl: boolean;
  /** 公开访问域名；空串表示私有桶 */
  public_base_url: string;
  /** 签名有效期（秒） */
  signed_ttl_sec: number;
  /** 是否允许浏览器直传 */
  direct_upload: boolean;
}

/** 替换凭证的请求体：AccessKey ID 与 Secret 必须一起换 */
export interface StorageSecretRequest {
  /** 新的 AccessKey ID */
  access_key_id: string;
  /** 新的 Secret，只写不读 */
  secret_key: string;
}

/** 删除预检的结果 */
export interface StorageDeleteCheck {
  /** 是否可以删除 */
  deletable: boolean;
  /** 不可删除的原因；可删除时为空串 */
  reason: string;
  /** 引用这套存储的素材数 */
  asset_count: number;
  /** 未过期且未完成的上传意图数 */
  pending_uploads: number;
}

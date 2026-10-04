/**
 * 图片处理服务管理接口的类型，契约见 docs/design/画布素材加载设计/图片处理服务接口契约.md 第 2 节。
 * 与 admin-ai、admin-storage 一样直接沿用后端 snake_case 字段与数字 id，不做映射。
 */

/** 处理服务厂商 */
export type ProcessorVendor = "cloudflare" | "tencent_ci" | "aliyun_oss_img";

/** 处理服务状态：草稿、已发布（线上生效）、已停用 */
export type ProcessorStatus = "draft" | "published" | "disabled";

/** 处理服务只能绑定的存储服务商 */
export type ProcessorStorageProvider = "r2" | "tencent_cos" | "aliyun_oss";

/** 处理服务的参数配置；不同厂商用到的字段不同 */
export interface ProcessorConfig {
  /** 访问域名，只写 host，不带协议 */
  domain: string;
  /** 缩略图 / 封面长边（px），16 到 2000 */
  width: number;
  /** 输出格式，取值见预设 formats */
  format: string;
  /** 视频封面取帧时间（秒），不小于 0；不支持封面的厂商忽略 */
  time_sec: number;
  /** 输出质量 1 到 100；仅 cloudflare */
  quality?: number;
  /** 处理失败时回退原图；仅 cloudflare */
  on_error_redirect?: boolean;
  /** 已开通数据万象“媒体处理”，未开通时没有视频封面；仅 tencent_ci */
  media_enabled?: boolean;
}

/** 校验结果的状态：通过、提示（不挡发布）、失败（挡发布） */
export type CheckItemStatus = "ok" | "warn" | "fail";

/** 校验的一项 */
export interface CheckItem {
  /** 稳定标识，如 binding / domain / sign / trial_image / trial_video */
  key: string;
  /** 中文名 */
  label: string;
  /** 结果 */
  status: CheckItemStatus;
  /** 说明 */
  message: string;
}

/** 图片试跑结果 */
export interface TrialImage {
  /** 缩略图体积（字节） */
  bytes: number;
  /** 耗时（毫秒） */
  ms: number;
  /** 原图体积（字节） */
  source_bytes: number;
}

/** 视频封面试跑结果 */
export interface TrialVideo {
  /** 封面体积（字节） */
  bytes: number;
  /** 耗时（毫秒） */
  ms: number;
}

/** 最近一次校验与试跑 */
export interface ProcessorCheck {
  /** 没有 fail 即为 true（warn 不挡发布） */
  ok: boolean;
  /** 这次校验针对的处理服务 version */
  version: number;
  /** 校验时间（ISO 时间串） */
  checked_at: string;
  /** 逐项结果 */
  checks: CheckItem[];
  /** 试跑：用该存储里的真实素材各取一次 */
  trial: {
    /** 图片试跑；该存储没有图片素材时为 null */
    image: TrialImage | null;
    /** 视频封面试跑；没有视频素材或厂商不支持时为 null */
    video: TrialVideo | null;
  };
}

/** 管理端的处理服务视图 */
export interface ProcessorView {
  /** 处理服务 ID */
  id: number;
  /** 显示名称 */
  name: string;
  /** 厂商 */
  vendor: ProcessorVendor;
  /** 绑定的存储 ID，创建后不可改 */
  storage_id: number;
  /** 绑定的存储名称 */
  storage_name: string;
  /** 绑定的存储服务商：local / aliyun_oss / tencent_cos / s3 / r2 */
  storage_provider: string;
  /** 状态 */
  status: ProcessorStatus;
  /** 工作配置（草稿）；已发布且没改过时与 published_config 相同 */
  config: ProcessorConfig;
  /** 线上正在用的配置；从未发布为 null */
  published_config: ProcessorConfig | null;
  /** 已发布且 config 与 published_config 不同，说明有未发布草稿 */
  has_draft: boolean;
  /** 线上版本号；从未发布为 0 */
  published_version: number;
  /** 可回滚到的上一个版本号；没有为 null */
  previous_version: number | null;
  /** 乐观锁版本，每次保存草稿 +1 */
  version: number;
  /** 最近一次校验与试跑；其 version 落后于 version 说明保存后还没重新校验 */
  check: ProcessorCheck | null;
  /** 最近更新时间（ISO 时间串） */
  updated_at: string;
  /** 创建时间（ISO 时间串） */
  created_at: string;
}

/** 厂商预设：名称、可绑定的存储、能力、可选格式都以接口为准 */
export interface ProcessorPreset {
  /** 厂商 */
  vendor: ProcessorVendor;
  /** 中文厂商名 */
  name: string;
  /** 只能绑定这种服务商的存储 */
  storage_provider: ProcessorStorageProvider;
  /** 存储必须已设置公开域名（cloudflare 为 true） */
  requires_public_base: boolean;
  /** 是否支持视频封面 */
  supports_poster: boolean;
  /** 可选输出格式，第一个是默认 */
  formats: string[];
  /** 默认参数 */
  default_config: ProcessorConfig;
}

/** 新建处理服务（草稿）的请求体 */
export interface ProcessorCreateRequest {
  /** 显示名称 */
  name: string;
  /** 厂商 */
  vendor: ProcessorVendor;
  /** 绑定的存储 ID */
  storage_id: number;
  /** 参数 */
  config: ProcessorConfig;
}

/** 保存草稿的请求体：整份提交，不改 vendor 与 storage_id */
export interface ProcessorUpdateRequest {
  /** 读到的版本号，乐观锁；冲突时后端返回 409 / 52004 */
  version: number;
  /** 显示名称 */
  name: string;
  /** 参数 */
  config: ProcessorConfig;
}

/** 发布的请求体 */
export interface ProcessorPublishRequest {
  /** 要发布的草稿版本号，必须等于最近一次校验针对的 version */
  version: number;
}

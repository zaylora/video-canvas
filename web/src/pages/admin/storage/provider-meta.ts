import {
  Cloud,
  CloudCog,
  CloudLightning,
  Database,
  HardDrive,
  type LucideIcon,
} from "lucide-react";

import type { StorageProvider } from "@/api/admin-storage/type.d";

/** 页面里每个服务商的展示信息：图标与填写提示。服务商名称、地域、直传方式不在这里，一律来自预设接口 */
export type ProviderMeta = {
  /** 图标 */
  icon: LucideIcon;
  /** 服务商卡片里的一句话说明 */
  hint: string;
  /** 桶名输入框下的提示 */
  bucketHint: string;
  /** 桶名输入框的占位 */
  bucketPlaceholder: string;
  /** AccessKey ID 在该服务商控制台里的叫法 */
  keyLabel: string;
  /** Secret 在该服务商控制台里的叫法 */
  secretLabel: string;
  /** 凭证区的权限建议 */
  credentialHint: string;
  /** 选公开访问时的额外提示；没有则为空串 */
  publicHint: string;
};

const GENERIC_CREDENTIAL_HINT = "建议用只授予该桶读写权限的子账号密钥（RAM / CAM / IAM）。";

/** 各服务商的展示信息 */
export const PROVIDER_META: Record<StorageProvider, ProviderMeta> = {
  local: {
    icon: HardDrive,
    hint: "内置，配置来自 config.yaml",
    bucketHint: "",
    bucketPlaceholder: "",
    keyLabel: "",
    secretLabel: "",
    credentialHint: "",
    publicHint: "",
  },
  aliyun_oss: {
    icon: Cloud,
    hint: "S3 兼容协议",
    bucketHint: "OSS 控制台 → Bucket 列表里的名称",
    bucketPlaceholder: "video-canvas-prod",
    keyLabel: "AccessKey ID",
    secretLabel: "AccessKey Secret",
    credentialHint: GENERIC_CREDENTIAL_HINT,
    publicHint: "",
  },
  tencent_cos: {
    icon: CloudCog,
    hint: "S3 兼容协议",
    bucketHint: "必须带 APPID 后缀，例如 canvas-1250000000",
    bucketPlaceholder: "canvas-1250000000",
    keyLabel: "SecretId",
    secretLabel: "SecretKey",
    credentialHint: GENERIC_CREDENTIAL_HINT,
    publicHint: "",
  },
  s3: {
    icon: Database,
    hint: "含 MinIO 等兼容服务",
    bucketHint: "MinIO 等兼容服务请在“高级”里填自定义 Endpoint",
    bucketPlaceholder: "video-canvas-prod",
    keyLabel: "AccessKey ID",
    secretLabel: "AccessKey Secret",
    credentialHint: GENERIC_CREDENTIAL_HINT,
    publicHint: "",
  },
  r2: {
    icon: CloudLightning,
    hint: "S3 兼容 · 无出口流量费",
    bucketHint: "R2 控制台 → 存储桶名称",
    bucketPlaceholder: "video-canvas-prod",
    keyLabel: "Access Key ID（R2 API 令牌）",
    secretLabel: "Secret Access Key",
    credentialHint: "在 R2 → 管理 API 令牌中创建，权限选“对象读和写”，并限定到该桶。",
    publicHint: "需先在 R2 控制台为桶开启 r2.dev 子域，或绑定自定义域名。",
  },
};

/** 寻址方式的下拉文案 */
export const ADDRESSING_OPTIONS = [
  { value: "auto", label: "自动" },
  { value: "virtual", label: "虚拟主机（bucket.endpoint）" },
  { value: "path", label: "路径（endpoint/bucket，MinIO 常用）" },
] as const;

/** 直传方式的说明文案，跟着服务商预设走，管理员不用选 */
export const DIRECT_METHOD_LABEL = {
  post_policy: "POST Policy 表单（桶侧限制大小）",
  presigned_put: "预签名 PUT（签入 Content-Length，登记时复核大小）",
} as const;

import type { DirectMethod, StorageProvider } from "@/api/admin-storage/type";

/**
 * 按服务商规则推导 endpoint，只用于表单里的展示：让管理员看到自己填的地域会连到哪里。
 * 真正使用的 endpoint 由后端按同样规则推导并保存，以后端为准。
 * @param provider 服务商
 * @param input 地域（OSS / COS / S3）或 Account ID（R2）
 * @returns endpoint；信息不足（还没选地域）时返回空串，R2 没填 Account ID 时用占位符提示
 */
export function deriveEndpoint(
  provider: StorageProvider,
  input: { region?: string; accountId?: string },
): string {
  const region = (input.region ?? "").trim();
  switch (provider) {
    case "aliyun_oss":
      return region ? `oss-${region}.aliyuncs.com` : "";
    case "tencent_cos":
      return region ? `cos.${region}.myqcloud.com` : "";
    case "s3":
      return region ? `s3.${region}.amazonaws.com` : "";
    case "r2":
      return `${(input.accountId ?? "").trim() || "<account_id>"}.r2.cloudflarestorage.com`;
    default:
      return "";
  }
}

/**
 * 浏览器直传需要在桶上配置的 CORS 规则文本，管理员对照云控制台的跨域设置填写。
 * POST Policy 要放行 POST；预签名 PUT 只用 PUT。
 * @param method 直传方式，来自服务商预设
 * @param origin 允许的来源，通常是当前页面的 origin
 * @returns 可直接复制的多行文本
 */
export function corsRules(method: DirectMethod, origin: string): string {
  const methods = method === "presigned_put" ? "PUT, GET, HEAD" : "POST, PUT, GET, HEAD";
  return [
    `AllowedOrigin: ${origin}`,
    `AllowedMethod: ${methods}`,
    "AllowedHeader: *",
    "ExposeHeader:  ETag",
    "MaxAgeSeconds: 600",
  ].join("\n");
}

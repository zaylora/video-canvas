/**
 * 浏览器直传的纯逻辑：决定走哪条路、构造直传请求、判断直传结果。
 * 不碰网络与全局状态（fetch 由调用方注入），方便单测；真正的请求编排在 api/asset。
 */
import type { UploadIntent, UploadIntentBody } from "@/api/asset/type";

/** 交给 fetch 的直传请求 */
export type DirectRequest = {
  /** 直传地址 */
  url: string;
  /** fetch 的参数；不带登录头，直传目标是对象存储而不是我们的后端 */
  init: { method: "POST" | "PUT"; headers?: Record<string, string>; body: FormData | File };
};

/** 上传方案：走后端中转，或浏览器直传 */
export type UploadPlan =
  | {
      /** 走原有 POST /assets 中转 */
      kind: "proxy";
    }
  | {
      /** 浏览器直传，成功后要调登记接口 */
      kind: "direct";
      /** 直传意图 ID */
      intentId: number;
      /** 直传请求 */
      request: DirectRequest;
    };

/** 浏览器不允许脚本设置的请求头：Content-Length 由浏览器按 body 计算，签名时带上的这个头要去掉 */
const FORBIDDEN_HEADERS = new Set(["content-length"]);

/**
 * 申请上传方式的请求体。文件类型为空（后端要求类型在白名单内）或文件为空时返回 null，调用方直接走中转。
 * @param file 待上传的文件
 */
export function buildIntentBody(file: File): UploadIntentBody | null {
  if (!file.type || file.size <= 0) return null;
  return { file_name: file.name, size: file.size, mime_type: file.type };
}

/**
 * 按申请结果构造直传请求；信息不完整（缺地址、方法不认识）时返回 null。
 * POST 表单里 fields 必须全部排在前面，file 字段必须是最后一个，否则对象存储会拒绝；
 * fields 里就算出现名为 file 的键也忽略，保证文件只有一份。
 * @param intent 申请结果
 * @param file 待上传的文件
 */
export function buildDirectRequest(intent: UploadIntent, file: File): DirectRequest | null {
  const url = intent.url;
  if (!url) return null;
  if (intent.method === "post") {
    const body = new FormData();
    for (const [key, value] of Object.entries(intent.fields ?? {})) {
      if (key.toLowerCase() !== "file") body.append(key, value);
    }
    body.append("file", file);
    return { url, init: { method: "POST", body } };
  }
  if (intent.method === "put") {
    const headers = Object.fromEntries(
      Object.entries(intent.headers ?? {}).filter(
        ([key]) => !FORBIDDEN_HEADERS.has(key.toLowerCase()),
      ),
    );
    return { url, init: { method: "PUT", headers, body: file } };
  }
  return null;
}

/**
 * 决定这次上传走哪条路：只有 mode=direct 且信息完整才直传，
 * 其余情况（没有响应、mode=proxy、信息缺失）一律走后端中转，不抛错。
 * @param intent 申请结果；申请接口失败时为空
 * @param file 待上传的文件
 */
export function planUpload(intent: UploadIntent | null | undefined, file: File): UploadPlan {
  if (!intent || intent.mode !== "direct" || !intent.intent_id) return { kind: "proxy" };
  const request = buildDirectRequest(intent, file);
  return request ? { kind: "direct", intentId: intent.intent_id, request } : { kind: "proxy" };
}

/**
 * 发出直传请求并判断结果：2xx 算成功（S3 的 POST Policy 成功返回 204）；
 * 非 2xx 或 fetch 抛错（CORS 未配置、断网）都算失败，不向外抛，由调用方降级到中转。
 * 不带 cookie 与登录头。
 * @param request 直传请求
 * @param fetchImpl fetch 实现，默认用全局 fetch；测试时注入假的
 * @returns 是否直传成功
 */
export async function sendDirect(
  request: DirectRequest,
  fetchImpl: typeof fetch = fetch,
): Promise<boolean> {
  try {
    const response = await fetchImpl(request.url, { ...request.init, credentials: "omit" });
    return response.ok;
  } catch {
    return false;
  }
}

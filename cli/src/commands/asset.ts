// 本地素材上传：把机器上的图片 / 视频 / 音频传到账号素材库。
// 上传路径的选择与网页画布完全一致：先申请直传凭证，能直传就直传，否则后端中转。
import fs from "node:fs";
import path from "node:path";

import { APIError, errorMessage, request, sendDirect, uploadFile } from "../client.ts";
import type { Args, Ctx } from "../types.ts";
import { baseOf, emit, requireToken } from "./shared.ts";

/** 上传返回的素材视图：CLI 只用 id / kind / 大小，其余字段原样放过。 */
export interface AssetView {
  id?: number;
  kind?: string;
  byte_size?: number;
  file_name?: string;
  [key: string]: unknown;
}

/** 申请直传的响应：mode=direct 时带凭证，mode=proxy 时走后端中转。 */
interface UploadIntent {
  mode?: string;
  intent_id?: number;
  method?: string;
  url?: string;
  fields?: Record<string, string>;
  headers?: Record<string, string>;
  [key: string]: unknown;
}

/** 客户端提前挡的大小上限，对齐后端 defaultMaxUpload（200MB）；后端改配置时以服务端为准。 */
const CLIENT_MAX_UPLOAD = 200 * 1024 * 1024;

/** 扩展名 → MIME：后端 assetMimeExt 白名单（按内容嗅探才是最终判定，这里只用来申请凭证）。 */
const MIME_BY_EXT: Record<string, string> = {
  ".png": "image/png",
  ".jpg": "image/jpeg",
  ".jpeg": "image/jpeg",
  ".gif": "image/gif",
  ".webp": "image/webp",
  ".mp4": "video/mp4",
  ".mov": "video/quicktime",
  ".webm": "video/webm",
  ".mp3": "audio/mpeg",
  ".wav": "audio/wav",
  ".m4a": "audio/mp4",
  ".ogg": "audio/ogg",
  ".flac": "audio/flac",
  ".aac": "audio/aac",
};

/** 直传凭证齐全才走直传；缺地址、方法不认识都按中转处理，与网页 planUpload 一致。 */
function usableIntent(intent: UploadIntent | null): intent is UploadIntent & {
  intent_id: number;
  url: string;
  method: "post" | "put";
} {
  if (!intent || intent.mode !== "direct" || !intent.intent_id || !intent.url) return false;
  return intent.method === "post" || intent.method === "put";
}

/**
 * asset upload <文件路径>
 *
 * 路径与网页画布上传一致：
 *   1. 申请 upload-intent（file_name / size / mime_type）
 *   2. mode=direct 且凭证齐全 → 直传对象存储（不带登录头）→ 成功后 complete 登记
 *   3. 直传失败（网络 / CORS / 非 2xx）、mode=proxy、申请失败 → 降级 POST /assets 中转
 *   4. complete 登记失败是真错误（类型不合法 / 超限），不降级
 *
 * 不做交互确认：上传是新增素材、不改画布，也不花钱；批量导入场景需要能脚本化。
 */
export async function assetUpload(ctx: Ctx, args: Args): Promise<void> {
  const file = args.positional[0];
  if (typeof file !== "string" || !file || args.positional.length !== 1) {
    throw new APIError(-1, "用法：asset upload <文件路径>（一次传一个文件）", 2);
  }

  let stat: fs.Stats;
  try {
    stat = fs.statSync(file);
  } catch (err) {
    throw new APIError(-1, `读取 ${file} 失败：${errorMessage(err)}`, 2);
  }
  if (!stat.isFile()) throw new APIError(-1, `${file} 不是普通文件`, 2);
  if (stat.size <= 0) throw new APIError(-1, `${file} 是空文件`, 2);
  if (stat.size > CLIENT_MAX_UPLOAD) {
    throw new APIError(
      -1,
      `${file} 超过 200MB 上限（${(stat.size / 1024 / 1024).toFixed(1)}MB），后端会拒收`,
      2,
    );
  }

  const token = requireToken(ctx);
  const base = baseOf(ctx);
  const filePath = path.resolve(file);
  const fileName = path.basename(file);
  // 按扩展名推断类型。认不出来（或不在后端白名单）就不申请直传——后端会按白名单拒掉，
  // 直接走中转让服务端按内容嗅探判定。网页端也是 file.type 为空就不申请。
  const mimeType = MIME_BY_EXT[path.extname(file).toLowerCase()];

  // 1. 申请上传方式：申请失败不算上传失败（旧版本后端 / 存储暂不可用），按中转处理
  let intent: UploadIntent | null = null;
  if (mimeType) {
    try {
      intent = await request<UploadIntent>({
        base,
        token,
        method: "POST",
        path: "/assets/upload-intents",
        body: { file_name: fileName, size: stat.size, mime_type: mimeType },
        fetchImpl: ctx.fetchImpl,
      });
    } catch {
      intent = null;
    }
  }

  // 2. 能直传就直传；失败降级中转（与网页一致，降级只提示一次到 stderr）
  if (usableIntent(intent)) {
    const ok = await sendDirect({
      url: intent.url,
      method: intent.method.toUpperCase() as "POST" | "PUT",
      headers: intent.headers,
      fields: intent.fields,
      file: filePath,
      fileName,
      contentType: mimeType,
      fetchImpl: ctx.fetchImpl,
    });
    if (ok) {
      // 3. 登记：服务端复核大小与内容类型，失败是真错误，不降级
      const view = await request<AssetView>({
        base,
        token,
        method: "POST",
        path: `/assets/upload-intents/${intent.intent_id}/complete`,
        fetchImpl: ctx.fetchImpl,
      });
      emit(ctx, view, humanSummary(view, fileName, stat.size));
      return;
    }
    ctx.stderr("直传失败，已改用服务器中转");
  }

  // 4. 后端中转
  const view = await uploadFile<AssetView>({
    base,
    token,
    path: "/assets",
    file: filePath,
    fileName,
    contentType: mimeType,
    fetchImpl: ctx.fetchImpl,
  });
  emit(ctx, view, humanSummary(view, fileName, stat.size));
}

function humanSummary(view: AssetView, fileName: string, fallbackSize: number): string {
  const size = view.byte_size ?? fallbackSize;
  return `已上传 ${view.file_name || fileName}（素材 id ${view.id ?? "?"}，${view.kind || "未知类型"}，${(size / 1024 / 1024).toFixed(1)}MB）`;
}

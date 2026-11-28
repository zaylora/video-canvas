import { randomUUID } from "node:crypto";
import fs from "node:fs";
import path from "node:path";
import { Readable, Transform } from "node:stream";
import { pipeline } from "node:stream/promises";

import { APIError } from "../client.ts";
import type { Args, Ctx } from "../types.ts";
import { stringFlag } from "./args.ts";
import { canvasPath } from "./collaboration.ts";
import { baseOf, emit, requireToken } from "./shared.ts";

const usage = "用法：asset download <素材ID> [--canvas <画布>] -o <文件>";

export async function assetDownload(ctx: Ctx, args: Args): Promise<void> {
  const [assetId] = args.positional;
  if (
    !assetId ||
    args.positional.length !== 1 ||
    !/^[1-9]\d{0,19}$/.test(assetId) ||
    BigInt(assetId) > 18_446_744_073_709_551_615n
  ) {
    throw new APIError(-1, usage, 2);
  }
  const output = stringFlag(args.flags, "output", usage);
  if (!output || output === "-") throw new APIError(-1, usage, 2);
  const canvas =
    args.flags.canvas === undefined ? undefined : stringFlag(args.flags, "canvas", usage);
  const result = await downloadAsset(ctx, assetId, path.resolve(output), canvas);
  emit(ctx, result, `已下载素材 ${assetId} 到 ${result.path}`);
}

export async function downloadAsset(
  ctx: Ctx,
  assetId: string,
  destination: string,
  canvas?: string,
  maxBytes = Number.POSITIVE_INFINITY,
) {
  if (fs.existsSync(destination)) throw new APIError(-1, "目标文件已存在", 409);
  const route = canvas ? `${canvasPath(canvas)}/media/${assetId}` : `/assets/${assetId}/download`;
  const token = requireToken(ctx);
  const response = await ctx
    .fetchImpl(`${baseOf(ctx)}${route}`, {
      headers: { Authorization: `Bearer ${token}` },
      redirect: "error",
      signal: AbortSignal.timeout(10 * 60 * 1_000),
    })
    .catch(() => {
      throw new APIError(-1, "无法连接站点下载素材", 502);
    });
  const mime = response.headers.get("content-type")?.split(";")[0] ?? "";
  if (response.status !== 200 || !/^(image|video|audio)\//i.test(mime) || !response.body) {
    await response.body?.cancel();
    throw new APIError(-1, `素材不可下载（HTTP ${response.status}）`, response.status);
  }
  const temporary = path.join(path.dirname(destination), `.${randomUUID()}.part`);
  const handle = await fs.promises.open(temporary, "wx", 0o600).catch(async () => {
    await response.body?.cancel();
    throw new APIError(-1, "无法创建临时下载文件", 500);
  });
  let byteSize = 0;
  const meter = new Transform({
    transform(chunk: Buffer, _encoding, done) {
      byteSize += chunk.length;
      if (byteSize > maxBytes) done(new APIError(-1, "素材超过本地加工大小上限", 413));
      else done(null, chunk);
    },
  });
  try {
    await pipeline(Readable.fromWeb(response.body), meter, handle.createWriteStream());
    if (byteSize === 0) throw new APIError(-1, "站点返回了空媒体文件", 502);
    await fs.promises.link(temporary, destination);
  } finally {
    await handle.close();
    await fs.promises.rm(temporary, { force: true });
  }
  return { assetId, path: destination, byteSize, mime };
}

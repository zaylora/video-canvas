import { execFile } from "node:child_process";
import fs from "node:fs/promises";
import path from "node:path";
import { promisify } from "node:util";

import { APIError } from "../client.ts";
import type { Args, Ctx, WireVersion } from "../types.ts";
import { intFlag, stringFlag } from "./args.ts";
import { versionFlag } from "./collaboration-version.ts";
import { downloadAsset } from "./asset-download.ts";
import { annotationOptions, renderAnnotation } from "./collaboration-media-annotate.ts";
import { canvasMediaDraft } from "./collaboration-media-draft.ts";
import { gridOptions, renderGrid } from "./collaboration-media-grid.ts";
import { outpaintPadding, outpaintSubmit, renderOutpaint } from "./collaboration-media-outpaint.ts";
import { separateVoice } from "./collaboration-media-vocal.ts";
import {
  CollaborationError,
  getCollaboration,
  type CollaborationSnapshot,
} from "./collaboration.ts";
import { emit } from "./shared.ts";

const run = promisify(execFile);
const sourceLimit = 200 * 1024 * 1024;
const maxTimeMs = 60 * 60 * 1_000;
const usage =
  "用法：canvas media frame|trim|grid|annotate|outpaint <画布> <节点> --epoch N --revision N -o <文件或目录>";

const inputFormats = new Map([
  ["video/mp4", "mov"],
  ["video/quicktime", "mov"],
  ["video/webm", "matroska"],
  ["audio/mp4", "mov"],
  ["audio/mpeg", "mp3"],
  ["audio/wav", "wav"],
  ["audio/x-wav", "wav"],
  ["audio/ogg", "ogg"],
  ["audio/flac", "flac"],
  ["audio/aac", "aac"],
  ["audio/webm", "matroska"],
  ["image/png", "png_pipe"],
  ["image/jpeg", "jpeg_pipe"],
  ["image/webp", "webp_pipe"],
  ["image/gif", "gif"],
]);

function sourceNode(snapshot: CollaborationSnapshot, id: string) {
  const data = snapshot.canvas.payload_json?.nodes?.find((node) => node.id === id)?.data;
  if (!data || (data.kind !== "audio" && data.kind !== "video" && data.kind !== "image")) {
    throw new APIError(-1, "请选择含素材的媒体节点", 2);
  }
  const output = data.outputs?.find(({ id }) => String(id) === String(data.activeOutputId));
  const rawId = output?.assetId ?? data.assetId;
  const assetId = typeof rawId === "number" && Number.isSafeInteger(rawId) ? String(rawId) : rawId;
  if (
    typeof assetId !== "string" ||
    !/^[1-9]\d{0,19}$/.test(assetId) ||
    BigInt(assetId) > 18_446_744_073_709_551_615n
  ) {
    throw new APIError(-1, "节点没有可读的已确认素材", 2);
  }
  return { kind: data.kind, assetId };
}

function sourceDetails(
  canvasId: string,
  nodeId: string,
  assetId: string,
  epoch: WireVersion,
  revision: WireVersion,
) {
  return { canvasId, nodeId, assetId, epoch: String(epoch), revision: String(revision) };
}

function verifySource(snapshot: CollaborationSnapshot, epoch: WireVersion, revision: WireVersion) {
  if (snapshot.epoch !== epoch)
    throw new CollaborationError(30018, "协作会话已变更", 409, "epoch_expired");
  if (snapshot.canvas.revision !== revision)
    throw new CollaborationError(30002, "观察版本已变化，请重新读取画布", 409, "conflict");
}

function frameFilter(args: Args) {
  const which =
    args.flags.which === undefined
      ? undefined
      : stringFlag(args.flags, "which", "--which first|last");
  const at = intFlag(args.flags, "at-ms", undefined, { min: 0, max: maxTimeMs });
  if (which !== undefined && (at !== undefined || (which !== "first" && which !== "last"))) {
    throw new APIError(-1, "使用 --which first|last 或 --at-ms 毫秒", 2);
  }
  return { which: which ?? (at === undefined ? "first" : "at"), at };
}

function trimRange(args: Args) {
  const from = intFlag(args.flags, "from-ms", undefined, { min: 0, max: maxTimeMs });
  const to = intFlag(args.flags, "to-ms", undefined, { min: 1, max: maxTimeMs });
  if (from === undefined || to === undefined || to <= from) {
    throw new APIError(-1, "请用 --from-ms 和 --to-ms 指定递增的毫秒区间", 2);
  }
  return { from, to };
}

function seconds(ms: number): string {
  return (ms / 1_000).toFixed(3);
}

async function encode(
  input: string,
  mime: string,
  filters: string[],
  target: string,
  seek: string[] = [],
) {
  const format = inputFormats.get(mime.toLowerCase());
  if (!format)
    throw new CollaborationError(-1, "站点返回了当前 CLI 无法加工的媒体格式", 415, "unsupported");
  try {
    await run(
      "ffmpeg",
      [
        "-nostdin",
        "-v",
        "error",
        "-protocol_whitelist",
        "file,pipe",
        "-f",
        format,
        ...seek,
        "-i",
        input,
        ...filters,
        "-fs",
        String(sourceLimit + 1),
        "-n",
        target,
      ],
      {
        timeout: 120_000,
        maxBuffer: 64 * 1024,
        windowsHide: true,
      },
    );
  } catch (error) {
    if ((error as NodeJS.ErrnoException).code === "ENOENT") {
      throw new CollaborationError(-1, "本地媒体加工需要安装 FFmpeg", 409, "unsupported");
    }
    throw new APIError(-1, "媒体加工失败，请检查输入编码与时间范围", 422);
  }
  const stats = await fs.stat(target).catch(() => undefined);
  if (!stats?.size || stats.size > sourceLimit)
    throw new APIError(-1, "加工结果为空或超过 200 MB", 422);
}

export async function canvasMedia(ctx: Ctx, args: Args): Promise<void> {
  const [action, code, node] = args.positional;
  if (action === "stage" || action === "attach") return canvasMediaDraft(ctx, args, action);
  if (action === "outpaint-submit") return outpaintSubmit(ctx, args);
  if (action === "separate-voice") return separateVoice(ctx, args);
  if (
    !code ||
    !node ||
    args.positional.length !== 3 ||
    (action !== "frame" &&
      action !== "trim" &&
      action !== "grid" &&
      action !== "annotate" &&
      action !== "outpaint")
  )
    throw new APIError(-1, usage, 2);
  const epoch = versionFlag(args.flags, "epoch");
  const revision = versionFlag(args.flags, "revision");
  if (epoch === undefined || revision === undefined) throw new APIError(-1, usage, 2);
  const output = path.resolve(stringFlag(args.flags, "output", usage));
  const snapshot = await getCollaboration(ctx, code);
  verifySource(snapshot, epoch, revision);
  const { assetId, kind } = sourceNode(snapshot, node);
  const source = sourceDetails(snapshot.canvas.id, node, assetId, epoch, revision);
  const verify = async () =>
    verifySource(await getCollaboration(ctx, snapshot.canvas.id), epoch, revision);
  if (action === "outpaint") {
    if (kind !== "image" || path.extname(output).toLowerCase() !== ".png") {
      throw new APIError(-1, "扩图底片需要图片节点及 .png 输出文件", 2);
    }
    const padding = outpaintPadding(args);
    const directory = await fs.mkdtemp(path.join(path.dirname(output), ".vc-media-"));
    try {
      const normalized = await normalizeImage(ctx, source, directory);
      const image = await renderOutpaint(normalized, padding, directory, encode);
      await verify();
      await publishMediaFile(image.file, output);
      emit(
        ctx,
        { action, path: output, source, width: image.width, height: image.height },
        `已生成扩图底片 ${output}`,
      );
    } finally {
      await fs.rm(directory, { recursive: true, force: true });
    }
    return;
  }
  if (action === "annotate") {
    if (kind !== "image" || path.extname(output).toLowerCase() !== ".png") {
      throw new APIError(-1, "图片标注需要图片节点及 .png 输出文件", 2);
    }
    const strokes = await annotationOptions(args);
    const directory = await fs.mkdtemp(path.join(path.dirname(output), ".vc-media-"));
    try {
      const normalized = await normalizeImage(ctx, source, directory);
      const rendered = await renderAnnotation(normalized, strokes, directory);
      const { size } = await fs.stat(rendered);
      if (!size || size > sourceLimit) throw new APIError(-1, "标注图片为空或超过 200 MB", 422);
      await verify();
      await publishMediaFile(rendered, output);
    } finally {
      await fs.rm(directory, { recursive: true, force: true });
    }
    return emit(ctx, { action, path: output, source }, `已生成 ${output}`);
  }
  if (action === "grid") {
    if (kind !== "image") throw new APIError(-1, "宫格切分需要图片节点", 2);
    const tiles = await runImageGrid(ctx, args, output, source, verify);
    emit(
      ctx,
      { action, path: output, source, tiles },
      `已在 ${output} 生成 ${tiles.length} 张图片`,
    );
    return;
  }
  if (kind === "image") throw new APIError(-1, "抽帧和修剪需要视频或音频节点", 2);
  const extension = action === "frame" ? ".png" : kind === "video" ? ".mp4" : ".m4a";
  if (
    path.extname(output).toLowerCase() !== extension ||
    (action === "frame" && kind !== "video")
  ) {
    throw new APIError(
      -1,
      `当前操作需要 ${action === "frame" ? "视频节点及 " : ""}${extension} 输出文件`,
      2,
    );
  }
  const frame = action === "frame" ? frameFilter(args) : undefined;
  const range = action === "trim" ? trimRange(args) : undefined;
  const directory = await fs.mkdtemp(path.join(path.dirname(output), ".vc-media-"));
  const target = path.join(directory, `result${extension}`);
  try {
    const input = path.join(directory, "source");
    const { mime } = await downloadAsset(ctx, assetId, input, snapshot.canvas.id, sourceLimit);
    if (frame) {
      const seek =
        frame.which === "last"
          ? ["-sseof", "-2"]
          : frame.at === undefined
            ? []
            : ["-ss", seconds(frame.at)];
      const filters = frame.which === "last" ? ["-update", "1"] : ["-frames:v", "1"];
      await encode(input, mime, ["-map", "0:v:0", ...filters], target, seek);
    } else if (range) {
      const codecs =
        kind === "video"
          ? ["-map", "0:v:0", "-map", "0:a:0?", "-c:v", "libx264", "-pix_fmt", "yuv420p"]
          : ["-map", "0:a:0", "-vn"];
      await encode(
        input,
        mime,
        [...codecs, "-c:a", "aac", "-t", seconds(range.to - range.from), "-movflags", "+faststart"],
        target,
        ["-ss", seconds(range.from)],
      );
    }
    await verify();
    await publishMediaFile(target, output);
  } finally {
    await fs.rm(directory, { recursive: true, force: true });
  }
  emit(
    ctx,
    {
      action,
      path: output,
      source,
      ...(frame ?? range),
    },
    `已生成 ${output}`,
  );
}

async function publishMediaFile(target: string, output: string) {
  await fs.chmod(target, 0o600);
  await fs.link(target, output).catch((error: NodeJS.ErrnoException) => {
    if (error.code === "EEXIST") throw new APIError(-1, "目标文件已存在", 409);
    throw error;
  });
}

async function normalizeImage(
  ctx: Ctx,
  source: ReturnType<typeof sourceDetails>,
  directory: string,
) {
  const input = path.join(directory, "source");
  const normalized = path.join(directory, "source.png");
  const { mime } = await downloadAsset(ctx, source.assetId, input, source.canvasId, sourceLimit);
  if (!mime.toLowerCase().startsWith("image/")) throw new APIError(-1, "节点素材应为图片", 422);
  await encode(input, mime, ["-map", "0:v:0", "-frames:v", "1", "-pix_fmt", "rgba"], normalized, [
    "-max_pixels",
    "16777216",
  ]);
  return normalized;
}

async function runImageGrid(
  ctx: Ctx,
  args: Args,
  output: string,
  source: ReturnType<typeof sourceDetails>,
  verify: () => Promise<void>,
) {
  const grid = gridOptions(args);
  const directory = await fs.mkdtemp(path.join(path.dirname(output), ".vc-media-"));
  try {
    const normalized = await normalizeImage(ctx, source, directory);
    const tiles = await renderGrid(normalized, grid, directory);
    let total = 0;
    for (const { name } of tiles) {
      const file = path.join(directory, name);
      const { size } = await fs.stat(file);
      if (size === 0 || (total += size) > sourceLimit)
        throw new APIError(-1, "宫格文件为空或总大小超过 200 MB", 422);
      await fs.chmod(file, 0o600);
    }
    await verify();
    await fs.mkdir(output, { mode: 0o700 }).catch((error: NodeJS.ErrnoException) => {
      if (error.code === "EEXIST") throw new APIError(-1, "目标路径已存在", 409);
      throw error;
    });
    const results = tiles.map((tile) => ({ ...tile, path: path.join(output, tile.name) }));
    const created: string[] = [];
    try {
      for (const tile of results) {
        await fs.link(path.join(directory, tile.name), tile.path);
        created.push(tile.path);
      }
    } catch (error) {
      for (const file of created) await fs.rm(file, { force: true });
      await fs.rmdir(output).catch(() => {});
      throw error;
    }
    return results;
  } finally {
    await fs.rm(directory, { recursive: true, force: true });
  }
}

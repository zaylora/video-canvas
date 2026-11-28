// 画布：列表、详情、新建、导入导出、整图读写。
import path from "node:path";

import { APIError, request } from "../client.ts";
import { boolFlag, intFlag, stringFlag, toInt } from "./args.ts";
import {
  CollaborationError,
  getCollaboration,
  readOwnedCanvas,
  readSharedCanvas,
  snapshotCanvas,
  tryCollaboration,
} from "./collaboration.ts";
import { kindName } from "./node.ts";
import {
  askYes,
  assertGraphPayload,
  baseOf,
  emit,
  readJSONFile,
  requireInteractive,
  requireToken,
  resolveOutput,
  writeJSONFile,
} from "./shared.ts";
import type { JSONValue } from "./shared.ts";
import type {
  Args,
  Canvas,
  CanvasNode,
  Ctx,
  ExportResult,
  Graph,
  ImportResult,
  Page,
} from "../types.ts";

function formatCanvasRows(list: Canvas[]): string {
  if (!list.length) return "（没有画布）";
  const lines = ["CODE\tREV\tUPDATED\tTITLE"];
  for (const item of list) {
    lines.push(
      `${item.id}\t${item.revision}\t${String(item.updated_at).replace("T", " ").slice(0, 19)}\t${item.title}`,
    );
  }
  return lines.join("\n");
}

export async function canvasList(ctx: Ctx, args: Args): Promise<void> {
  const token = requireToken(ctx);
  const scope = args.flags.scope ?? "personal";
  if (scope !== "personal" && scope !== "shared" && scope !== "all") {
    throw new APIError(-1, "--scope 需要 personal、shared 或 all", 2);
  }
  const pageSizeRaw =
    args.flags["page-size"] !== undefined ? args.flags["page-size"] : args.flags.page_size;
  const load = (path: string) =>
    request<Page<Canvas>>({
      base: baseOf(ctx),
      token,
      path,
      query: {
        page: intFlag(args.flags, "page", undefined),
        page_size:
          pageSizeRaw === undefined ? undefined : toInt(pageSizeRaw, "page-size", { max: 200 }),
      },
      fetchImpl: ctx.fetchImpl,
    });
  if (scope === "all") {
    const [personal, shared] = await Promise.all([load("/canvas"), load("/canvas/shared")]);
    emit(
      ctx,
      { personal, shared },
      `个人画布\n${formatCanvasRows(personal.list ?? [])}\n\n共享画布\n${formatCanvasRows(shared.list ?? [])}`,
    );
    return;
  }
  const page = await load(scope === "shared" ? "/canvas/shared" : "/canvas");
  const list = page.list || [];
  emit(
    ctx,
    { list, total: page.total, page: page.page, page_size: page.page_size },
    `${formatCanvasRows(list)}\n共 ${page.total} 个，第 ${page.page} 页`,
  );
}

/** 取一张画布的完整数据；code 是画布的对外十六进制 id。 */
export async function fetchCanvas(ctx: Ctx, code: string): Promise<Canvas> {
  if (ctx.automation) return snapshotCanvas(await getCollaboration(ctx, code));
  const owned = await readOwnedCanvas(ctx, code);
  if (owned && !owned.collaboration_active) return owned;
  const snapshot = await tryCollaboration(ctx, owned?.id ?? code);
  if (snapshot) return snapshotCanvas(snapshot);
  if (owned) throw new CollaborationError(30018, "请重新读取当前协作会话", 409, "epoch_expired");
  return readSharedCanvas(ctx, code);
}

export async function canvasGet(ctx: Ctx, args: Args): Promise<void> {
  const code = args.positional[0];
  if (!code) throw new APIError(-1, "用法：canvas get <code>", 2);
  const canvas = await fetchCanvas(ctx, code);
  const graph: Graph = canvas.payload_json || {};
  const human = [
    `画布：${canvas.title}（${canvas.id}）`,
    `版本：${canvas.revision}`,
    `更新时间：${canvas.updated_at}`,
    `节点：${(graph.nodes || []).length}  连线：${(graph.edges || []).length}`,
  ].join("\n");
  emit(ctx, canvas, human);
}

/** 新建项目前的确认信息。 */
interface CreateConfirm {
  what: string;
  title: string;
  lines: string[];
  hint: string;
}

/**
 * 新建项目（画布）前的硬门禁。
 *
 * 为什么单独设这道门：`canvas create` / `canvas import` 会在账号下**新增**一张画布，
 * 而命令行是能被脚本/Agent 批量调用的——没有拦截的话，一次循环就能堆出几十张没人知道的空项目，
 * 事后还得有人去前端一张张删（CLI 本身没有 delete）。节点级修改只是改已有画布，这道门管的是「新建」这类动作。
 *
 * 三道门与节点级修改一致：--json 不可用（脚本无法"确认"）、stdin/stderr 必须是终端、必须输入 yes。
 */
async function confirmCreate(ctx: Ctx, confirm: CreateConfirm): Promise<void> {
  const { what, title, lines, hint } = confirm;
  if (ctx.json)
    throw new APIError(
      -1,
      `\`${what}\` 会在账号下新建项目，必须先由人确认，不能与 --json 一起用；${hint}`,
      2,
    );
  if (!process.stdin.isTTY || !process.stderr.isTTY)
    throw new APIError(
      -1,
      [
        `\`${what}\` 需要交互确认：新建项目会在账号下留下新画布，当前终端不是交互式的，已拒绝执行。`,
        `请在终端里手动执行；${hint}`,
      ].join("\n"),
      2,
    );
  ctx.stderr(`将新建画布：`);
  ctx.stderr(`  标题：${title}`);
  for (const line of lines) ctx.stderr(`  ${line}`);
  await askYes(ctx, `确认新建？输入 yes 回车，其它任意输入取消：`, "已取消，没有新建任何画布");
}

export async function canvasCreate(ctx: Ctx, args: Args): Promise<void> {
  const token = requireToken(ctx);
  const usage = "canvas create --title <标题> [--payload 文件]";
  const title =
    args.flags.title === undefined ? "未命名画布" : stringFlag(args.flags, "title", usage);
  const payload: JSONValue =
    args.flags.payload === undefined
      ? { nodes: [], edges: [], viewport: { x: 0, y: 0, zoom: 1 } }
      : readJSONFile(stringFlag(args.flags, "payload", usage));
  assertGraphPayload(payload, "canvas create");

  const nodes = payload.nodes || [];
  const edges = payload.edges || [];
  await confirmCreate(ctx, {
    what: "canvas create",
    title,
    lines: [
      `内容：${nodes.length ? `${nodes.length} 个节点、${edges.length} 条连线` : "空白画布"}`,
      `来源：${args.flags.payload === undefined ? "命令行参数（无 payload）" : "命令行参数 --payload"}`,
    ],
    hint: "要在脚本里批量建画布，只能在终端里逐次确认；CLI 没有跳过确认的开关。",
  });

  const canvas = await request<Canvas>({
    base: baseOf(ctx),
    token,
    method: "POST",
    path: "/canvas",
    body: { title, payload_json: payload },
    fetchImpl: ctx.fetchImpl,
  });
  emit(ctx, canvas, `已创建：${canvas.title}（${canvas.id}）版本 ${canvas.revision}`);
}

export async function canvasImport(ctx: Ctx, args: Args): Promise<void> {
  const token = requireToken(ctx);
  const file = args.positional[0] ?? args.flags.file;
  if (file === undefined) throw new APIError(-1, "用法：canvas import <导出文件.json>", 2);
  const document: JSONValue = readJSONFile(file);
  const source = document.canvas || {};
  const assets = Array.isArray(document.assets)
    ? document.assets.length
    : Object.keys(document.assets || {}).length;
  await confirmCreate(ctx, {
    what: "canvas import",
    title: source.title || "（导出文档里没有标题）",
    lines: [
      `来源：${typeof file === "string" ? path.resolve(file) : file}`,
      `内容：${(source.nodes || []).length} 个节点、${(source.edges || []).length} 条连线、${assets} 份素材`,
    ],
    hint: "导入只新建画布，不会覆盖已有画布；已存在的画布请用 canvas save 改。",
  });
  const result = await request<ImportResult>({
    base: baseOf(ctx),
    token,
    method: "POST",
    path: "/canvas/import",
    body: { document },
    fetchImpl: ctx.fetchImpl,
  });
  const canvas: Canvas = result.canvas || (result as unknown as Canvas);
  const report = result.report || {};
  emit(
    ctx,
    result,
    [
      `已导入：${canvas.title}（${canvas.id}）`,
      `素材：复制 ${report.assets?.copied ?? 0} / 共 ${report.assets?.total ?? 0}，丢失 ${report.assets?.missing?.length ?? 0}`,
      `丢弃连线：${report.edges_dropped ?? 0}`,
    ].join("\n"),
  );
}

export async function canvasExport(ctx: Ctx, args: Args): Promise<void> {
  const token = requireToken(ctx);
  const code = args.positional[0];
  if (!code) throw new APIError(-1, "用法：canvas export <code> [-o 文件]", 2);
  const out = resolveOutput(
    ctx,
    args,
    `canvas-${code}.json`,
    "canvas export <code> [-o 文件] [--force]",
  );
  const result = await request<ExportResult>({
    base: baseOf(ctx),
    token,
    path: `/canvas/${encodeURIComponent(code)}/export`,
    fetchImpl: ctx.fetchImpl,
  });
  const document = result.document ?? result;
  if (out === "-") {
    ctx.stdout(JSON.stringify(document, null, 2));
    return;
  }
  const saved = writeJSONFile(ctx, out, document, { overwrite: boolFlag(args.flags, "force") });
  emit(
    ctx,
    { file: saved, skipped_assets: result.report?.skipped_assets ?? 0 },
    `已导出到 ${saved}`,
  );
}

export async function canvasGraph(ctx: Ctx, args: Args): Promise<void> {
  const code = args.positional[0];
  if (!code) throw new APIError(-1, "用法：canvas graph <code> [-o 文件]", 2);
  const out = resolveOutput(ctx, args, undefined, "canvas graph <code> [-o 文件]");
  const canvas = await fetchCanvas(ctx, code);
  const graph: Graph = canvas.payload_json || {
    nodes: [],
    edges: [],
    viewport: { x: 0, y: 0, zoom: 1 },
  };
  // revision 是后面 canvas save 的必填项：stdout 只放图时（-o - 或不带 -o）写到 stderr，别让它丢了
  if (out === "-" || (!out && !ctx.json)) {
    ctx.stdout(JSON.stringify(graph, null, 2));
    ctx.stderr(`版本：${canvas.revision}（canvas save 需要 --revision ${canvas.revision}）`);
    return;
  }
  if (out) {
    const saved = writeJSONFile(ctx, out, graph);
    emit(
      ctx,
      { file: saved, revision: canvas.revision },
      [
        `已写出 ${saved}（版本 ${canvas.revision}）`,
        `改完后保存：canvas save ${code} --payload ${saved} --revision ${canvas.revision}`,
      ].join("\n"),
    );
    return;
  }
  ctx.stdout(JSON.stringify({ ok: true, data: { revision: canvas.revision, graph } }));
}

/**
 * 整图保存：必须显式给 --revision，也就是「我手里这份图是从哪个版本取的」。
 * 以前没给就现读一次当前版本——读到的版本和用户手里的文件不是同一份，别人在这期间的改动会被静默覆盖。
 * 新图会让现有节点消失时，还要走和 node rm 一样的终端人工确认。
 */
export async function canvasSave(ctx: Ctx, args: Args): Promise<void> {
  const token = requireToken(ctx);
  const code = args.positional[0];
  const usage = "canvas save <code> --payload <文件.json> --revision N";
  if (!code) throw new APIError(-1, `用法：${usage}`, 2);
  const file = stringFlag(args.flags, "payload", usage);
  const payload = readJSONFile(file);
  assertGraphPayload(payload, "canvas save");
  if (args.flags.revision === undefined)
    throw new APIError(
      -1,
      `canvas save 必须显式给 --revision（你手里这份图取自哪个版本）。\n` +
        `用 canvas graph ${code} -o 图.json 取图，输出里的「版本」就是它；用法：${usage}`,
      2,
    );
  const revision = intFlag(args.flags, "revision", undefined) as number;

  const current = await fetchCanvas(ctx, code);
  if (current.collaboration_role === "viewer") {
    throw new CollaborationError(-1, "仅查看成员不能修改画布", 403, "permission_denied");
  }
  if (current.collaboration_active) {
    throw new CollaborationError(
      30010,
      "协作画布请用 canvas plan + canvas apply 提交差异",
      409,
      "unsupported",
    );
  }
  const keep = new Set<string>(
    (Array.isArray(payload?.nodes) ? payload.nodes : []).map((n: CanvasNode) => String(n?.id)),
  );
  const removed = ((current.payload_json?.nodes || []) as CanvasNode[]).filter(
    (n) => !keep.has(String(n.id)),
  );
  if (removed.length) {
    requireInteractive(
      ctx,
      "会删除节点的整图保存",
      "只想改个别节点请用 canvas node set / add；确要删节点请在终端里逐次确认。",
    );
    ctx.stderr(
      `画布：${current.title}（${code}）当前版本 ${current.revision}，你给的基准版本 ${revision}`,
    );
    ctx.stderr(`这份图会让 ${removed.length} 个现有节点消失：`);
    for (const n of removed)
      ctx.stderr(
        `  - ${kindName(n.data?.kind)}节点 ${n.id}${n.data?.label ? `「${n.data.label}」` : ""}`,
      );
    await askYes(ctx, "确认保存？输入 yes 回车，其它任意输入取消：", "已取消，画布未修改");
  }

  const canvas = await request<Canvas>({
    base: baseOf(ctx),
    token,
    method: "PUT",
    path: `/canvas/${encodeURIComponent(code)}`,
    body: { revision, payload_json: payload },
    fetchImpl: ctx.fetchImpl,
  });
  emit(ctx, canvas, `已保存，新版本 ${canvas.revision}`);
}

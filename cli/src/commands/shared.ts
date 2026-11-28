// 各命令共用的小工具：输出、读写 JSON 文件、令牌与 API 地址、终端确认。
import fs from "node:fs";
import path from "node:path";
import readline from "node:readline/promises";

import { APIError, errorMessage, normalizeBase } from "../client.ts";
import type { Args, Ctx, Flags } from "../types.ts";

/** JSON 解析出来的值：结构由调用方按用途断言，不在解析处假装知道形状。 */
// eslint-disable-next-line @typescript-eslint/no-explicit-any
export type JSONValue = any;

/** 任务的终态：到这些状态就不必再等。 */
export const TERMINAL_TASK_STATUSES = new Set<string>([
  "succeeded",
  "failed",
  "canceled",
  "expired",
]);

export const sleep = (ms: number): Promise<void> =>
  new Promise((resolve) => setTimeout(resolve, ms));

/** 成功输出：--json 时一行 JSON（Agent 友好），否则人读文本。 */
export function emit(ctx: Ctx, data: unknown, human: string): void {
  if (ctx.json) {
    ctx.stdout(JSON.stringify({ ok: true, data }));
  } else {
    ctx.stdout(human);
  }
}

/** 读 JSON 文件；路径写错或内容不是 JSON 都属于用法错误（退出码 2）。 */
export function readJSONFile(file: unknown): JSONValue {
  if (file === true || typeof file !== "string")
    throw new APIError(-1, "需要一个 JSON 文件路径", 2);
  let text: string;
  try {
    text = fs.readFileSync(file, "utf8");
  } catch (err) {
    throw new APIError(-1, `读取 ${file} 失败：${errorMessage(err)}`, 2);
  }
  try {
    return JSON.parse(text);
  } catch (err) {
    throw new APIError(-1, `${file} 不是合法 JSON：${errorMessage(err)}`, 2);
  }
}

/**
 * 写 JSON 文件并返回展示用的路径。
 * `-` 表示输出到 stdout（与常见 Unix 习惯一致），此时不落盘、也不返回绝对路径。
 */
export function writeJSONFile(
  ctx: Ctx,
  file: unknown,
  value: unknown,
  options: { overwrite?: boolean } = {},
): string {
  const { overwrite = true } = options;
  const text = `${JSON.stringify(value, null, 2)}\n`;
  if (file === "-") {
    ctx.stdout(text.trimEnd());
    return "-";
  }
  if (file === true || typeof file !== "string")
    throw new APIError(-1, "写文件需要给出路径（用 - 表示输出到 stdout）", 2);
  const target = path.resolve(file);
  try {
    // wx：文件已存在就失败，检查与写入是一步，不会被别的进程插在中间
    fs.writeFileSync(target, text, { flag: overwrite ? "w" : "wx" });
  } catch (err) {
    if ((err as NodeJS.ErrnoException)?.code === "EEXIST")
      throw new APIError(-1, `${target} 已存在，不覆盖；确认要覆盖就加 --force`, 2);
    throw new APIError(-1, `写入 ${target} 失败：${errorMessage(err)}`, 0);
  }
  return target;
}

/**
 * 拒绝把 canvas export 的整份导出文档当成画布 payload 灌进去。
 * save / create 要的是 canvas graph 的输出（nodes / edges / viewport）；
 * 导出文档额外带 format / canvas / assets，直接写进去会把画布结构弄坏。
 */
export function assertGraphPayload(payload: JSONValue, command: string): void {
  if (payload && typeof payload === "object" && payload.format && payload.canvas) {
    throw new APIError(
      -1,
      `${command} 收到的是 canvas export 的导出文档（含 format / canvas）。` +
        `要建新画布请用 canvas import；要整图保存请先用 canvas graph 取 nodes 与 edges。`,
      2,
    );
  }
}

/** 解析 --output / -o；`-` 表示 stdout，且不能与 --json 同时用。 */
export function resolveOutput(
  ctx: Ctx,
  args: Args,
  fallback: string | undefined,
  usage: string,
): string | undefined {
  const raw = args.flags.output !== undefined ? args.flags.output : args.flags.o;
  if (raw === undefined) return fallback;
  if (raw === true || typeof raw !== "string")
    throw new APIError(-1, `--output 需要文件路径（用 - 表示输出到 stdout）；用法：${usage}`, 2);
  if (raw === "-" && ctx.json)
    throw new APIError(-1, "--output - 与 --json 冲突：stdout 只能放一种内容", 2);
  return raw;
}

export function requireToken(ctx: Ctx): string {
  if (ctx.automation) throw new APIError(-1, "自动化凭据只能访问授权的协作操作", 403);
  if (ctx.baseMismatch) {
    const { saved, requested } = ctx.baseMismatch;
    throw new APIError(
      -1,
      [
        `已保存的登录态属于 ${saved ?? "（凭据里没有记录登录站点）"}，而这次要访问的是 ${requested}。`,
        "令牌只在登录时的站点上有效，不会带到别的站点；",
        `要用这个站点请先执行：video-canvas-cli login --base ${requested}`,
      ].join("\n"),
      2,
    );
  }
  if (!ctx.token) throw new APIError(-1, "还没有登录，先执行 `video-canvas-cli login`", 0);
  return ctx.token;
}

/**
 * 取可用的 API 根地址。
 * 构建 ctx 时就失败过（比如 --base 少了 http://），优先抛那条原始错误——
 * 直接把 null 拼进 URL 只会得到误导性的「无法连接 null：Invalid URL」。
 */
export function baseOf(ctx: Ctx): string {
  if (ctx.base) return ctx.base;
  if (ctx.baseError) throw ctx.baseError;
  throw new APIError(-1, "缺少 API 地址，请用 --base 或环境变量 VC_API_BASE 指定", 2);
}

/** 把当前有效配置（命令行 > 环境变量 > 已保存）解析成 base。 */
export function resolveBase(ctx: Ctx, flags: Flags): string {
  return normalizeBase(flags.base || ctx.env.VC_API_BASE || ctx.creds?.base);
}

/** 终端确认的读写流：默认是真实终端，测试时可以换成内存流。 */
export interface AskIO {
  input: NodeJS.ReadableStream;
  output: NodeJS.WritableStream;
}

/**
 * 等人点头的唯一出口：只认终端里敲的 `yes`。
 * 取消用退出码 4（与「被取消」区分于「失败」），非交互终端与 --json 一律拒绝。
 *
 * 这是约定性的软闸门，不是安全边界：伪造 pty 或直接调接口都能绕过，
 * 它的作用是让「批量脚本一不小心改了画布」多一道必须有人在场的停顿。
 */
export async function askYes(
  _ctx: Ctx,
  question: string,
  cancelMessage: string,
  io: AskIO = { input: process.stdin, output: process.stderr },
): Promise<void> {
  const rl = readline.createInterface(io);
  let answer: string;
  try {
    answer = await new Promise<string>((resolve, reject) => {
      // 输入流在给出答案之前就结束（Ctrl-D、管道关闭）：question 的 Promise 永远不会落定，
      // 进程会静默地以 0 退出——必须自己接住 close，明确按「取消」处理。
      rl.once("close", () =>
        reject(new APIError(-1, "输入已结束（Ctrl-D 或管道关闭），没有收到确认，已取消", 0, 4)),
      );
      rl.question(question).then(resolve, reject);
    });
  } finally {
    rl.close();
  }
  if (answer.trim() !== "yes") throw new APIError(-1, cancelMessage, 0, 4);
}

/**
 * 需要人在场确认的动作的前置检查：--json 与非交互终端一律拒绝（脚本没法「确认」）。
 * what 是动作名，hint 说明脚本里该怎么办。
 */
export function requireInteractive(ctx: Ctx, what: string, hint: string): void {
  if (ctx.json) throw new APIError(-1, `${what}必须先由人确认，不能与 --json 一起用；${hint}`, 2);
  if (!process.stdin.isTTY || !process.stderr.isTTY)
    throw new APIError(
      -1,
      [
        `${what}需要交互确认：当前终端不是交互式的，已拒绝执行。`,
        `请在终端里手动执行；${hint}`,
      ].join("\n"),
      2,
    );
}

/** 提交后等任务到终态；超时抛退出码 3，与 task wait 一致。 */
/** 人类可读的已等时长：75s / 3m20s。 */
export function formatElapsed(ms: number): string {
  const total = Math.floor(ms / 1000);
  const m = Math.floor(total / 60);
  return m > 0 ? `${m}m${String(total % 60).padStart(2, "0")}s` : `${total}s`;
}

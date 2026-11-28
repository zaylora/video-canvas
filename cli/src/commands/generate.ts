// 生成：提交 /generation-tasks，可选等待结果。
import { randomUUID } from "node:crypto";
import fs from "node:fs";

import { APIError, errorMessage } from "../client.ts";
import { boolFlag, intFlag, listFlag, stringFlag } from "./args.ts";
import { generationBudget, submitGenerationRequest } from "./collaboration-generation.ts";
import { TERMINAL_TASK_STATUSES, emit, formatElapsed, readJSONFile } from "./shared.ts";
import { formatTask, waitTaskTerminal } from "./task.ts";
import type { JSONValue } from "./shared.ts";
import type { Args, Ctx, FlagValue, Flags, GenerateItem, Task } from "../types.ts";

const GENERATE_KINDS = new Set<string>(["image", "video", "audio", "text"]);

/** 读 --input：JSON 字符串、@文件 或 -（标准输入）。无论从哪来，都只接受 JSON 对象。 */
function readInputJSON(flags: Flags, usage: string): Record<string, unknown> {
  const raw = flags.input;
  if (raw === undefined) return {};
  if (raw === true || typeof raw !== "string")
    throw new APIError(-1, `--input 需要 JSON 字符串、@文件 或 -；用法：${usage}`, 2);
  let parsed: JSONValue;
  if (raw.startsWith("@")) {
    parsed = readJSONFile(raw.slice(1));
  } else {
    let text = raw;
    if (raw === "-") {
      try {
        text = fs.readFileSync(0, "utf8");
      } catch (err) {
        throw new APIError(-1, `读取标准输入失败：${errorMessage(err)}`, 2);
      }
    }
    try {
      parsed = JSON.parse(text);
    } catch (err) {
      throw new APIError(-1, `--input 不是合法 JSON：${errorMessage(err)}`, 2);
    }
  }
  if (!parsed || typeof parsed !== "object" || Array.isArray(parsed))
    throw new APIError(-1, '--input 需要 JSON 对象，例如 \'{"prompt":"海边日出"}\'', 2);
  return parsed;
}

/** 把 --input / --prompt / --param / --ref-* 合成接口要的 input 对象。 */
function buildGenerateInput(flags: Flags, usage: string): Record<string, unknown> {
  const input: Record<string, unknown> = { ...readInputJSON(flags, usage) };
  if (flags.prompt !== undefined) input.prompt = stringFlag(flags, "prompt", usage);
  for (const pair of listFlag(flags, "param")) {
    const eq = pair.indexOf("=");
    if (eq < 1) throw new APIError(-1, `--param 要写成 名=值，收到「${pair}」`, 2);
    const name = pair.slice(0, eq).trim();
    const raw = pair.slice(eq + 1);
    let value: unknown = raw;
    // 数字、布尔、数组按 JSON 解释，其余按字符串：CLI 上 `--param duration=5` 最自然
    try {
      value = JSON.parse(raw);
    } catch {
      value = raw;
    }
    input[name] = value;
  }
  const refFlags: Array<[string, string]> = [
    ["ref-image", "images"],
    ["ref-video", "videos"],
    ["ref-audio", "audios"],
  ];
  for (const [key, name] of refFlags) {
    const ids = listFlag(flags, key);
    if (!ids.length) continue;
    input[name] = ids.map((id) => {
      if (!/^\d+$/.test(id))
        throw new APIError(-1, `--${key} 要素材 id（纯数字），收到「${id}」`, 2);
      return id;
    });
  }
  applyFrameFlags(flags, input, usage);
  return input;
}

/**
 * --first-frame / --last-frame：直接锁首尾帧。
 * 模型按「第一张图 = 首帧、第二张 = 尾帧」取值，所以尾帧不能单独给——单独给会顶到首帧位置上。
 */
function applyFrameFlags(flags: Flags, input: Record<string, unknown>, usage: string): void {
  const first = flags["first-frame"];
  const last = flags["last-frame"];
  if (first === undefined && last === undefined) return;
  const pick = (value: FlagValue | undefined, name: string): string => {
    if (value === undefined) return "";
    if (value === true || typeof value !== "string")
      throw new APIError(-1, `--${name} 需要素材 id。用法：${usage}`, 2);
    if (!/^\d+$/.test(value))
      throw new APIError(-1, `--${name} 要素材 id（纯数字），收到「${value}」`, 2);
    return value;
  };
  const head = pick(first, "first-frame");
  const tail = pick(last, "last-frame");
  if (!head)
    throw new APIError(
      -1,
      "--last-frame 要跟 --first-frame 一起给：模型把第一张图当首帧，只给尾帧会顶到首帧位置上",
      2,
    );
  const existing = Array.isArray(input.images) ? (input.images as string[]) : [];
  if (existing.length)
    throw new APIError(
      -1,
      "首尾帧与 --ref-image 不要混用：用 --first-frame/--last-frame 就够了",
      2,
    );
  // 模型的模式判定是「显式 mode > op > 素材」，显式给的冲突值会把锁帧顶掉且不报错，
  // 结果画面跟参考图毫无关系。这种自相矛盾的组合要当场拒掉，不能默默降级。
  const mode = typeof input.mode === "string" ? input.mode : "";
  const op = typeof input.op === "string" ? input.op : "";
  if (mode && mode !== "keyframe")
    throw new APIError(
      -1,
      `首尾帧与 --param mode=${mode} 冲突：锁帧走的是 keyframe，去掉 mode 或改成 keyframe`,
      2,
    );
  if (op && op !== "i2v")
    throw new APIError(-1, `首尾帧与 --param op=${op} 冲突：锁帧要走 i2v，去掉 op 或改成 i2v`, 2);
  if (input.audios !== undefined || input.videos !== undefined)
    throw new APIError(
      -1,
      "keyframe 模式只吃首尾两张图，--ref-audio / --ref-video 在这里不生效；要混参考请改用 --ref-image",
      2,
    );
  input.images = tail ? [head, tail] : [head];
  input.mode = "keyframe";
}

/** 这次提交里的参考图到底起什么作用，说清楚，别让人以为锁了首帧其实没有。 */
function describeRefInput(input: Record<string, unknown>): string {
  const images = Array.isArray(input.images) ? (input.images as unknown[]).map(String) : [];
  if (!images.length) return "";
  const mode = typeof input.mode === "string" ? input.mode : "";
  const op = typeof input.op === "string" ? input.op : "";
  if (mode === "keyframe" || op === "i2v")
    return images.length > 1
      ? `参考图：首帧 ${images[0]} / 尾帧 ${images[1]}（keyframe，锁帧）`
      : `参考图：首帧 ${images[0]}（keyframe，锁帧）`;
  return (
    `参考图 ${images.length} 张，按「参考生成」使用：影响风格与内容，但不锁首帧。` +
    "要锁首尾帧改用 --first-frame <首帧id> --last-frame <尾帧id>"
  );
}

function formatSubmittedItems(items: GenerateItem[]): string {
  const lines: string[] = [];
  for (const item of items) {
    if (item.task) {
      lines.push(
        `+ 节点 ${item.node_id || "-"}：任务 ${item.task.task_ref || item.task.id}（${item.task.status}，冻结 ${item.task.credits} 积分）`,
      );
    } else if (item.error) {
      lines.push(`! 节点 ${item.node_id || "-"}：失败 ${item.error.status} ${item.error.message}`);
    }
  }
  return lines.length ? lines.join("\n") : "（没有提交任何任务）";
}

/**
 * 提交一次生成。产物回填由画布负责：任务已经绑在节点上，打开画布时按节点拉取生成记录并落到节点，
 * 与在网页上点生成是同一条链路——CLI 不自己改节点，也就不会和画布里的编辑抢版本。
 */
export async function generate(ctx: Ctx, args: Args): Promise<void> {
  const kind = args.positional[0];
  const usage =
    "generate <image|video|audio|text> --model <模型key> " +
    "[--canvas <画布编号> --node <节点id>] [--prompt <提示词>] [--param 名=值]... " +
    "[--input '<json>'|@文件|-] [--ref-image <素材id>]... " +
    "[--first-frame <素材id> [--last-frame <素材id>]] [--count 1] [--max-credits N] [--wait] [--timeout 600] [--interval 3]";
  if (!kind || !GENERATE_KINDS.has(kind))
    throw new APIError(
      -1,
      `第一项要写生成种类（${[...GENERATE_KINDS].join(" / ")}）。用法：${usage}`,
      2,
    );
  const model = stringFlag(args.flags, "model", usage);
  const canvas = args.flags.canvas;
  const node = args.flags.node;
  const targets: Array<[string, FlagValue | undefined]> = [
    ["canvas", canvas],
    ["node", node],
  ];
  for (const [name, value] of targets) {
    if (value !== undefined && (value === true || typeof value !== "string"))
      throw new APIError(-1, `--${name} 需要字符串值。用法：${usage}`, 2);
  }
  // 绑节点才会在画布里留下这次生成；只给一个的话另一个就没意义了
  if ((canvas === undefined) !== (node === undefined))
    throw new APIError(-1, "--canvas 与 --node 要一起给（都省略就是一条不挂画布的临时任务）", 2);

  const input = buildGenerateInput(args.flags, usage);
  if (!Object.keys(input).length)
    throw new APIError(-1, "至少要给一个生成参数：--prompt、--param 或 --input", 2);

  const count = intFlag(args.flags, "count", 1, { max: 8 });
  if (count > 1 && node === undefined)
    throw new APIError(-1, "--count 大于 1 时必须给 --node：多个任务要绑在同一个节点上", 2);

  const body: Record<string, unknown> = { kind, model_id: model, input };
  if (canvas !== undefined) body.canvas_id = canvas;
  if (node !== undefined) {
    body.node_id = node;
    body.node_ids = Array.from({ length: count }, () => node);
  }

  // 等待参数在提交前就校验：提交之后才发现 --timeout 写错，积分已经冻结了
  const wait = boolFlag(args.flags, "wait");
  const timeoutSeconds = intFlag(args.flags, "timeout", 600, { max: 86400 });
  const intervalSeconds = intFlag(args.flags, "interval", 3, { max: 3600 });
  const idempotencyKey = idempotencyKeyOf(args.flags, usage);
  const maxCredits = intFlag(args.flags, "max-credits", undefined, { min: 0 });
  const budget = generationBudget(args.flags);

  // 先把参考图的语义说清楚：给了图不等于锁了首帧，模式不对会白跑一段。
  const refNote = describeRefInput(input);
  if (refNote) ctx.stderr(refNote);
  // 幂等键在发请求之前就打印：响应丢了、进程被杀时，用同一个键原样重跑不会重复扣费
  ctx.stderr(
    `幂等键：${idempotencyKey}（请求结果不确定时，加 --idempotency-key ${idempotencyKey} 重跑同一条命令，不会重复扣费）`,
  );
  const res = await submitGenerationRequest(ctx, idempotencyKey, body, maxCredits, { budget });
  const items = Array.isArray(res?.items) ? res.items : [];
  const tasks = items.map((item) => item.task).filter((task): task is Task => Boolean(task));
  const failures = items.filter((item) => item.error);
  if (!tasks.length) {
    const first = failures[0]?.error;
    const reason = first ? `${first.status} ${first.message}` : "服务端没有返回任务";
    // 上游满载 / 限流是等着就能过的，跟参数写错不是一回事，别让人以为要改命令
    const retryable = /队列已满|速率限制|繁忙|busy|rate.?limit|超时|timeout/i.test(reason);
    throw new APIError(
      first?.code ?? -1,
      `提交失败：${reason}${retryable ? "（上游繁忙，等一会儿重投即可，命令不用改）" : ""}`,
      0,
    );
  }

  if (!wait) {
    emit(
      ctx,
      { items, tasks, idempotency_key: idempotencyKey },
      [
        formatSubmittedItems(items),
        "",
        "任务已提交；完成后产物由服务端写回节点，不需要打开画布。看进度：task get <id>；要在这里等：加 --wait。",
      ].join("\n"),
    );
    return;
  }

  // 所有任务共用一个总截止时间，并发地等
  const startedAt = Date.now();
  const deadline = startedAt + timeoutSeconds * 1000;
  const outcomes = await Promise.all(
    tasks.map((task) => waitTaskTerminal(ctx, task, { deadline, startedAt, intervalSeconds })),
  );
  const settled = outcomes.map((one) => one.task);
  const data = { items, tasks: settled, idempotency_key: idempotencyKey };
  const human = [formatSubmittedItems(items), "", ...settled.map((task) => formatTask(task))].join(
    "\n",
  );
  const errored = outcomes.find((one) => one.error !== undefined);
  const pending = outcomes.filter((one) => one.timedOut);
  const failed = settled.filter(
    (task) => TERMINAL_TASK_STATUSES.has(task.status) && task.status !== "succeeded",
  );
  if (!errored && !pending.length && !failed.length) {
    emit(ctx, data, human);
    return;
  }

  // 失败、出错、超时三种结局共用一个出口：JSON 的 ok 与退出码一致，且所有已提交任务的 id 都在 data 里
  let code: number | string = -1;
  let message: string;
  let exitCode = 1;
  if (failed.length) {
    const first = failed[0] as Task;
    code = first.error_code ?? -1;
    message = first.error_message || `任务${first.status}`;
  } else if (errored) {
    message = errorMessage(errored.error);
  } else {
    exitCode = 3; // 没等到不是失败：独立退出码，脚本才能区分「还在跑」和「跑挂了」
    const ids = pending.map((one) => one.task.id).join(", ");
    message =
      `等待超时（${formatElapsed(timeoutSeconds * 1000)}），${pending.length} 个任务仍在跑（不是失败）：${ids}；` +
      "用 task wait <id> 接着等，或 task get <id> 查询";
  }
  if (ctx.json) {
    ctx.stdout(JSON.stringify({ ok: false, error: { code, message }, data }));
  } else {
    ctx.stdout(human);
    ctx.stderr(message);
  }
  process.exitCode = exitCode;
}

/** 幂等键：调用方给的优先（重跑同一条命令用），没给就生成一个新的。 */
function idempotencyKeyOf(flags: Flags, usage: string): string {
  const raw = flags["idempotency-key"];
  if (raw === undefined) return randomUUID();
  if (typeof raw !== "string" || !raw.trim() || raw.length > 128)
    throw new APIError(-1, `--idempotency-key 需要 1–128 个字符的字符串。用法：${usage}`, 2);
  return raw.trim();
}

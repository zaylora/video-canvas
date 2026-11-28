// 命令行参数解析：flag 的种类（开关 / 文本 / 多值）规则都在这里，命令本身不碰 argv。

import { APIError } from "../client.ts";
import type { Args, FlagValue, Flags } from "../types.ts";

/**
 * 短选项别名。只登记确实有文档承诺的那几个；其余 `-x` 原样留给 positional，
 * 这样 `trace` 透传给 agent-trace 的参数不会被 CLI 吞掉。
 */
const SHORT_FLAGS: Record<string, string> = { o: "output" };

/**
 * 可以给多次的 flag：保留成列表，而不是后一个覆盖前一个。
 * 只登记「本来就是多值」的几个（生成参数、参考素材、成片素材、镜号），
 * 其余 flag 仍是「后出现者生效」——单值字段给两次是笔误，不该悄悄变成数组。
 */
const MULTI_FLAGS = new Set<string>([
  "param",
  "ref-image",
  "ref-video",
  "ref-audio",
  "assets",
  "seqs",
  "to",
  "mention",
  "attach",
]);

/**
 * 只表示开关的 flag：后面跟的 token 是位置参数，不是它的值。
 * `canvas get --json CODE`、`agent approve --reject ID` 里 CODE / ID 不能被吞掉；
 * 仍然支持 `--json=true`，也认紧跟的字面量 true / false。
 */
const BOOLEAN_FLAGS = new Set<string>([
  "json",
  "jsonl",
  "once",
  "wait",
  "watch",
  "tasks",
  "voice-from-image",
  "active",
  "reject",
  "force",
  "no-open",
  "no-update-check",
]);

/**
 * 值常常是自由文本的 flag：值以 `-` 开头（`--text "- 列表项"`）也照样当值取。
 * 其余未登记的 flag 保持老行为：后面是选项就当开关。
 */
const TEXT_FLAGS = new Set<string>([
  "text",
  "prompt",
  "label",
  "message",
  "answer",
  "title",
  "name",
  "keyword",
  "input",
  "param",
  "idempotency-key",
]);

/**
 * 下一个 token 是不是「另一个选项」而不是值。
 * 负数（`-1`）和单个 `-`（stdout 约定）都算值，否则 `--page-size -1`、`-o -` 无法表达。
 */
function isOptionToken(token: unknown): boolean {
  if (typeof token !== "string") return false;
  if (token === "-") return false;
  if (/^-\d/.test(token)) return false;
  return token.startsWith("-");
}

/** 开关型 flag 是否开启：true 与 "true" 为开，"false" 与缺省为关。 */
export function boolFlag(flags: Flags, name: string): boolean {
  const value = flags[name];
  return value === true || value === "true";
}

/** 取一个必需的字符串型 flag（如 --payload、--title）；无值时给出明确用法。 */
export function stringFlag(flags: Flags, name: string, usage: string): string {
  const value = flags[name];
  if (value === undefined) throw new APIError(-1, usage, 2);
  if (value === true || typeof value !== "string")
    throw new APIError(-1, `--${name} 需要字符串值；用法：${usage}`, 2);
  return value;
}

export interface IntOptions {
  min?: number;
  max?: number;
}

/** 校验并转换一个整数值；非法值直接报用法错误，不静默退回默认值。 */
export function toInt(raw: FlagValue | number, name: string, options: IntOptions = {}): number {
  const { min = 1, max = Number.MAX_SAFE_INTEGER } = options;
  if (raw === true || Array.isArray(raw)) throw new APIError(-1, `--${name} 需要一个数字`, 2);
  const value = Number(raw);
  if (!Number.isInteger(value) || value < min || value > max)
    throw new APIError(-1, `--${name} 需要 ${min}–${max} 的整数，收到「${raw}」`, 2);
  return value;
}

/** 取一个整数型 flag；没给就用默认值。 */
export function intFlag(
  flags: Flags,
  name: string,
  fallback: undefined,
  options?: IntOptions,
): number | undefined;
export function intFlag(flags: Flags, name: string, fallback: number, options?: IntOptions): number;
export function intFlag(
  flags: Flags,
  name: string,
  fallback: number | undefined,
  options?: IntOptions,
): number | undefined;
export function intFlag(
  flags: Flags,
  name: string,
  fallback: number | undefined,
  options?: IntOptions,
): number | undefined {
  const raw = flags[name];
  return raw === undefined ? fallback : toInt(raw, name, options);
}

/**
 * 轻量参数解析：`--key value` / `--key=value` / `--flag` / `-o value`。
 * CLI 只有几个命令，不值得引入第三方解析器；语义固定，Agent 也容易生成。
 */
export function parseArgs(argv: string[]): Args {
  const positional: string[] = [];
  const flags: Flags = {};
  const setFlag = (name: string, value: string | true): void => {
    if (!MULTI_FLAGS.has(name)) {
      flags[name] = value;
      return;
    }
    const prev = flags[name];
    if (prev === undefined) flags[name] = [value];
    else if (Array.isArray(prev)) prev.push(value);
    else flags[name] = [prev, value];
  };
  for (let i = 0; i < argv.length; i++) {
    const arg = argv[i] as string;
    if (arg === "-h" || arg === "--help") {
      flags.help = true;
      continue;
    }
    if (arg.startsWith("--")) {
      const eq = arg.indexOf("=");
      if (eq > -1) {
        setFlag(arg.slice(2, eq), arg.slice(eq + 1));
        continue;
      }
      const name = arg.slice(2);
      const next = argv[i + 1];
      if (BOOLEAN_FLAGS.has(name)) {
        // 开关只认紧跟的 true / false 字面量，其余都留给位置参数
        if (next === "true" || next === "false") {
          setFlag(name, next);
          i++;
        } else {
          setFlag(name, true);
        }
      } else if (next === undefined || (isOptionToken(next) && !TEXT_FLAGS.has(name))) {
        setFlag(name, true);
      } else {
        setFlag(name, next);
        i++;
      }
      continue;
    }
    // 短选项只认登记过的别名（当前只有 -o → output），其余保持原样
    const alias = arg.length === 2 && arg[0] === "-" ? SHORT_FLAGS[arg[1]] : undefined;
    if (alias) {
      const next = argv[i + 1];
      if (next === undefined || isOptionToken(next)) {
        flags[alias] = true;
      } else {
        flags[alias] = next;
        i++;
      }
      continue;
    }
    positional.push(arg);
  }
  return { positional, flags };
}

/** 取一个可重复 flag 的值：支持给多次，也支持一次给逗号分隔。 */
export function listFlag(flags: Flags, name: string): string[] {
  const raw = flags[name];
  if (raw === undefined) return [];
  const values: Array<string | true> = Array.isArray(raw) ? raw : [raw];
  const out: string[] = [];
  for (const value of values) {
    if (value === true || typeof value !== "string")
      throw new APIError(-1, `--${name} 需要值，例如 --${name} 1024`, 2);
    for (const part of value.split(",")) {
      const trimmed = part.trim();
      if (trimmed) out.push(trimmed);
    }
  }
  return out;
}

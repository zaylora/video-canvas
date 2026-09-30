export type JsonParseResult =
  | { ok: true; value: unknown }
  | { ok: false; message: string; line?: number; column?: number };

/** 解析编辑器里的 JSON，出错时尽量给出行列，方便定位 */
export function parseJsonText(text: string): JsonParseResult {
  if (text.trim() === "") return { ok: false, message: "内容为空" };
  try {
    return { ok: true, value: JSON.parse(text) as unknown };
  } catch (error) {
    const message = error instanceof Error ? error.message : "JSON 格式错误";
    // 优先用自己的扫描器定位：新版 V8 / JSC 的报错信息里常常没有位置，行列要靠它
    const at = locateJsonError(text);
    if (at !== null) return { ok: false, message, ...lineColumn(text, at) };
    // 扫描器认为合法而引擎报错（极少见）：退回解析报错信息里的位置
    const lineMatch = /line (\d+) column (\d+)/.exec(message);
    if (lineMatch) {
      return { ok: false, message, line: Number(lineMatch[1]), column: Number(lineMatch[2]) };
    }
    const posMatch = /position (\d+)/.exec(message);
    if (posMatch) return { ok: false, message, ...lineColumn(text, Number(posMatch[1])) };
    return { ok: false, message };
  }
}

/** 下标 → 行列（都从 1 开始） */
function lineColumn(text: string, index: number): { line: number; column: number } {
  const lines = text.slice(0, index).split("\n");
  return { line: lines.length, column: lines[lines.length - 1].length + 1 };
}

/**
 * 找 JSON 文本里第一处语法错误的下标；语法正确返回 null。
 * 一个严格的递归下降扫描（只认标准 JSON），与引擎无关，所以行列稳定。
 */
export function locateJsonError(text: string): number | null {
  const n = text.length;
  let i = 0;
  const skip = () => {
    while (i < n && " \t\n\r".includes(text[i])) i++;
  };
  const string = () => {
    if (text[i] !== '"') throw i;
    i++;
    while (i < n) {
      const c = text[i];
      if (c === '"') {
        i++;
        return;
      }
      if (c < " ") throw i;
      if (c === "\\") {
        const next = text[i + 1];
        if (next === "u") {
          if (!/^[0-9a-fA-F]{4}$/.test(text.slice(i + 2, i + 6))) throw i;
          i += 6;
        } else if (next !== undefined && '"\\/bfnrt'.includes(next)) i += 2;
        else throw i;
      } else i++;
    }
    throw n;
  };
  const value = (): void => {
    skip();
    const c = text[i];
    if (c === "{") {
      i++;
      skip();
      if (text[i] === "}") {
        i++;
        return;
      }
      for (;;) {
        skip();
        string();
        skip();
        if (text[i] !== ":") throw i;
        i++;
        value();
        skip();
        if (text[i] === ",") {
          i++;
          continue;
        }
        if (text[i] === "}") {
          i++;
          return;
        }
        throw i;
      }
    }
    if (c === "[") {
      i++;
      skip();
      if (text[i] === "]") {
        i++;
        return;
      }
      for (;;) {
        value();
        skip();
        if (text[i] === ",") {
          i++;
          continue;
        }
        if (text[i] === "]") {
          i++;
          return;
        }
        throw i;
      }
    }
    if (c === '"') return string();
    const number = /-?(0|[1-9]\d*)(\.\d+)?([eE][+-]?\d+)?/y;
    number.lastIndex = i;
    const hit = number.exec(text);
    if (hit) {
      i += hit[0].length;
      return;
    }
    for (const word of ["true", "false", "null"]) {
      if (text.startsWith(word, i)) {
        i += word.length;
        return;
      }
    }
    throw i;
  };
  try {
    value();
    skip();
    return i < n ? i : null;
  } catch (at) {
    return typeof at === "number" ? at : null;
  }
}

/** 格式化：2 空格缩进；不是合法 JSON 就返回错误 */
export function formatJsonText(
  text: string,
): { ok: true; text: string } | { ok: false; message: string } {
  const parsed = parseJsonText(text);
  return parsed.ok
    ? { ok: true, text: JSON.stringify(parsed.value, null, 2) }
    : { ok: false, message: parsed.message };
}

export const toJsonText = (value: unknown) => JSON.stringify(value ?? {}, null, 2);

/** 从正文里取 key（新建时 key 由正文决定） */
export function readConfigKey(value: unknown): string {
  if (typeof value === "object" && value !== null && "key" in value) {
    const key = (value as { key?: unknown }).key;
    return typeof key === "string" ? key.trim() : "";
  }
  return "";
}

/** 在 JSON 文本里的定位结果 */
export type JsonLocation = {
  /** 命中的键名在文本里的起始下标（含引号） */
  index: number;
  /** 命中长度（含引号） */
  length: number;
  /** 行号，从 1 开始 */
  line: number;
  /** 列号，从 1 开始 */
  column: number;
};

/**
 * 按校验问题的路径（如 channels[0].upstream_model、input_schema.prompt.type）在 JSON 文本里找位置：
 * 依次找每一段键名（数组下标跳过），命中最后一段；中途某段找不到就停在已命中的最深一段。一个都没命中返回 null。
 * 只是“大致定位到行”，重名键会命中第一个。
 */
export function findPathInJson(text: string, path: string): JsonLocation | null {
  const segments = path
    .replace(/\[(\d+)\]/g, ".$1")
    .split(".")
    .filter((segment) => segment !== "" && !/^\d+$/.test(segment));
  let from = 0;
  let found = -1;
  let length = 0;
  for (const segment of segments) {
    const needle = `"${segment}"`;
    const index = text.indexOf(needle, from);
    if (index < 0) break;
    found = index;
    length = needle.length;
    from = index + needle.length;
  }
  if (found < 0) return null;
  const before = text.slice(0, found).split("\n");
  return {
    index: found,
    length,
    line: before.length,
    column: before[before.length - 1].length + 1,
  };
}

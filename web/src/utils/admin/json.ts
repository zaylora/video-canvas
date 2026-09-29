export type JsonParseResult =
  | { ok: true; value: unknown }
  | { ok: false; message: string; line?: number; column?: number }

/** 解析编辑器里的 JSON，出错时尽量给出行列，方便定位 */
export function parseJsonText(text: string): JsonParseResult {
  if (text.trim() === '') return { ok: false, message: '内容为空' }
  try {
    return { ok: true, value: JSON.parse(text) as unknown }
  } catch (error) {
    const message = error instanceof Error ? error.message : 'JSON 格式错误'
    // V8: "... at position 12 (line 2 column 5)"；其他引擎只有 position
    const lineMatch = /line (\d+) column (\d+)/.exec(message)
    if (lineMatch) {
      return { ok: false, message, line: Number(lineMatch[1]), column: Number(lineMatch[2]) }
    }
    const posMatch = /position (\d+)/.exec(message)
    if (posMatch) {
      const before = text.slice(0, Number(posMatch[1]))
      const lines = before.split('\n')
      return { ok: false, message, line: lines.length, column: lines[lines.length - 1].length + 1 }
    }
    return { ok: false, message }
  }
}

/** 格式化：2 空格缩进；不是合法 JSON 就返回错误 */
export function formatJsonText(
  text: string,
): { ok: true; text: string } | { ok: false; message: string } {
  const parsed = parseJsonText(text)
  return parsed.ok
    ? { ok: true, text: JSON.stringify(parsed.value, null, 2) }
    : { ok: false, message: parsed.message }
}

export const toJsonText = (value: unknown) => JSON.stringify(value ?? {}, null, 2)

/** 从正文里取 key（新建时 key 由正文决定） */
export function readConfigKey(value: unknown): string {
  if (typeof value === 'object' && value !== null && 'key' in value) {
    const key = (value as { key?: unknown }).key
    return typeof key === 'string' ? key.trim() : ''
  }
  return ''
}

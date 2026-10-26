/** Go 下发的工具参数 schema（普通 JSON Schema，只用到这里处理的几个字段） */
export interface JsonSchema {
  type?: string;
  properties?: Record<string, JsonSchema>;
  items?: JsonSchema;
  [k: string]: unknown;
}

/**
 * 迁移前参数是 typebox 定义，它在校验前会把单个值包成数组（模型常把 nodeIds 写成一个字符串）；
 * Go 下发的是普通 JSON Schema，pi 不会这样做，所以在校验前补上，模型看到的行为不变。
 * 只处理标量：对象放在要求数组的地方不动，让校验照常报错。不修改入参。
 */
export function wrapScalarArrays(schema: JsonSchema | undefined, value: unknown): unknown {
  if (!schema || typeof schema !== "object") return value;
  if (schema.type === "array") {
    if (value === undefined || value === null) return value;
    if (!Array.isArray(value) && typeof value === "object") return value;
    const arr: unknown[] = Array.isArray(value) ? value : [value];
    return schema.items ? arr.map((v) => wrapScalarArrays(schema.items, v)) : arr;
  }
  if (schema.type === "object" && schema.properties && value && typeof value === "object" && !Array.isArray(value)) {
    const out: Record<string, unknown> = { ...value };
    for (const [k, s] of Object.entries(schema.properties)) if (k in out) out[k] = wrapScalarArrays(s, out[k]);
    return out;
  }
  return value;
}

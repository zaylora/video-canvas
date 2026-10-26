/**
 * 请求参数校验：规则和 Go 的绑定标签（binding / label）一一对应，错误提示的措辞和 validator 的中文翻译一致，
 * 多条用「；」连起来。前端会把 msg 原样显示给用户，所以措辞不能随意改。
 */

export interface FieldSpec {
  key: string;
  /** 提示里的字段名（Go 的 label 标签） */
  label: string;
  kind: "string" | "int" | "bool" | "strings" | "items" | "any";
  required?: boolean;
  min?: number;
  max?: number;
  oneof?: readonly string[];
  /** 字符串数组每一项的最大长度 */
  itemMax?: number;
  /** 对象数组每一项的字段 */
  item?: readonly FieldSpec[];
  /** int 字段可以是 null（Go 里的 *int） */
  nullable?: boolean;
}

export class ValidationError extends Error {}

const runes = (s: string) => Array.from(s).length;

export function validateBody(body: unknown, specs: readonly FieldSpec[]): Record<string, any> {
  if (typeof body !== "object" || body === null || Array.isArray(body)) throw new ValidationError("请求参数格式错误");
  const obj = body as Record<string, unknown>;
  // 类型不对是解析错误，先于字段校验报出（和 Go 的 JSON 解析一致）
  for (const s of specs) {
    const v = obj[s.key];
    if (v === undefined || v === null) continue;
    const bad =
      (s.kind === "string" && typeof v !== "string") ||
      (s.kind === "int" && !(typeof v === "number" && Number.isInteger(v))) ||
      (s.kind === "bool" && typeof v !== "boolean") ||
      (s.kind === "strings" && !(Array.isArray(v) && v.every((x) => typeof x === "string"))) ||
      (s.kind === "items" && !(Array.isArray(v) && v.every((x) => typeof x === "object" && x !== null && !Array.isArray(x))));
    if (bad) throw new ValidationError(`${s.key} 类型错误`);
  }
  const errors: string[] = [];
  const out: Record<string, any> = {};
  check(obj, specs, errors, out);
  if (errors.length) throw new ValidationError(errors.join("；"));
  return out;
}

function check(obj: Record<string, unknown>, specs: readonly FieldSpec[], errors: string[], out: Record<string, any>): void {
  for (const s of specs) {
    const v = obj[s.key];
    const missing = v === undefined || v === null;
    switch (s.kind) {
      case "string": {
        const str = missing ? "" : (v as string);
        if (s.required && str === "") errors.push(`${s.label}为必填字段`);
        else if (s.oneof && str !== "" && !s.oneof.includes(str)) errors.push(`${s.label}必须是[${s.oneof.join(" ")}]中的一个`);
        if (s.max !== undefined && runes(str) > s.max) errors.push(`${s.label}长度不能超过${s.max}个字符`);
        out[s.key] = str;
        break;
      }
      case "int": {
        if (missing || (s.required && v === 0)) {
          if (s.required) errors.push(`${s.label}为必填字段`);
          if (!missing) break; // required 的整数 0 等于没填（和 Go 的 validator 一致）
          out[s.key] = s.nullable ? null : 0;
          break;
        }
        const n = v as number;
        if (s.oneof && !s.oneof.includes(String(n))) errors.push(`${s.label}必须是[${s.oneof.join(" ")}]中的一个`);
        if (s.min !== undefined && n < s.min) errors.push(`${s.label}必须大于或等于${s.min}`);
        if (s.max !== undefined && n > s.max) errors.push(`${s.label}必须小于或等于${s.max}`);
        out[s.key] = n;
        break;
      }
      case "bool":
        out[s.key] = missing ? false : v;
        break;
      case "strings": {
        const arr = missing ? [] : (v as string[]);
        if (s.max !== undefined && arr.length > s.max) errors.push(`${s.label}最多包含${s.max}项`);
        if (s.itemMax !== undefined) arr.forEach((x, i) => runes(x) > s.itemMax! && errors.push(`${s.label}[${i}]长度不能超过${s.itemMax}个字符`));
        out[s.key] = arr;
        break;
      }
      case "items": {
        const arr = missing ? [] : (v as Record<string, unknown>[]);
        if (s.max !== undefined && arr.length > s.max) errors.push(`${s.label}最多包含${s.max}项`);
        out[s.key] = arr.map((x) => {
          const item: Record<string, any> = {};
          check(x, s.item ?? [], errors, item);
          return item;
        });
        break;
      }
      case "any":
        out[s.key] = missing ? undefined : v;
        break;
    }
  }
}

/** 查询参数里的整数：不是整数是「参数格式错误：xxx」，再按 min / max 校验 */
export function queryInt(q: URLSearchParams, key: string, label: string, min: number, max?: number): number {
  const raw = q.get(key);
  if (raw === null || raw === "") return 0;
  if (!/^-?\d+$/.test(raw)) throw new ValidationError(`参数格式错误：${raw}`);
  const n = Number(raw);
  if (n < min) throw new ValidationError(`${label}必须大于或等于${min}`);
  if (max !== undefined && n > max) throw new ValidationError(`${label}必须小于或等于${max}`);
  return n;
}

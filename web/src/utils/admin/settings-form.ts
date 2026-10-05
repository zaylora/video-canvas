import type { SettingSchema, SettingSpec, SettingType } from "@/api/admin/ai/type";

/**
 * 插件声明的设置项（meta.channelSettings / meta.import.args）→ 表单。
 * 表单里 string / number / enum 都存成字符串（number 输入框的原始文本），boolean 存布尔；
 * 提交时再按类型转换、校验必填。键顺序沿用声明对象的书写顺序。
 */

/** 表单里的一个字段 */
export type SettingField = {
  name: string;
  type: SettingType;
  label: string;
  description?: string;
  required: boolean;
  default?: unknown;
  options: string[];
};

export type SettingFormValue = string | boolean;
export type SettingFormValues = Record<string, SettingFormValue>;

const KNOWN_TYPES: readonly SettingType[] = ["string", "number", "boolean", "enum"];

const isRecord = (value: unknown): value is Record<string, unknown> =>
  typeof value === "object" && value !== null && !Array.isArray(value);

/** 声明对象 → 有序字段列表；坏条目跳过，未知类型按 string 处理（表单至少能填） */
export function settingFields(schema: SettingSchema | null | undefined): SettingField[] {
  if (!isRecord(schema)) return [];
  return Object.entries(schema).flatMap(([name, raw]) => {
    if (!isRecord(raw)) return [];
    const spec = raw as Partial<SettingSpec>;
    const type = KNOWN_TYPES.includes(spec.type as SettingType)
      ? (spec.type as SettingType)
      : "string";
    const options = Array.isArray(spec.options)
      ? spec.options.filter((item): item is string => typeof item === "string")
      : [];
    return [
      {
        name,
        type,
        label: typeof spec.label === "string" && spec.label ? spec.label : name,
        description: typeof spec.description === "string" ? spec.description : undefined,
        required: spec.required === true,
        default: spec.default,
        options,
      },
    ];
  });
}

/** 一个值能不能放进某类型的表单控件；放不进就当没有 */
function toFormValue(field: SettingField, value: unknown): SettingFormValue | undefined {
  switch (field.type) {
    case "boolean":
      return typeof value === "boolean" ? value : undefined;
    case "number":
      return typeof value === "number" && Number.isFinite(value) ? String(value) : undefined;
    case "enum":
      return typeof value === "string" && field.options.includes(value) ? value : undefined;
    default:
      return typeof value === "string"
        ? value
        : typeof value === "number"
          ? String(value)
          : undefined;
  }
}

/**
 * 表单初始值：已有取值（编辑渠道时）优先，其次声明的默认值，再次空值。
 * boolean 没有默认值时为 false；enum 没有默认值时为空串（等于没选）。
 */
export function initialSettingValues(
  fields: SettingField[],
  current?: Record<string, unknown> | null,
): SettingFormValues {
  const values: SettingFormValues = {};
  for (const field of fields) {
    const fromCurrent = isRecord(current) ? toFormValue(field, current[field.name]) : undefined;
    const fromDefault = toFormValue(field, field.default);
    values[field.name] = fromCurrent ?? fromDefault ?? (field.type === "boolean" ? false : "");
  }
  return values;
}

export type SettingValidation = {
  ok: boolean;
  /** 转换好、可以直接提交的取值；空着的可选项不出现 */
  values: Record<string, unknown>;
  /** 字段名 -> 错误文案 */
  errors: Record<string, string>;
};

/** 按声明校验并转换表单值：必填、数字格式、enum 取值 */
export function validateSettingValues(
  fields: SettingField[],
  form: SettingFormValues,
): SettingValidation {
  const values: Record<string, unknown> = {};
  const errors: Record<string, string> = {};
  for (const field of fields) {
    const raw = form[field.name];
    if (field.type === "boolean") {
      values[field.name] = raw === true;
      continue;
    }
    const text = typeof raw === "string" ? raw.trim() : "";
    if (text === "") {
      if (field.required) errors[field.name] = `请填写${field.label}`;
      continue;
    }
    if (field.type === "number") {
      const num = Number(text);
      if (!Number.isFinite(num)) {
        errors[field.name] = `${field.label}必须是数字`;
        continue;
      }
      values[field.name] = num;
      continue;
    }
    if (field.type === "enum" && !field.options.includes(text)) {
      errors[field.name] = `${field.label}只能是：${field.options.join(" / ")}`;
      continue;
    }
    values[field.name] = text;
  }
  return { ok: Object.keys(errors).length === 0, values, errors };
}

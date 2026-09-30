import type { InputFieldSchema, InputPortType, InputSchema } from "@/api/model/type";
import type { CanvasNodeData, NodeKind } from "@/types";

/** input_schema 字段 + 字段名，按 schema 对象的键顺序排列（也就是渲染顺序） */
export type SchemaField = InputFieldSchema & { name: string };

export const schemaFields = (schema: InputSchema | undefined): SchemaField[] =>
  Object.entries(schema ?? {}).map(([name, field]) => ({ ...field, name }));

export const isMediaFieldType = (type: InputFieldSchema["type"]) =>
  type === "image" || type === "video" || type === "audio";

/** 能从上游连线取值的字段，也就是节点上要开输入口的那些 */
export const portFields = (schema: InputSchema | undefined): SchemaField[] =>
  schemaFields(schema).filter((field) => !!field.port);

/** 上游节点种类能提供哪种端口的值 */
export const PORT_OF_KIND: Record<NodeKind, InputPortType> = {
  script: "text",
  image: "image",
  video: "video",
  audio: "audio",
};

/** 连到本节点的一根上游连线，以及上游节点此刻能提供的东西 */
export type IncomingLink = {
  edgeId: string;
  sourceId: string;
  sourceKind: NodeKind;
  sourceLabel: string;
  /** 连线落在哪个输入口；null 表示落在节点上没指定口 */
  targetHandle: string | null;
  /** 上游素材 id（图片 / 视频 / 音频节点） */
  assetId?: string;
  /** 上游文本（文本节点的正文，没生成过就取它写的提示词） */
  text?: string;
};

/**
 * 决定每个有 port 的字段由哪根上游连线提供：
 * 先认落在具名输入口且类型对得上的线，剩下的线按种类落到第一个还空着的同类型输入口。
 * 同一个口有多根线时取排在前面的那根。
 */
export function resolveBindings(
  schema: InputSchema | undefined,
  links: IncomingLink[],
): Record<string, IncomingLink> {
  const ports = portFields(schema);
  const bound: Record<string, IncomingLink> = {};
  const used = new Set<string>();

  for (const link of links) {
    const field = ports.find(
      (item) =>
        item.name === link.targetHandle &&
        item.port === PORT_OF_KIND[link.sourceKind] &&
        !bound[item.name],
    );
    if (field) {
      bound[field.name] = link;
      used.add(link.edgeId);
    }
  }
  for (const link of links) {
    if (used.has(link.edgeId)) continue;
    const field = ports.find(
      (item) => item.port === PORT_OF_KIND[link.sourceKind] && !bound[item.name],
    );
    if (field) {
      bound[field.name] = link;
      used.add(link.edgeId);
    }
  }
  return bound;
}

/**
 * 让连线的落点和绑定结果一致：落在空口 / 已经不存在的口上的线，改挂到实际绑定的输入口，
 * 没有可绑定输入口的线改成默认口（null），免得 xyflow 找不到 handle 把线藏掉。
 * 返回 edgeId -> 新的 targetHandle，没有要改的就是空对象。
 */
export function computeHandleFixes(
  schema: InputSchema | undefined,
  links: IncomingLink[],
): Record<string, string | null> {
  const ports = portFields(schema);
  if (ports.length === 0) return {};
  const bindings = resolveBindings(schema, links);
  const bindingByEdge = new Map(
    Object.entries(bindings).map(([name, link]) => [link.edgeId, name]),
  );
  const names = new Set(ports.map((field) => field.name));
  const fixes: Record<string, string | null> = {};
  for (const link of links) {
    const target = bindingByEdge.get(link.edgeId);
    if (target !== undefined) {
      if (link.targetHandle !== target) fixes[link.edgeId] = target;
    } else if (link.targetHandle !== null && !names.has(link.targetHandle)) {
      fixes[link.edgeId] = null;
    }
  }
  return fixes;
}

/** 提示词兼容：旧节点只有 data.prompt，新节点提示词放 params.prompt */
export function readParams(
  data: Pick<CanvasNodeData, "params" | "prompt">,
): Record<string, unknown> {
  const params = { ...data.params };
  if (params.prompt === undefined && data.prompt !== undefined) params.prompt = data.prompt;
  return params;
}

const isEmptyValue = (value: unknown) =>
  value === undefined || value === null || (typeof value === "string" && value.trim() === "");

/** 参数值：用户填的，没填就取 schema 默认值 */
export function effectiveValue(
  field: InputFieldSchema,
  params: Record<string, unknown>,
  name: string,
) {
  const value = params[name];
  return value === undefined ? field.default : value;
}

const optionMatch = (field: InputFieldSchema, value: unknown) =>
  field.options?.find((option) => String(option.value) === String(value));

/** 把 assetId（前端存字符串）转成后端要的数字；转不成就返回 null */
export function toAssetNumber(value: unknown): number | null {
  if (typeof value === "number") return Number.isFinite(value) && value > 0 ? value : null;
  if (typeof value === "string" && value.trim() !== "") {
    const n = Number(value.trim());
    return Number.isFinite(n) && n > 0 ? n : null;
  }
  return null;
}

export type BuiltInput = {
  /** 可以直接放进 POST /generation-tasks 的 input */
  input: Record<string, unknown>;
  /** 字段名 -> 该标红的原因；空表示可以提交 */
  errors: Record<string, string>;
};

/**
 * 按 input_schema 组装提交用的 input 并校验：
 * 有 port 且已连线的字段取上游（连线优先于手填），其余取参数值或默认值；
 * 媒体字段的值是素材 assetId（数字），不是 URL。
 */
export function buildTaskInput(
  schema: InputSchema | undefined,
  params: Record<string, unknown>,
  bindings: Record<string, IncomingLink> = {},
): BuiltInput {
  const input: Record<string, unknown> = {};
  const errors: Record<string, string> = {};

  for (const field of schemaFields(schema)) {
    const { name, label, type } = field;
    const link = bindings[name];

    if (link) {
      if (isMediaFieldType(type)) {
        const id = toAssetNumber(link.assetId);
        if (id === null) {
          errors[name] = `上游「${link.sourceLabel}」还没有可用的素材`;
        } else input[name] = id;
        continue;
      }
      if (type === "text") {
        const text = link.text?.trim();
        if (text) {
          if (field.max_length && [...text].length > field.max_length) {
            errors[name] = `${label}不能超过 ${field.max_length} 个字`;
          } else input[name] = text;
          continue;
        }
        // 上游还没有文字：必填就报错，选填退回手填的值
        if (field.required && isEmptyValue(effectiveValue(field, params, name))) {
          errors[name] = `上游「${link.sourceLabel}」还没有文字`;
          continue;
        }
      }
    }

    const value = effectiveValue(field, params, name);
    if (isEmptyValue(value)) {
      if (field.required && type !== "boolean") {
        errors[name] = isMediaFieldType(type) ? `请上传或选择${label}` : `请填写${label}`;
      }
      continue;
    }

    switch (type) {
      case "text": {
        const text = String(value);
        if (field.max_length && [...text].length > field.max_length) {
          errors[name] = `${label}不能超过 ${field.max_length} 个字`;
        } else input[name] = text;
        break;
      }
      case "number": {
        const n = typeof value === "number" ? value : Number(value);
        if (!Number.isFinite(n)) errors[name] = `${label}需要是数字`;
        else if (field.min !== undefined && n < field.min)
          errors[name] = `${label}不能小于 ${field.min}`;
        else if (field.max !== undefined && n > field.max)
          errors[name] = `${label}不能大于 ${field.max}`;
        else input[name] = n;
        break;
      }
      case "enum": {
        const option = optionMatch(field, value);
        if (!option) errors[name] = `请重新选择${label}`;
        else input[name] = option.value;
        break;
      }
      case "boolean":
        input[name] = value === true || value === "true";
        break;
      default: {
        const id = toAssetNumber(value);
        if (id === null) errors[name] = `请重新上传或选择${label}`;
        else input[name] = id;
      }
    }
  }
  return { input, errors };
}

/** 某个参数值在新字段定义下还成不成立 */
function valueFits(field: InputFieldSchema, value: unknown): boolean {
  switch (field.type) {
    case "enum":
      return !!optionMatch(field, value);
    case "number": {
      const n = typeof value === "number" ? value : Number(value);
      return (
        Number.isFinite(n) &&
        (field.min === undefined || n >= field.min) &&
        (field.max === undefined || n <= field.max)
      );
    }
    case "boolean":
      return typeof value === "boolean";
    case "text":
      return typeof value === "string";
    default:
      return toAssetNumber(value) !== null;
  }
}

export type SwitchedParams = {
  /** 切换后保留下来的参数 */
  params: Record<string, unknown>;
  /** 被丢弃的、用户确实填过内容的字段名 */
  droppedNames: string[];
  /** 被丢弃字段在旧模型里的名字（label），用来提示 */
  droppedLabels: string[];
};

/**
 * 切换模型：保留同名同类型（且取值在新定义下仍成立）的参数，其余丢弃。
 * 只把用户真填过内容的丢弃项报出来，空值静默丢掉。
 */
export function switchModelParams(
  oldSchema: InputSchema | undefined,
  newSchema: InputSchema | undefined,
  params: Record<string, unknown>,
): SwitchedParams {
  const kept: Record<string, unknown> = {};
  const droppedNames: string[] = [];
  const droppedLabels: string[] = [];
  for (const [name, value] of Object.entries(params)) {
    const next = newSchema?.[name];
    const prev = oldSchema?.[name];
    if (next && (!prev || prev.type === next.type) && valueFits(next, value)) {
      kept[name] = value;
    } else if (!isEmptyValue(value)) {
      droppedNames.push(name);
      droppedLabels.push(prev?.label ?? name);
    }
  }
  return { params: kept, droppedNames, droppedLabels };
}

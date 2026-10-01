import type {
  Capabilities,
  GenerationOp,
  ParamField,
  ParamOption,
  RefKind,
} from "@/api/model/type";
import type { CanvasNodeData, NodeKind } from "@/types";

/** 参数 + 参数名，按 capabilities.params 的键顺序排列（也就是参数面板的显示顺序） */
export type ParamEntry = ParamField & { name: string };

export const paramEntries = (caps: Capabilities | undefined): ParamEntry[] =>
  Object.entries(caps?.params ?? {}).map(([name, field]) => ({ ...field, name }));

/** 开放给用户、会出现在画布参数面板里的参数 */
export const openParams = (caps: Capabilities | undefined): ParamEntry[] =>
  paramEntries(caps).filter((field) => field.open);

/** 参考素材在任务输入里的键，以及它对应的素材种类 */
export const REF_KEYS: Array<{
  key: "images" | "videos" | "audios";
  kind: RefKind;
  label: string;
}> = [
  { key: "images", kind: "image", label: "参考图片" },
  { key: "videos", kind: "video", label: "参考视频" },
  { key: "audios", kind: "audio", label: "参考音频" },
];

export type RefKey = (typeof REF_KEYS)[number]["key"];

/** 生成方式的中文名 */
export const OP_LABEL: Record<GenerationOp, string> = {
  t2v: "文生视频",
  i2v: "图生视频",
  omni: "全能参考",
  t2i: "文生图",
  i2i: "图生图",
};

/** 各生成方式能引用的素材种类：文生不引用，图生只引用图片，全能参考不限类型（以 refs 里开启的为准） */
const REF_KINDS_OF_OP: Record<GenerationOp, RefKind[]> = {
  t2v: [],
  t2i: [],
  i2v: ["image"],
  i2i: ["image"],
  omni: ["image", "video", "audio"],
};

/** 当前生成方式：节点里选过且仍在模型支持的列表里就用它，否则取第一种；文本、音频模型没有生成方式 */
export function currentOp(
  caps: Capabilities | undefined,
  params: Record<string, unknown>,
): GenerationOp | undefined {
  const ops = caps?.ops ?? [];
  const picked = params.op;
  return ops.find((op) => op === picked) ?? ops[0];
}

/** 当前生成方式下，节点能接收的素材种类（已过 refs 开关） */
export function refKindsOf(
  caps: Capabilities | undefined,
  op: GenerationOp | undefined,
): RefKind[] {
  if (!caps || !op) return [];
  return REF_KINDS_OF_OP[op].filter((kind) => caps.refs?.[kind]?.on);
}

/** 上游节点种类能提供哪种东西：文本节点给文字，其余给对应种类的素材 */
export const PORT_OF_KIND: Record<NodeKind, "text" | RefKind> = {
  script: "text",
  image: "image",
  video: "video",
  audio: "audio",
};

/** 节点上的一个输入口：提示词口 + 每种可接收的素材一个口 */
export type InputPort = { id: "prompt" | RefKey; label: string; port: "text" | RefKind };

export function inputPorts(caps: Capabilities | undefined, op: GenerationOp | undefined) {
  const ports: InputPort[] = [{ id: "prompt", label: "提示词", port: "text" }];
  const kinds = refKindsOf(caps, op);
  for (const ref of REF_KEYS) {
    if (kinds.includes(ref.kind)) ports.push({ id: ref.key, label: ref.label, port: ref.kind });
  }
  return ports;
}

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

/** 每个输入口由哪些上游连线提供：提示词口取第一根，素材口可以接多根 */
export type Bindings = {
  prompt?: IncomingLink;
  images: IncomingLink[];
  videos: IncomingLink[];
  audios: IncomingLink[];
};

const emptyBindings = (): Bindings => ({ images: [], videos: [], audios: [] });

/**
 * 决定每个输入口由哪些上游连线提供：
 * 先认落在具名输入口且类型对得上的线，剩下的线按种类落到第一个同类型输入口。
 * 当前生成方式不接收的素材（含关闭的类型）不绑定，连线保留但不参与提交。
 */
export function resolveBindings(
  caps: Capabilities | undefined,
  op: GenerationOp | undefined,
  links: IncomingLink[],
): Bindings {
  const ports = inputPorts(caps, op);
  const out = emptyBindings();
  const put = (port: InputPort, link: IncomingLink) => {
    if (port.id === "prompt") out.prompt ??= link;
    else out[port.id].push(link);
  };
  const rest: IncomingLink[] = [];
  for (const link of links) {
    const port = ports.find(
      (item) => item.id === link.targetHandle && item.port === PORT_OF_KIND[link.sourceKind],
    );
    if (port) put(port, link);
    else rest.push(link);
  }
  for (const link of rest) {
    const port = ports.find((item) => item.port === PORT_OF_KIND[link.sourceKind]);
    if (port) put(port, link);
  }
  return out;
}

/** 绑定结果里被占用的连线 id -> 它实际落在的输入口 */
function boundHandles(bindings: Bindings): Map<string, string> {
  const map = new Map<string, string>();
  if (bindings.prompt) map.set(bindings.prompt.edgeId, "prompt");
  for (const ref of REF_KEYS) for (const link of bindings[ref.key]) map.set(link.edgeId, ref.key);
  return map;
}

/**
 * 让连线的落点和绑定结果一致：落在空口 / 已经不存在的口上的线，改挂到实际绑定的输入口；
 * 当前方式不接收的线改成默认口（null），免得 xyflow 找不到 handle 把线藏掉。
 * 返回 edgeId -> 新的 targetHandle，没有要改的就是空对象。
 */
export function computeHandleFixes(
  caps: Capabilities | undefined,
  op: GenerationOp | undefined,
  links: IncomingLink[],
): Record<string, string | null> {
  if (!caps) return {};
  const bound = boundHandles(resolveBindings(caps, op, links));
  const names = new Set<string>(inputPorts(caps, op).map((port) => port.id));
  const fixes: Record<string, string | null> = {};
  for (const link of links) {
    const target = bound.get(link.edgeId);
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

/** 参数值：用户填的，没填就取默认值 */
export function effectiveValue(field: ParamField, params: Record<string, unknown>, name: string) {
  const value = params[name];
  return value === undefined ? field.default : value;
}

/** 在 enum 的可选值里找 value（数字与字符串写法互认），找不到返回 undefined */
export const matchOption = (field: ParamField, value: unknown): ParamOption | undefined =>
  field.options?.find((option) => String(option) === String(value));

/** 把 assetId（前端存字符串）转成后端要的数字；转不成就返回 null */
export function toAssetNumber(value: unknown): number | null {
  if (typeof value === "number") return Number.isFinite(value) && value > 0 ? value : null;
  if (typeof value === "string" && value.trim() !== "") {
    const n = Number(value.trim());
    return Number.isFinite(n) && n > 0 ? n : null;
  }
  return null;
}

/** 手动添加的参考素材 id（节点 params.images / videos / audios 里存的数组），转成去重的数字 */
export function manualRefs(params: Record<string, unknown>, key: RefKey): number[] {
  const raw = params[key];
  if (!Array.isArray(raw)) return [];
  const ids = raw.map(toAssetNumber).filter((id): id is number => id !== null);
  return [...new Set(ids)];
}

export type BuiltInput = {
  /** 可以直接放进 POST /generation-tasks 的 input */
  input: Record<string, unknown>;
  /** 输入名（prompt / op / images … / 参数名）-> 该标红的原因；空表示可以提交 */
  errors: Record<string, string>;
};

/** 某个参数值在字段定义下是否成立；成立返回规范化后的值，否则返回错误文案 */
function checkParam(field: ParamField, value: unknown): { value: unknown } | { error: string } {
  switch (field.type) {
    case "enum": {
      const option = matchOption(field, value);
      return option === undefined ? { error: `请重新选择${field.label}` } : { value: option };
    }
    case "number": {
      const n = typeof value === "number" ? value : Number(value);
      if (!Number.isFinite(n) || !Number.isInteger(n)) return { error: `${field.label}需要是整数` };
      if (field.min !== undefined && n < field.min)
        return { error: `${field.label}不能小于 ${field.min}` };
      if (field.max !== undefined && n > field.max)
        return { error: `${field.label}不能大于 ${field.max}` };
      return { value: n };
    }
    default:
      return { value: value === true || value === "true" };
  }
}

/**
 * 按模型能力组装提交用的 input 并校验：
 * 提示词（上游文字优先于手填）、生成方式、开放给用户的参数（没填取默认值），
 * 以及参考素材：当前方式允许的种类里，上游连线 + 手动添加的素材合并去重后按 refs 上限和方式要求检查。
 * 未开放的参数不放进 input，后端按默认值补齐。素材值是素材 assetId（数字），不是 URL。
 */
export function buildTaskInput(
  caps: Capabilities | undefined,
  params: Record<string, unknown>,
  bindings: Bindings = emptyBindings(),
): BuiltInput {
  const input: Record<string, unknown> = {};
  const errors: Record<string, string> = {};
  if (!caps) return { input, errors };
  const op = currentOp(caps, params);
  const maxLength = caps.prompt?.max_length ?? 0;

  // 提示词
  const link = bindings.prompt;
  const upstreamText = link?.text?.trim();
  const typed = typeof params.prompt === "string" ? params.prompt.trim() : "";
  const prompt = upstreamText || typed;
  if (!prompt) {
    errors.prompt = link ? `上游「${link.sourceLabel}」还没有文字` : "请填写提示词";
  } else if (maxLength > 0 && [...prompt].length > maxLength) {
    errors.prompt = `提示词不能超过 ${maxLength} 个字`;
  } else input.prompt = prompt;

  if (op) input.op = op;

  // 生成参数：只提交开放给用户的
  for (const field of openParams(caps)) {
    const value = effectiveValue(field, params, field.name);
    if (isEmptyValue(value)) {
      if (field.type !== "boolean") errors[field.name] = `请填写${field.label}`;
      else input[field.name] = false;
      continue;
    }
    const checked = checkParam(field, value);
    if ("error" in checked) errors[field.name] = checked.error;
    else input[field.name] = checked.value;
  }

  // 参考素材
  const kinds = refKindsOf(caps, op);
  let total = 0;
  for (const ref of REF_KEYS) {
    if (!kinds.includes(ref.kind)) continue;
    const ids: number[] = [];
    for (const item of bindings[ref.key]) {
      const id = toAssetNumber(item.assetId);
      if (id === null) {
        errors[ref.key] ??= `上游「${item.sourceLabel}」还没有可用的素材`;
      } else ids.push(id);
    }
    const all = [...new Set([...ids, ...manualRefs(params, ref.key)])];
    const max = caps.refs[ref.kind].max;
    if (all.length > max) errors[ref.key] ??= `${ref.label}最多 ${max} 个，请移除多余的`;
    if (all.length > 0) input[ref.key] = all;
    total += all.length;
  }
  if ((op === "i2v" || op === "i2i") && !input.images && !errors.images)
    errors.images = "图生方式需要至少 1 张参考图片";
  if (op === "omni" && total === 0 && !Object.keys(errors).some((k) => k in REF_INDEX))
    errors.images = "全能参考需要至少 1 个参考素材";
  return { input, errors };
}

const REF_INDEX: Record<string, true> = { images: true, videos: true, audios: true };

/** 某个参数值在新字段定义下还成不成立 */
function valueFits(field: ParamField, value: unknown): boolean {
  if (field.type === "boolean") return typeof value === "boolean";
  return "value" in checkParam(field, value);
}

export type SwitchedParams = {
  /** 切换后保留下来的参数 */
  params: Record<string, unknown>;
  /** 被丢弃的、用户确实填过内容的参数名 */
  droppedNames: string[];
  /** 被丢弃参数在旧模型里的名字（label），用来提示 */
  droppedLabels: string[];
};

/**
 * 切换模型：保留提示词；生成方式在新模型仍支持时保留；手动添加的参考素材在新方式允许该种类时保留；
 * 其余生成参数保留同名同类型且取值在新定义下仍成立的，剩下的丢弃。
 * 只把用户真填过内容的丢弃项报出来，空值静默丢掉。
 */
export function switchModelParams(
  oldCaps: Capabilities | undefined,
  newCaps: Capabilities | undefined,
  params: Record<string, unknown>,
): SwitchedParams {
  const kept: Record<string, unknown> = {};
  const droppedNames: string[] = [];
  const droppedLabels: string[] = [];
  const drop = (name: string, label: string) => {
    droppedNames.push(name);
    droppedLabels.push(label);
  };
  const newOp = currentOp(newCaps, params);
  const allowedKinds = new Set(refKindsOf(newCaps, newOp));
  const refLabel = (key: string) => REF_KEYS.find((ref) => ref.key === key)?.label ?? key;

  for (const [name, value] of Object.entries(params)) {
    if (name === "prompt") {
      kept[name] = value;
    } else if (name === "op") {
      if (newCaps?.ops?.includes(value as GenerationOp)) kept[name] = value;
    } else if (name in REF_INDEX) {
      const ref = REF_KEYS.find((item) => item.key === name)!;
      if (allowedKinds.has(ref.kind)) kept[name] = value;
      else if (manualRefs(params, ref.key).length > 0) drop(name, refLabel(name));
    } else {
      const next = newCaps?.params?.[name];
      const prev = oldCaps?.params?.[name];
      if (next && (!prev || prev.type === next.type) && valueFits(next, value)) kept[name] = value;
      else if (!isEmptyValue(value)) drop(name, prev?.label ?? name);
    }
  }
  return { params: kept, droppedNames, droppedLabels };
}

/** 参数摘要，如「Auto · 720P · 5秒 · 声音开」：开放给用户的参数当前取值，按书写顺序 */
export function paramSummary(caps: Capabilities | undefined, params: Record<string, unknown>) {
  return openParams(caps)
    .map((field) => {
      const value = effectiveValue(field, params, field.name);
      if (field.type === "boolean") return `${field.label}${value === true ? "开" : "关"}`;
      return value === undefined || value === null ? "" : `${value}${field.unit ?? ""}`;
    })
    .filter(Boolean)
    .join(" · ");
}

/**
 * 计价用的规格：生成方式、是否有参考视频、所有参数的取值（开放的取用户值，未开放的取默认值，和后端补齐的一致）、
 * 提示词字数。input 是 buildTaskInput 组装出的提交输入。画布拿不到固定系统提示，Token 预估因此略低于后端。
 */
export function priceSpecOf(caps: Capabilities | undefined, input: Record<string, unknown>) {
  const values: Record<string, unknown> = {};
  for (const field of paramEntries(caps)) {
    values[field.name] =
      field.open && input[field.name] !== undefined ? input[field.name] : field.default;
  }
  const videos = input.videos;
  return {
    op: typeof input.op === "string" ? input.op : undefined,
    refVideo: Array.isArray(videos) && videos.length > 0,
    params: values,
    promptChars: typeof input.prompt === "string" ? [...input.prompt].length : 0,
  };
}

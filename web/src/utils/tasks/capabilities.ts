import type {
  Capabilities,
  GenerationOp,
  ParamField,
  ParamOption,
  RefKind,
  RefSpec,
} from "@/api/model/type";
import type { CanvasNodeData, NodeKind } from "@/types";
import { findPreset } from "@/constants/presets";
import { expandPrompt, promptPresets } from "@/utils/canvas/prompt-tokens";

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

/**
 * 模型是否自动切换生成方式：同时支持文生图和图生图时，不让用户选，由有没有引用图片决定。
 * 视频的图生、全能参考靠用户判断，不在此列。
 */
export function isAutoOp(caps: Capabilities | undefined): boolean {
  const ops = caps?.ops ?? [];
  return ops.includes("t2i") && ops.includes("i2i");
}

/**
 * 当前生成方式。自动切换的模型：有图片引用（hasImageRef）就是图生图，没有就是文生图，不看节点里存的 op；
 * 其它模型：节点里选过且仍在模型支持的列表里就用它，否则取第一种；文本、音频模型没有生成方式。
 */
export function currentOp(
  caps: Capabilities | undefined,
  params: Record<string, unknown>,
  hasImageRef = false,
): GenerationOp | undefined {
  if (isAutoOp(caps)) return hasImageRef ? "i2i" : "t2i";
  const ops = caps?.ops ?? [];
  const picked = params.op;
  return ops.find((op) => op === picked) ?? ops[0];
}

/**
 * 摆素材口、判断能不能连图片时用的生成方式：自动切换的模型始终按图生图算，
 * 否则文生图状态下既看不到参考图入口，也没法拉第一根图片线进来。
 */
export function refPanelOp(
  caps: Capabilities | undefined,
  op: GenerationOp | undefined,
): GenerationOp | undefined {
  return isAutoOp(caps) ? "i2i" : op;
}

/** 当前生成方式下，节点能接收的素材种类（已过 refs 开关） */
export function refKindsOf(
  caps: Capabilities | undefined,
  op: GenerationOp | undefined,
): RefKind[] {
  if (!caps || !op) return [];
  return REF_KINDS_OF_OP[op].filter((kind) => caps.refs?.[kind]?.on);
}

/**
 * 上游节点种类能提供哪些东西：文本节点给文字，图片 / 视频 / 音频节点给对应种类的素材，
 * 角色卡同时给图片（形象）和音频（声音）；导演台什么都不提供。
 */
export const PORTS_OF_KIND: Record<NodeKind, ReadonlyArray<"text" | RefKind>> = {
  script: ["text"],
  image: ["image"],
  video: ["video"],
  audio: ["audio"],
  director: [],
  character: ["image", "audio"],
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

/**
 * 角色卡节点此刻的引用内容（来自角色卡 store 的最新读取）：素材 id 是前端的字符串 id。
 * 提交任务时据此展开成参考图和参考音频。
 */
export type CharacterRefs = {
  /** 角色卡 ID */
  id: string;
  /** 角色名，用于提示 */
  name: string;
  /** 主图素材 ID */
  avatarAssetId: string;
  /** 补充图素材 ID，按保存顺序 */
  extraAssetIds: string[];
  /** 声音素材 ID；没有声音为 null */
  voiceAssetId: string | null;
};

/** 一根展开出来的虚拟连线是角色卡的哪一部分 */
export type CharacterPart = {
  /** 角色卡 ID */
  id: string;
  /** avatar 主图；extra 补充图；voice 声音 */
  part: "avatar" | "extra" | "voice";
  /** 同一部分里的序号（补充图第几张），从 0 起 */
  index: number;
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
  /** 角色卡节点引用的角色卡 ID（sourceKind 为 character 时有，缺省是旧数据） */
  characterId?: string;
  /** 角色卡节点的当前内容：缺省表示还在加载，null 表示角色卡已失效，两者都不参与提交 */
  character?: CharacterRefs | null;
  /** 只在 resolveBindings 展开出来的虚拟连线上有：它是角色卡的哪一部分 */
  fromCharacter?: CharacterPart;
};

/** 每个输入口由哪些上游连线提供：提示词口取第一根，素材口可以接多根 */
export type Bindings = {
  prompt?: IncomingLink;
  images: IncomingLink[];
  videos: IncomingLink[];
  audios: IncomingLink[];
  /** 超出参考上限、按顺序被截掉的角色卡素材（虚拟连线） */
  dropped: IncomingLink[];
  /** 目标不收参考音频，被丢弃的角色卡声音（虚拟连线） */
  ignored: IncomingLink[];
  /** 摆给用户看的非阻断提示：声音未生效、第 N 张起未带入…… */
  notices: string[];
};

export const emptyBindings = (): Bindings => ({
  images: [],
  videos: [],
  audios: [],
  dropped: [],
  ignored: [],
  notices: [],
});

/** 角色卡展开成的虚拟连线：沿用原连线的 id（断线、定位都认它），带上素材 id 和来源部分 */
function characterLinks(link: IncomingLink, refs: CharacterRefs) {
  const virtual = (assetId: string, part: CharacterPart["part"], index: number): IncomingLink => ({
    ...link,
    character: undefined,
    assetId,
    fromCharacter: { id: refs.id, part, index },
  });
  return {
    images: [
      virtual(refs.avatarAssetId, "avatar", 0),
      ...refs.extraAssetIds.map((assetId, index) => virtual(assetId, "extra", index)),
    ],
    voice: refs.voiceAssetId ? virtual(refs.voiceAssetId, "voice", 0) : null,
  };
}

/**
 * 决定每个输入口由哪些上游连线提供：
 * 先认落在具名输入口且类型对得上的线，剩下的线按种类落到第一个同类型输入口。
 * 当前生成方式不接收的素材（含关闭的类型）不绑定，连线保留但不参与提交。
 * 角色卡连线展开成虚拟连线：主图在前、补充图随后进 images，声音进 audios；
 * 失效 / 加载中的角色卡跳过；超出参考图 / 参考音频上限的按顺序截断（普通连线先占名额），
 * 目标不收参考音频时声音丢弃，两种情况都在 notices 里给出提示，不阻断提交。
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
    if (link.sourceKind === "character") continue;
    const port = ports.find(
      (item) => item.id === link.targetHandle && PORTS_OF_KIND[link.sourceKind].includes(item.port),
    );
    if (port) put(port, link);
    else rest.push(link);
  }
  for (const link of rest) {
    const port = ports.find((item) => PORTS_OF_KIND[link.sourceKind].includes(item.port));
    if (port) put(port, link);
  }

  const imagePort = ports.some((item) => item.id === "images");
  const audioPort = ports.some((item) => item.id === "audios");
  if (!imagePort && !audioPort) return out;
  const virtualImages: IncomingLink[] = [];
  const virtualVoices: IncomingLink[] = [];
  for (const link of links) {
    if (link.sourceKind !== "character" || !link.character) continue;
    const parts = characterLinks(link, link.character);
    if (imagePort) virtualImages.push(...parts.images);
    if (parts.voice) virtualVoices.push(parts.voice);
  }

  // 参考图：普通连线先占名额，角色卡的图按顺序填剩下的，放不下的截掉
  if (imagePort && caps) {
    const max = caps.refs?.image?.max ?? 0;
    const room = Math.max(0, max - out.images.length);
    out.images.push(...virtualImages.slice(0, room));
    out.dropped.push(...virtualImages.slice(room));
    if (virtualImages.length > room) {
      out.notices.push(`参考图最多 ${max} 张，第 ${max + 1} 张起未带入`);
    }
  }
  // 声音：目标不收参考音频就丢弃；收的话同样按上限截断
  if (virtualVoices.length > 0 && imagePort) {
    if (!audioPort) {
      out.ignored.push(...virtualVoices);
      out.notices.push("当前模型不支持参考音频，声音未生效");
    } else if (caps) {
      const max = caps.refs?.audio?.max ?? 0;
      const room = Math.max(0, max - out.audios.length);
      out.audios.push(...virtualVoices.slice(0, room));
      out.dropped.push(...virtualVoices.slice(room));
      if (virtualVoices.length > room) {
        out.notices.push(`参考音频最多 ${max} 条，多出的声音未带入`);
      }
    }
  } else if (virtualVoices.length > 0 && audioPort && caps) {
    // 只收音频不收图片的方式：声音照常带入
    const max = caps.refs?.audio?.max ?? 0;
    const room = Math.max(0, max - out.audios.length);
    out.audios.push(...virtualVoices.slice(0, room));
    out.dropped.push(...virtualVoices.slice(room));
  }
  return out;
}

/**
 * 能当「图片引用」的上游连线：图片节点本身，以及有主图的角色卡（取主图一张，够判断「有没有图片引用」）。
 * 生成方式自动切换的模型据此决定是图生图还是文生图，要在 resolveBindings 之前算。
 */
export function imageLinksOf(links: IncomingLink[]): IncomingLink[] {
  return links.flatMap((link) => {
    if (link.sourceKind === "image") return [link];
    if (link.sourceKind === "character" && link.character) {
      return [characterLinks(link, link.character).images[0]];
    }
    return [];
  });
}

/**
 * 节点只有一个输入口（设计稿：左侧一个 ⊕），连线不再挂在具名口上：
 * 旧画布里挂在「提示词 / 图片…」口上的线一律改回默认口（null），用途由 resolveBindings 按上游种类决定。
 * 返回 edgeId -> null，没有要改的就是空对象。
 */
export function legacyHandleFixes(links: IncomingLink[]): Record<string, null> {
  const fixes: Record<string, null> = {};
  for (const link of links) if (link.targetHandle !== null) fixes[link.edgeId] = null;
  return fixes;
}

/**
 * 按当前模型和生成方式，本节点收不收这种上游：文字总是进提示词，
 * 素材要当前方式收这种素材（比如「文生视频」不收图片）。模型清单还没到时不拦，只看种类规则。
 */
export function acceptsSourceKind(
  caps: Capabilities | undefined,
  op: GenerationOp | undefined,
  sourceKind: NodeKind,
): boolean {
  if (!caps) return true;
  const ports = PORTS_OF_KIND[sourceKind];
  const kinds = refKindsOf(caps, refPanelOp(caps, op));
  return ports.includes("text") || ports.some((port) => port !== "text" && kinds.includes(port));
}

/**
 * 生成方式在选项里要不要灰掉，灰掉时返回悬浮提示：
 * 没有参考素材（连线的图片 / 视频 / 音频，或手动添加的）时，要引用素材的方式没法用；
 * 有参考素材时，不引用素材的文生方式用不上。可选返回 undefined。
 */
export function opDisabledHint(
  caps: Capabilities | undefined,
  op: GenerationOp,
  hasRefs: boolean,
): string | undefined {
  if (!caps) return undefined;
  const kinds = REF_KINDS_OF_OP[op];
  if (kinds.length === 0) return hasRefs ? `已有参考素材，${OP_LABEL[op]}不可用` : undefined;
  if (hasRefs) return undefined;
  const labels = kinds
    .filter((kind) => caps.refs?.[kind]?.on)
    .map((kind) => REF_KEYS.find((ref) => ref.kind === kind)?.label.replace("参考", ""));
  return labels.length > 0 ? `需要连接${labels.join("/")}节点` : undefined;
}

/**
 * 当前生成方式收不下这种上游时，换成哪种方式就收得下：优先全能参考，
 * 其次任何收这种素材的方式。已经收得下、清单没到、哪种方式都收不下，都返回 undefined（不切）。
 * 「文生视频」节点拉出图片线、音频线时靠它自动变成全能参考。
 */
export function opToAcceptSource(
  caps: Capabilities | undefined,
  op: GenerationOp | undefined,
  sourceKind: NodeKind,
): GenerationOp | undefined {
  if (!caps || acceptsSourceKind(caps, op, sourceKind)) return undefined;
  const ops = caps.ops ?? [];
  const accepting = ops.filter((item) => acceptsSourceKind(caps, item, sourceKind));
  // 角色卡同时给图片和音频：优先选两样都收的方式，其次才是只收一样的
  const ports = PORTS_OF_KIND[sourceKind];
  const covering = accepting.filter((item) => {
    const kinds = refKindsOf(caps, refPanelOp(caps, item));
    return ports.every((port) => port !== "text" && kinds.includes(port));
  });
  const pick = (list: GenerationOp[]) => (list.includes("omni") ? "omni" : list[0]);
  return pick(covering) ?? pick(accepting);
}

/** 文件属于哪种参考素材：按 MIME 前缀判断，不是图片 / 视频 / 音频返回 null */
export function refKindOfFile(file: { type: string }): RefKind | null {
  return REF_KEYS.find((ref) => file.type.startsWith(`${ref.kind}/`))?.kind ?? null;
}

/**
 * 模型能接收哪些种类的参考素材：这种素材在 refs 里开着，并且至少有一种生成方式收它。
 * 当前生成方式暂时不收的（比如「文生视频」不收图片）也算——加进来时会像画布连线一样自动切到收它的方式。
 * 画布的素材口、首页输入卡片的上传入口共用它。
 */
export function acceptableRefKinds(caps: Capabilities | undefined): RefKind[] {
  if (!caps) return [];
  const ops = caps.ops ?? [];
  return REF_KEYS.map((ref) => ref.kind).filter(
    (kind) =>
      caps.refs?.[kind]?.on &&
      ops.some((op) => refKindsOf(caps, refPanelOp(caps, op)).includes(kind)),
  );
}

/** 某种参考素材最多几个；模型不收这种素材为 0 */
export const refMax = (caps: Capabilities | undefined, kind: RefKind): number =>
  caps?.refs?.[kind]?.on ? caps.refs[kind].max : 0;

/**
 * 上传一个参考素材前的校验：模型收不收这种素材、这种素材是不是已经满了、单个文件会不会太大。
 * 画布的参考素材卡和首页输入卡片共用，文案一致。
 * @param spec 这种素材在模型能力里的配置（caps.refs[kind]）；模型清单还没到为 undefined
 * @param kind 素材种类
 * @param file 要上传的文件（只看大小）
 * @param used 这种素材已经用了几个（连线的、手动添加的、已经放进输入卡片的都算）
 * @returns 不能上传的原因；可以上传为 null
 */
export function refUploadError(
  spec: RefSpec | undefined,
  kind: RefKind,
  file: { size: number },
  used: number,
): string | null {
  const label = REF_KEYS.find((ref) => ref.kind === kind)?.label ?? "参考素材";
  const max = spec?.on ? spec.max : 0;
  if (max <= 0) return `当前模型不支持${label}`;
  if (used >= max) return `${label}最多 ${max} 个`;
  const maxMb = spec?.max_mb ?? 0;
  if (maxMb > 0 && file.size > maxMb * 1024 * 1024) return `单个${label}不能超过 ${maxMb} MB`;
  return null;
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

/** 有没有图片引用：上游连着已经出图的图片节点，或手动添加过参考图；还没出图的上游不算 */
export function hasImageRefs(
  params: Record<string, unknown>,
  bindings: Pick<Bindings, "images">,
): boolean {
  return (
    bindings.images.some((link) => toAssetNumber(link.assetId) !== null) ||
    manualRefs(params, "images").length > 0
  );
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

/** 提交时素材引用写成「图片1」这种编号，序号按 input 里同类素材的顺序 */
const REF_WORD: Record<RefKind, string> = { image: "图片", video: "视频", audio: "音频" };

/**
 * 把提示词里 @ 进来的素材和节点预设换成模型看得懂的文字：文本上游换成它的正文，
 * 素材换成在 input 里的编号（连线已断、素材没提交的，退回成素材名）；
 * 角色卡换成角色名；预设换成预设提示词（设计稿 6.13），已下架的退回成名称。
 */
function expandRefs(prompt: string, links: IncomingLink[], input: Record<string, unknown>) {
  return expandPrompt(
    prompt,
    (id, label) => {
      const link = links.find((item) => item.sourceId === id);
      if (!link) return label;
      // 形象和声音已经由连线展开成参考图 / 参考音频带入 input，这里只把 @ 换成名字；
      // 节点已删或卡已失效时同样是名字，不报错
      if (link.sourceKind === "character") return link.character?.name || label;
      const ports = PORTS_OF_KIND[link.sourceKind];
      if (ports.length !== 1) return label;
      const port = ports[0];
      if (port === "text") return link.text?.trim() || label;
      const ref = REF_KEYS.find((item) => item.kind === port);
      const list = ref ? input[ref.key] : undefined;
      const asset = toAssetNumber(link.assetId);
      const index = Array.isArray(list) && asset !== null ? list.indexOf(asset) : -1;
      return index >= 0 ? `${REF_WORD[port]}${index + 1}` : label;
    },
    (kind, id) => findPreset(kind, id)?.prompt,
  );
}

/**
 * 按模型能力组装提交用的 input 并校验：
 * 提示词（手填优先，里面 @ 的素材展开成编号或正文；没手填才用上游文字）、生成方式、开放给用户的参数（没填取默认值），
 * 以及参考素材：当前方式允许的种类里，上游连线（还没出图的跳过）+ 手动添加的素材合并去重后按 refs 上限和方式要求检查。
 * 未开放的参数不放进 input，后端按默认值补齐。素材值是素材 assetId（数字），不是 URL。
 */
export function buildTaskInput(
  caps: Capabilities | undefined,
  params: Record<string, unknown>,
  bindings: Bindings = emptyBindings(),
  /** 调用方已经算好生成方式时直接传，缺省按引用情况推算 */
  opOverride?: GenerationOp,
  /** 接进来的全部上游，用来展开提示词里 @ 的素材 */
  links: IncomingLink[] = [],
): BuiltInput {
  const input: Record<string, unknown> = {};
  const errors: Record<string, string> = {};
  if (!caps) return { input, errors };
  const op = opOverride ?? currentOp(caps, params, hasImageRefs(params, bindings));
  const maxLength = caps.prompt?.max_length ?? 0;

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
  // 连着当前方式收的素材节点（哪怕还没出图）就算有引用，不再拦着
  let linked = 0;
  for (const ref of REF_KEYS) {
    if (!kinds.includes(ref.kind)) continue;
    linked += bindings[ref.key].length;
    // 还没出图的上游直接跳过：它在引用条里标着「还没生成」，不拦着整次提交
    const ids = bindings[ref.key]
      .map((item) => toAssetNumber(item.assetId))
      .filter((id): id is number => id !== null);
    const all = [...new Set([...ids, ...manualRefs(params, ref.key)])];
    const max = caps.refs[ref.kind].max;
    if (all.length > max) errors[ref.key] ??= `${ref.label}最多 ${max} 个，请移除多余的`;
    if (all.length > 0) input[ref.key] = all;
    total += all.length;
  }
  if ((op === "i2v" || op === "i2i") && !input.images && !errors.images && linked === 0)
    errors.images = "图生方式需要至少 1 张参考图片";
  if (
    op === "omni" &&
    total === 0 &&
    linked === 0 &&
    !Object.keys(errors).some((k) => k in REF_INDEX)
  )
    errors.images = "全能参考需要至少 1 个参考素材";

  // 提示词：手填的优先（@ 的素材展开），没手填才用上游文字
  const link = bindings.prompt;
  const typed = typeof params.prompt === "string" ? params.prompt : "";
  const prompt = expandRefs(typed, links, input).trim() || link?.text?.trim() || "";
  const promptError = !prompt
    ? link
      ? `上游「${link.sourceLabel}」还没有文字`
      : "请填写提示词"
    : maxLength > 0 && [...prompt].length > maxLength
      ? promptPresets(typed).length > 0
        ? `预设展开后提示词超过 ${maxLength} 个字，请去掉一个预设或缩短文字`
        : `提示词不能超过 ${maxLength} 个字`
      : null;
  // 素材编号要等参考素材收齐才能展开，所以提示词最后算；报错仍排在最前，节点上先提示它
  if (promptError) return { input, errors: { prompt: promptError, ...errors } };
  return { input: { prompt, ...input }, errors };
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
  const newOp = refPanelOp(newCaps, currentOp(newCaps, params));
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

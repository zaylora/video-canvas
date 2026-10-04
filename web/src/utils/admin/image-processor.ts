/**
 * 图片处理服务管理页的纯逻辑：存储与厂商的兼容判定、默认配置推导、表单校验、
 * 状态到按钮可用性的映射、发布前置条件。组件只负责渲染，规则都放这里便于单测。
 */
import type {
  ProcessorCheck,
  ProcessorConfig,
  ProcessorPreset,
  ProcessorVendor,
  ProcessorView,
} from "@/api/admin-image-processor/type";
import type { StorageView } from "@/api/admin-storage/type";

/** 绑定判定只需要存储的这几个字段 */
export type BindableStorage = Pick<
  StorageView,
  "id" | "name" | "provider" | "bucket" | "region" | "public_base_url"
>;

/** 判定占用关系只需要处理服务的这几个字段 */
type BindingProcessor = Pick<ProcessorView, "id" | "name" | "storage_id" | "status">;

/** 存储服务商的中文名；只用于写禁用原因，与存储配置页的预设名保持一致 */
const STORAGE_PROVIDER_LABEL: Record<string, string> = {
  local: "本地磁盘",
  aliyun_oss: "阿里云 OSS",
  tencent_cos: "腾讯云 COS",
  s3: "AWS S3",
  r2: "Cloudflare R2",
};

/**
 * 存储服务商的中文名；未知服务商回退为原 id
 * @param provider 存储服务商
 */
export const storageProviderLabel = (provider: string) =>
  STORAGE_PROVIDER_LABEL[provider] ?? provider;

/** 本地磁盘的地址厂商访问不到，所以不能接入任何处理服务 */
const LOCAL_BLOCK_REASON = "本地磁盘的地址厂商访问不到，无法接入处理服务";

/** 必须使用自家存储的厂商：处理能力是存储桶自带的，素材在别处时不生效 */
const OWN_STORAGE_VENDORS: ProcessorVendor[] = ["tencent_ci", "aliyun_oss_img"];

/**
 * 是否“必须使用自家存储”（腾讯云、阿里云）；第一步据此亮出标签，不等用户选到一半才发现不行
 * @param vendor 厂商
 */
export const isOwnStorageVendor = (vendor: ProcessorVendor) => OWN_STORAGE_VENDORS.includes(vendor);

/**
 * 取 URL 的 host（含端口）；空串或不是合法 URL 返回空串
 * @param url 形如 https://assets.example.com 的地址
 */
export function hostOf(url: string): string {
  try {
    return new URL(url).host;
  } catch {
    return "";
  }
}

/** 第二步里一个存储的可选性 */
export type StorageChoice = {
  /** 能否选择；false 时灰掉 */
  selectable: boolean;
  /** 不可选的原因；可选时为 null */
  reason: string | null;
  /** 仍可选但要提醒的事（已被另一个已发布的处理服务占用，发布会替换它）；没有为 null */
  note: string | null;
};

/**
 * 判定一个存储能否绑定到某厂商：存储类型必须与预设匹配；预设要求公开域名时存储必须设置；
 * 已被另一个已发布的处理服务占用只是提示，发布会把旧的自动停用。
 * @param preset 厂商预设
 * @param storage 候选存储
 * @param processors 全部处理服务（判定占用）
 * @param selfId 正在编辑的处理服务 ID，占用判定要排除它自己
 */
export function evaluateStorage(
  preset: ProcessorPreset,
  storage: BindableStorage,
  processors: BindingProcessor[],
  selfId?: number | null,
): StorageChoice {
  if (storage.provider !== preset.storage_provider) {
    return {
      selectable: false,
      reason: `这是「${storageProviderLabel(storage.provider)}」存储；${preset.name}只能绑定「${storageProviderLabel(preset.storage_provider)}」存储`,
      note: null,
    };
  }
  if (preset.requires_public_base && !storage.public_base_url) {
    return {
      selectable: false,
      reason: "这套存储还没设置公开域名：先到存储配置里填写，再回来绑定",
      note: null,
    };
  }
  const occupier = processors.find(
    (item) => item.storage_id === storage.id && item.status === "published" && item.id !== selfId,
  );
  return {
    selectable: true,
    reason: null,
    note: occupier
      ? `已被「${occupier.name}」绑定；发布后它会被自动停用，由当前处理服务接替`
      : null,
  };
}

/** 存储表“启用处理服务”按钮的目标 */
export type EnableTarget = {
  /** 可直接使用的厂商预设；不能启用时为 null */
  preset: ProcessorPreset | null;
  /** 不能启用的原因；可启用时为 null */
  reason: string | null;
};

/**
 * 存储表里“启用处理服务”：找出能直接用于这套存储的厂商；找不到时给出原因。
 * @param storage 存储
 * @param presets 厂商预设
 * @param processors 全部处理服务
 */
export function enableTarget(
  storage: BindableStorage,
  presets: ProcessorPreset[],
  processors: BindingProcessor[],
): EnableTarget {
  if (storage.provider === "local") return { preset: null, reason: LOCAL_BLOCK_REASON };
  const matched = presets.filter((item) => item.storage_provider === storage.provider);
  if (matched.length === 0) {
    return {
      preset: null,
      reason: `暂无支持「${storageProviderLabel(storage.provider)}」存储的处理服务厂商`,
    };
  }
  let firstReason: string | null = null;
  for (const preset of matched) {
    const choice = evaluateStorage(preset, storage, processors);
    if (choice.selectable) return { preset, reason: null };
    firstReason ??= choice.reason;
  }
  return { preset: null, reason: firstReason };
}

/** 一套存储上的处理服务归属 */
export type StorageBinding = {
  /** 线上生效的处理服务 */
  published: BindingProcessor | null;
  /** 草稿 */
  draft: BindingProcessor | null;
  /** 已停用的 */
  disabled: BindingProcessor | null;
};

/**
 * 按存储归拢处理服务，存储表据此显示“已启用 / 草稿 / 已停用 / 未启用”
 * @param storageId 存储 ID
 * @param processors 全部处理服务
 */
export function bindingOfStorage<T extends BindingProcessor>(
  storageId: number,
  processors: T[],
): { published: T | null; draft: T | null; disabled: T | null } {
  const own = processors.filter((item) => item.storage_id === storageId);
  const pick = (status: T["status"]) => own.find((item) => item.status === status) ?? null;
  return { published: pick("published"), draft: pick("draft"), disabled: pick("disabled") };
}

/**
 * 新建时的默认参数：以预设为底；cloudflare 的 domain 取存储公开域名的 host，
 * 腾讯云用存储的 bucket 与 region 拼 COS 默认域名。
 * @param preset 厂商预设
 * @param storage 绑定的存储；还没选时为空
 * @returns 新对象，改它不会影响预设
 */
export function defaultConfig(
  preset: ProcessorPreset,
  storage?: Pick<BindableStorage, "bucket" | "region" | "public_base_url"> | null,
): ProcessorConfig {
  const config: ProcessorConfig = { ...preset.default_config };
  if (!storage) return config;
  if (preset.vendor === "cloudflare" && storage.public_base_url) {
    config.domain = hostOf(storage.public_base_url) || config.domain;
  }
  if (preset.vendor === "tencent_ci" && storage.bucket && storage.region) {
    config.domain = `${storage.bucket}.cos.${storage.region}.myqcloud.com`;
  }
  return config;
}

/** 第三步按厂商显示哪些字段 */
export type ConfigFields = {
  /** 视频封面取帧时间：只有预设支持封面才显示 */
  time: boolean;
  /** 输出质量：仅 cloudflare */
  quality: boolean;
  /** 处理失败回退原图：仅 cloudflare */
  onErrorRedirect: boolean;
  /** 已开通媒体处理开关：仅腾讯云，且厂商支持封面 */
  mediaEnabled: boolean;
  /** 访问域名一栏的标题 */
  domainLabel: string;
  /** 访问域名一栏的说明 */
  domainHint: string;
};

/**
 * 按厂商与预设决定参数表单显示的字段
 * @param preset 厂商预设
 */
export function configFields(preset: ProcessorPreset): ConfigFields {
  const domainText: Record<ProcessorVendor, [string, string]> = {
    tencent_ci: ["访问域名", "默认是 COS 域名，也可填绑定的 CDN 域名"],
    aliyun_oss_img: ["自定义域名", "建议绑定自定义域名，否则图片预览会变成下载"],
    cloudflare: ["公开域名（zone）", "必须与存储配置里的公开域名一致"],
  };
  const [domainLabel, domainHint] = domainText[preset.vendor];
  return {
    time: preset.supports_poster,
    quality: preset.vendor === "cloudflare",
    onErrorRedirect: preset.vendor === "cloudflare",
    mediaEnabled: preset.vendor === "tencent_ci" && preset.supports_poster,
    domainLabel,
    domainHint,
  };
}

/**
 * 提交前整理参数：域名去空白，裁掉当前厂商用不到的字段（后端整份校验，多余字段没意义）
 * @param preset 厂商预设
 * @param config 表单里的参数
 */
export function buildProcessorConfig(
  preset: ProcessorPreset,
  config: ProcessorConfig,
): ProcessorConfig {
  const fields = configFields(preset);
  const body: ProcessorConfig = {
    domain: config.domain.trim(),
    width: config.width,
    format: config.format,
    time_sec: fields.time ? config.time_sec : 0,
  };
  if (fields.quality) body.quality = config.quality;
  if (fields.onErrorRedirect) body.on_error_redirect = config.on_error_redirect ?? true;
  if (fields.mediaEnabled) body.media_enabled = config.media_enabled ?? false;
  return body;
}

/** 参数表单的错误，key 是字段名 */
export type ProcessorFormErrors = Partial<
  Record<"name" | "domain" | "width" | "time_sec" | "quality", string>
>;

/** 宽度范围（px），与后端校验一致 */
export const WIDTH_MIN = 16;
export const WIDTH_MAX = 2000;

/**
 * 校验参数表单：名称、域名必填，域名只写 host；width 16–2000 的整数；
 * 取帧时间不小于 0（不支持封面时忽略）；cloudflare 的 quality 1–100。
 * @param preset 厂商预设
 * @param form 名称与参数
 * @returns 错误表；没有错误为空对象
 */
export function validateProcessorForm(
  preset: ProcessorPreset,
  form: { name: string; config: ProcessorConfig },
): ProcessorFormErrors {
  const errors: ProcessorFormErrors = {};
  const { config } = form;
  const fields = configFields(preset);
  const domain = config.domain.trim();
  if (!form.name.trim()) errors.name = "请填写名称";
  if (!domain) errors.domain = "请填写访问域名";
  else if (/[/\s]/.test(domain)) errors.domain = "只写域名，不要带 https:// 或路径";
  if (!Number.isInteger(config.width) || config.width < WIDTH_MIN || config.width > WIDTH_MAX) {
    errors.width = `长边需是 ${WIDTH_MIN}–${WIDTH_MAX} 之间的整数`;
  }
  if (fields.time && !(config.time_sec >= 0)) errors.time_sec = "取帧时间不能小于 0";
  if (fields.quality) {
    const quality = config.quality ?? Number.NaN;
    if (!Number.isInteger(quality) || quality < 1 || quality > 100) {
      errors.quality = "质量需是 1–100 之间的整数";
    }
  }
  return errors;
}

/** 列表一行上各操作是否可用 */
export type ProcessorActions = {
  /** 编辑（已发布的编辑产生草稿） */
  edit: boolean;
  /** 校验 / 发布 */
  verify: boolean;
  /** 回滚：已发布且有上一个版本 */
  rollback: boolean;
  /** 停用：已发布 */
  disable: boolean;
  /** 删除：仅草稿 / 已停用 */
  remove: boolean;
};

/**
 * 状态到按钮可用性：写操作只对 super_admin，其余按处理服务状态判断（后端同样会校验）。
 * @param processor 处理服务
 * @param canWrite 是否有写权限
 */
export function processorActions(
  processor: Pick<ProcessorView, "status" | "has_draft" | "previous_version">,
  canWrite: boolean,
): ProcessorActions {
  if (!canWrite)
    return { edit: false, verify: false, rollback: false, disable: false, remove: false };
  const published = processor.status === "published";
  return {
    edit: true,
    verify: !published || processor.has_draft,
    rollback: published && processor.previous_version !== null,
    disable: published,
    remove: !published,
  };
}

/**
 * 最近一次校验是否已经过期：没校验过，或保存草稿后 version 超过了校验针对的 version
 * @param processor 处理服务
 */
export const checkIsStale = (processor: Pick<ProcessorView, "version" | "check">) =>
  !processor.check || processor.check.version !== processor.version;

/**
 * 发布前置条件：有可发布的内容、校验未过期且没有 fail（warn 不挡）。
 * @param processor 处理服务
 * @returns 不能发布的原因；可以发布为 null
 */
export function publishBlockReason(
  processor: Pick<ProcessorView, "status" | "has_draft" | "version" | "check">,
): string | null {
  if (processor.status === "published" && !processor.has_draft)
    return "线上已是最新配置，没有需要发布的草稿";
  if (!processor.check) return "还没有校验与试跑，请先运行校验";
  if (processor.check.version !== processor.version) return "配置已修改，需要重新校验后才能发布";
  if (!processor.check.ok) return "校验未通过，不能发布";
  return null;
}

/**
 * 体积换算成人看的单位：B / KB / MB，10 以下保留一位小数
 * @param bytes 字节数
 */
export function formatBytes(bytes: number): string {
  const trim = (value: number) =>
    value >= 10 ? String(Math.round(value)) : String(Number(value.toFixed(1)));
  if (bytes < 1024) return `${Math.round(bytes)} B`;
  if (bytes < 1024 * 1024) return `${trim(bytes / 1024)} KB`;
  return `${trim(bytes / (1024 * 1024))} MB`;
}

/**
 * 试跑结果的一句话摘要；没有对应素材（试跑项为 null）时对应项为 null
 * @param check 最近一次校验；没有为 null
 */
export function trialSummary(check: ProcessorCheck | null): {
  image: string | null;
  video: string | null;
} {
  const image = check?.trial.image;
  const video = check?.trial.video;
  return {
    image: image
      ? `${formatBytes(image.source_bytes)} → ${formatBytes(image.bytes)} · ${image.ms} ms`
      : null,
    video: video ? `封面 ${formatBytes(video.bytes)} · ${video.ms} ms` : null,
  };
}

/** 试跑结果的一行展示：文案加语气（成功 / 提示 / 失败 / 没有结果） */
export type TrialLine = { text: string; tone: "ok" | "warn" | "fail" | "none" };

/**
 * 某一类试跑的展示行。成功给体积与耗时；没有结果时不能一律说“没有素材”——
 * 失败、没有素材、不支持这个变体都会让结果为空，所以沿用后端校验项（trial_image / trial_video）给出的状态与说明。
 * @param check 最近一次校验；没有为 null
 * @param kind 图片缩略图或视频封面
 */
export function trialLine(check: ProcessorCheck | null, kind: "image" | "video"): TrialLine {
  const text = trialSummary(check)[kind];
  if (text) return { text, tone: "ok" };
  const item = check?.checks.find((c) => c.key === `trial_${kind}`);
  if (!item) return { text: "未试跑", tone: "none" };
  return { text: item.message, tone: item.status === "ok" ? "none" : item.status };
}

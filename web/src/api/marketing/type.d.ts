/** 投放平台 */
export type MarketingPlatform =
  | "douyin"
  | "xhs"
  | "tmall"
  | "tiktok"
  | "amazon"
  | "reels"
  | "generic";

/** 创意角度；跟拍脚本的角度固定为 follow */
export type MarketingAngle = "pain" | "feature" | "lifestyle" | "compare" | "unbox" | "mood";

/** 旁白语言 */
export type MarketingLanguage = "zh" | "en";

/** 写脚本的四个环节 */
export type MarketingStep = "facts" | "reverse" | "follow" | "create";

/** 轮次状态 */
export type RoundStatus = "scripting" | "ready" | "failed" | "canceled";

/** 环节运行状态；事实卡沿用项目已有的为 reused */
export type StepRunStatus =
  | "queued"
  | "running"
  | "succeeded"
  | "failed"
  | "skipped"
  | "canceled"
  | "reused";

/** 脚本状态：rewriting 是重写跟拍版中 */
export type ScriptStatus = "draft" | "rewriting" | "rendering" | "done" | "failed";

/** 脚本种类：跟拍版或创意改编版 */
export type ScriptKind = "follow" | "create";

/** 参考片镜头里商品的出现方式 */
export type ProductRole = "none" | "dynamic" | "hero_real" | "package_text";

/** 旁白问题类型：禁用说法、背书无依据、第三方品牌 */
export type FlagType = "forbidden" | "claim" | "brand";

/** 方法来源：内置或导入的技能 */
export type MethodSource = "builtin" | "skill";

/** 后端 GET /marketing/options：一个投放平台预设 */
export interface BackendPlatformOption {
  key: MarketingPlatform;
  name: string;
  aspect_ratio: string;
  language: MarketingLanguage;
  style: string;
}

/** 后端 GET /marketing/options：一个创意角度 */
export interface BackendAngleOption {
  key: MarketingAngle;
  name: string;
  desc: string;
}

/** 后端 GET /marketing/options 的响应 */
export interface BackendMarketingOptions {
  platforms: BackendPlatformOption[] | null;
  angles: BackendAngleOption[] | null;
  /** 参考视频时长上限（秒），模型能力里没有这个维度，由后端给出 */
  ref_video_max_seconds?: number | null;
}

/** 投放平台预设 */
export interface PlatformOption {
  /** 平台 key */
  key: MarketingPlatform;
  /** 展示名 */
  name: string;
  /** 默认画幅，如 9:16 */
  aspectRatio: string;
  /** 默认旁白语言 */
  language: MarketingLanguage;
  /** 写法要点 */
  style: string;
}

/** 创意角度预设 */
export interface AngleOption {
  /** 角度 key */
  key: MarketingAngle;
  /** 展示名 */
  name: string;
  /** 一句话说明 */
  desc: string;
}

/** 平台与角度预设 */
export interface MarketingOptions {
  /** 投放平台 */
  platforms: PlatformOption[];
  /** 内置创意角度 */
  angles: AngleOption[];
  /** 参考视频时长上限（秒），没有为 null */
  refVideoMaxSeconds: number | null;
}

/** 后端轮次设置（请求和视图同形） */
export interface BackendRoundSettings {
  platform: MarketingPlatform;
  language: MarketingLanguage;
  angles: MarketingAngle[] | null;
  auto_count: number;
  video_model: string;
  text_model: string;
  params: Record<string, unknown> | null;
  description: string;
}

/** 轮次设置 */
export interface RoundSettings {
  /** 投放平台 */
  platform: MarketingPlatform;
  /** 旁白语言 */
  language: MarketingLanguage;
  /** 创意角度；空数组 = 自动，由模型挑 autoCount 个 */
  angles: MarketingAngle[];
  /** angles 为空时生效：无参考 1–4，有参考 0–3 */
  autoCount: number;
  /** 视频模型 key */
  videoModel: string;
  /** 写脚本的文本模型 key；空串 = 后端按能力自动选 */
  textModel: string;
  /** 视频参数，键名与生成任务 input 一致（画幅、分辨率、时长） */
  params: Record<string, unknown>;
  /** 用户描述 */
  description: string;
}

/** 后端商品卖点 */
export interface BackendSellingPoint {
  text: string;
  proof: string;
  source: "description" | "image";
}

/** 后端商品事实卡 */
export interface BackendProductFacts {
  name: string;
  category: string;
  anchors: string[] | null;
  selling_points: BackendSellingPoint[] | null;
  prohibited: string[] | null;
  audience: string;
  cta: string;
}

/** 商品卖点 */
export interface SellingPoint {
  /** 卖点原文 */
  text: string;
  /** 画面里怎么证明 */
  proof: string;
  /** 来自描述还是图片 */
  source: "description" | "image";
}

/** 商品事实卡 */
export interface ProductFacts {
  /** 商品名 */
  name: string;
  /** 品类 */
  category: string;
  /** 外观锚点 */
  anchors: string[];
  /** 卖点 */
  sellingPoints: SellingPoint[];
  /** 禁用说法 */
  prohibited: string[];
  /** 目标人群 */
  audience: string;
  /** 引导下单 */
  cta: string;
}

/** 后端素材引用 */
export interface BackendAssetRef {
  id: number;
  url: string;
}

/** 素材引用 */
export interface AssetRef {
  /** 素材 ID（字符串） */
  id: string;
  /** 素材地址 */
  url: string;
}

/** 后端方法说明 */
export interface BackendMethod {
  source: MethodSource;
  skill_name?: string;
  skill_title?: string;
  version?: number;
  fallback?: boolean;
}

/** 方法说明：哪个方法写出的内容 */
export interface MethodInfo {
  /** 内置或技能 */
  source: MethodSource;
  /** 技能名；内置为空串 */
  skillName: string;
  /** 技能显示名 */
  skillTitle: string;
  /** 技能版本；内置为 0 */
  version: number;
  /** 技能被停用，已自动退回内置 */
  fallback: boolean;
}

/** 后端环节运行 */
export interface BackendStepRun {
  id: string;
  step: MarketingStep;
  status: StepRunStatus;
  method?: BackendMethod | null;
  text_model?: string;
  error?: string;
  attempts?: number;
  charged?: number;
}

/** 环节运行（每个环节最新一条） */
export interface StepRun {
  /** 运行记录 ID */
  id: string;
  /** 环节 */
  step: MarketingStep;
  /** 状态 */
  status: StepRunStatus;
  /** 使用的方法；没有为 null */
  method: MethodInfo | null;
  /** 使用的文本模型 */
  textModel: string;
  /** 失败原因 */
  error: string;
  /** 尝试次数 */
  attempts: number;
  /** 实扣积分 */
  charged: number;
}

/** 后端拆解镜头 */
export interface BackendReverseShot {
  id: number;
  start: number;
  end: number;
  is_opening_3s: boolean;
  shot_size: string;
  camera: string;
  subject: string;
  action: string;
  scene: string;
  lighting: string;
  person: string;
  product_in_frame: string;
  product_role: ProductRole;
  dialogue: string;
  onscreen_text: string;
  key_colors: string[] | null;
}

/** 后端参考片拆解 */
export interface BackendReverse {
  overall: {
    style: string;
    narrative_arc: string;
    why_viral: string;
    transcript: string;
    category: string;
  };
  shots: BackendReverseShot[] | null;
  brands: string[] | null;
  claims: string[] | null;
}

/** 拆解镜头 */
export interface ReverseShot {
  /** 镜头序号，从 1 起 */
  id: number;
  /** 起点（秒） */
  start: number;
  /** 终点（秒） */
  end: number;
  /** 属于前 3 秒 */
  isOpening3s: boolean;
  /** 景别 */
  shotSize: string;
  /** 运镜 */
  camera: string;
  /** 主体 */
  subject: string;
  /** 主体 + 动作，用户可改 */
  action: string;
  /** 场景 */
  scene: string;
  /** 光线 */
  lighting: string;
  /** 人物 */
  person: string;
  /** 商品如何出现 + 占画面比例 */
  productInFrame: string;
  /** 商品出现方式，用户可改 */
  productRole: ProductRole;
  /** 台词，用户可改 */
  dialogue: string;
  /** 画面文字 */
  onscreenText: string;
  /** 关键颜色，用户可改 */
  keyColors: string[];
}

/** 参考片拆解 */
export interface Reverse {
  /** 整体 */
  overall: {
    /** 风格 */
    style: string;
    /** 叙事弧线 */
    narrativeArc: string;
    /** 为什么火 */
    whyViral: string;
    /** 台词全文 */
    transcript: string;
    /** 品类 */
    category: string;
  };
  /** 镜头 */
  shots: ReverseShot[];
  /** 出现的第三方品牌 / IP */
  brands: string[];
  /** 促销、销量、权威背书话术原文 */
  claims: string[];
}

/** 后端脚本镜头 */
export interface BackendScriptShot {
  start: number;
  end: number;
  purpose?: string;
  shot: string;
  camera: string;
  scene: string;
  narration: string;
  ref_shot?: number | null;
}

/** 脚本镜头 */
export interface ScriptShot {
  /** 起点（秒） */
  start: number;
  /** 终点（秒） */
  end: number;
  /** 镜头作用 */
  purpose: string;
  /** 景别 */
  shot: string;
  /** 运镜 */
  camera: string;
  /** 画面 */
  scene: string;
  /** 旁白，用户可改 */
  narration: string;
  /** 跟拍对应的参考镜头 id；改编版为 null */
  refShot: number | null;
}

/** 后端旁白问题 */
export interface BackendFlag {
  shot: number;
  type: FlagType;
  text: string;
}

/** 旁白问题：shot 为镜头下标（从 0 起） */
export interface ScriptFlag {
  /** 镜头下标，从 0 起 */
  shot: number;
  /** 问题类型 */
  type: FlagType;
  /** 问题文字 */
  text: string;
}

/** 后端出片任务摘要 */
export interface BackendTaskLite {
  id: string | number;
  task_ref?: string;
  status: string;
  progress?: number | null;
  outputs?: Array<{ url?: string; poster_url?: string }> | null;
  error_message?: string | null;
}

/** 出片任务摘要 */
export interface TaskLite {
  /** 任务 ID */
  id: string;
  /** 任务编号，失败时给用户报错用 */
  taskRef: string;
  /** 任务状态 */
  status: string;
  /** 进度；未知为 null */
  progress: number | null;
  /** 产出，带地址的才有意义 */
  outputs: Array<{ url: string; posterUrl: string }>;
  /** 失败原因 */
  errorMessage: string;
}

/** 后端脚本 */
export interface BackendScript {
  id: string;
  round_id: string;
  kind: ScriptKind;
  angle: MarketingAngle | "follow";
  title: string;
  hook?: string;
  shots: BackendScriptShot[] | null;
  music?: string;
  voice?: string;
  flags: BackendFlag[] | null;
  status: ScriptStatus;
  edited: boolean;
  method?: BackendMethod | null;
  takes: BackendTaskLite[] | null;
  render_credits: number;
  error?: string;
  revision: number;
}

/** 脚本 */
export interface Script {
  /** 脚本 ID */
  id: string;
  /** 所属轮次 */
  roundId: string;
  /** 跟拍版或改编版 */
  kind: ScriptKind;
  /** 角度；跟拍为 follow */
  angle: MarketingAngle | "follow";
  /** 标题 */
  title: string;
  /** 钩子 */
  hook: string;
  /** 镜头 */
  shots: ScriptShot[];
  /** 配乐 */
  music: string;
  /** 旁白音色 */
  voice: string;
  /** 旁白问题 */
  flags: ScriptFlag[];
  /** 状态 */
  status: ScriptStatus;
  /** 和模型原稿不同 */
  edited: boolean;
  /** 写出这份脚本的方法 */
  method: MethodInfo | null;
  /** 出片任务历史，按时间正序 */
  takes: TaskLite[];
  /** 一次出片预估积分 */
  renderCredits: number;
  /** 最近一次出片失败原因 */
  error: string;
  /** 乐观锁版本 */
  revision: number;
}

/** 后端轮次 */
export interface BackendRound {
  id: string;
  no: number;
  status: RoundStatus;
  stage?: string;
  error?: string;
  warnings: string[] | null;
  settings: BackendRoundSettings;
  ref_video?: { id: number; url: string; duration: number } | null;
  model_asset?: BackendAssetRef | null;
  steps: BackendStepRun[] | null;
  plan_count: number;
  reverse?: BackendReverse | null;
  reverse_revision?: number;
  reverse_edited?: boolean;
  reverse_dirty?: boolean;
  scripts: BackendScript[] | null;
  frozen?: number;
  charged?: number;
  created_at: string;
}

/** 参考视频 */
export interface RefVideo {
  /** 素材 ID */
  id: string;
  /** 素材地址 */
  url: string;
  /** 时长（秒） */
  duration: number;
}

/** 轮次 */
export interface Round {
  /** 轮次 ID */
  id: string;
  /** 第几轮 */
  no: number;
  /** 状态 */
  status: RoundStatus;
  /** 写脚本阶段说明，只在 scripting 时有意义 */
  stage: string;
  /** failed 时给用户看的原因 */
  error: string;
  /** 警告，如拆解未包含声音 */
  warnings: string[];
  /** 设置快照 */
  settings: RoundSettings;
  /** 参考视频；没有为 null */
  refVideo: RefVideo | null;
  /** 本轮出片用的模特图；没有为 null */
  modelAsset: AssetRef | null;
  /** 每个环节最新一条运行记录 */
  steps: StepRun[];
  /** 计划出几份脚本，写脚本中前端画骨架卡 */
  planCount: number;
  /** 当前拆解；没有为 null */
  reverse: Reverse | null;
  /** 拆解乐观锁版本 */
  reverseRevision: number;
  /** 和模型原稿不同 */
  reverseEdited: boolean;
  /** 和当前跟拍版据以写成的拆解不同 */
  reverseDirty: boolean;
  /** 脚本，跟拍在前 */
  scripts: Script[];
  /** 冻结积分 */
  frozen: number;
  /** 实扣积分 */
  charged: number;
  /** 创建时间 */
  createdAt: string;
}

/** 后端项目列表项 */
export interface BackendProjectSummary {
  id: string;
  title: string;
  cover_url?: string;
  scripting: boolean;
  rendering: number;
  last_round_failed: boolean;
  script_counts?: { draft?: number; rendering?: number; done?: number; failed?: number };
  bars: ScriptBar[] | null;
  updated_at: string;
}

/** 列表卡底部状态条的一格 */
export type ScriptBar = "draft" | "rendering" | "done" | "failed";

/** 项目列表项 */
export interface ProjectSummary {
  /** 项目 ID */
  id: string;
  /** 标题 */
  title: string;
  /** 封面地址；没有为空串 */
  coverUrl: string;
  /** 有轮次在写脚本 */
  scripting: boolean;
  /** 出片中的脚本数 */
  rendering: number;
  /** 最近一轮失败 */
  lastRoundFailed: boolean;
  /** 各状态脚本数 */
  scriptCounts: Record<ScriptBar, number>;
  /** 最近最多 8 份脚本的状态 */
  bars: ScriptBar[];
  /** 更新时间 */
  updatedAt: string;
}

/** 后端项目详情 */
export interface BackendProject {
  id: string;
  title: string;
  product?: BackendProductFacts | null;
  product_assets: BackendAssetRef[] | null;
  model_asset?: BackendAssetRef | null;
  revision: number;
  rounds: BackendRound[] | null;
  created_at: string;
  updated_at: string;
}

/** 项目详情 */
export interface Project {
  /** 项目 ID */
  id: string;
  /** 标题 */
  title: string;
  /** 事实卡；第一轮事实卡环节成功后才有 */
  product: ProductFacts | null;
  /** 商品图 */
  productAssets: AssetRef[];
  /** 模特图；没有为 null */
  modelAsset: AssetRef | null;
  /** 乐观锁版本 */
  revision: number;
  /** 轮次，最新在前 */
  rounds: Round[];
  /** 创建时间 */
  createdAt: string;
  /** 更新时间 */
  updatedAt: string;
}

/** 项目列表分页 */
export interface ProjectPage {
  /** 当前页 */
  items: ProjectSummary[];
  /** 总数 */
  total: number;
}

/** 创建项目的输入 */
export interface CreateProjectInput {
  /** 商品图素材 ID（1–4 张） */
  productAssetIds: number[];
  /** 模特图素材 ID */
  modelAssetId?: number;
  /** 参考视频素材 ID */
  refVideoAssetId?: number;
  /** 第一轮设置 */
  settings: RoundSettings;
}

/** 改一轮出片设置的输入：画幅、时长须与原来一致，模特图只能换不能加减 */
export interface UpdateRoundSettingsInput {
  /** 视频模型 key */
  videoModel: string;
  /** 视频参数，键名按所选模型 */
  params: Record<string, unknown>;
  /** 换用的模特图素材 ID；不换不传 */
  modelAssetId?: number;
}

/** 新开一轮的输入 */
export interface CreateRoundInput {
  /** 换用的模特图素材 ID */
  modelAssetId?: number;
  /** 参考视频素材 ID */
  refVideoAssetId?: number;
  /** 本轮设置 */
  settings: RoundSettings;
}

/** 写脚本预估积分的请求 */
export interface QuoteInput {
  /** 已有项目 ID；项目已有事实卡时事实卡环节为 0 */
  projectId?: string;
  /** 是否带参考视频 */
  hasRef: boolean;
  /** 设置 */
  settings: RoundSettings;
}

/** 写脚本预估积分 */
export interface Quote {
  /** 各环节之和 */
  credits: number;
  /** 分环节明细 */
  steps: Array<{ step: MarketingStep; credits: number }>;
}

/** 视频提示词 */
/** 后端提示词预览 */
export interface BackendScriptPrompt {
  format: ScriptPrompt["format"];
  prompt: string;
  adaptation?: { prompt: string; stale: boolean; edited: boolean; updated_at: string } | null;
  adapt_credits?: number;
}

/** 转写稿 */
export interface PromptAdaptation {
  /** 转写稿正文 */
  prompt: string;
  /** 原稿已变，出片时会重新转写 */
  stale: boolean;
  /** 手改过 */
  edited: boolean;
  /** 更新时间 */
  updatedAt: string;
}

export interface ScriptPrompt {
  /** 格式：seedance 分区块、通用段落；h3 要先转写（prompt 是原稿，出片用转写稿） */
  format: "seedance" | "generic" | "h3";
  /** 原稿：seedance、generic 就是出片用的提示词；h3 是转写依据 */
  prompt: string;
  /** h3 的转写稿；还没转写为 null */
  adaptation: PromptAdaptation | null;
  /** 转写一次的预估积分（h3 才有意义） */
  adaptCredits: number;
}

/** 一键出片的结果 */
export interface RenderAllResult {
  /** 更新后的轮次 */
  round: Round;
  /** 被跳过的脚本与原因 */
  skipped: Array<{ scriptId: string; reason: string }>;
}

/** WebSocket marketing.round 的原始数据 */
export interface MarketingRoundEvent {
  project_id: string;
  round: BackendRound;
}

/** WebSocket marketing.script 的原始数据 */
export interface MarketingScriptEvent {
  project_id: string;
  round_id: string;
  script: BackendScript;
}

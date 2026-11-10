import type { AssetDto, BackendAssetDto } from "@/api/asset/type";
import type { TaskStatus, TaskView } from "@/api/generation-task/type";

/**
 * 短剧工作室接口类型（契约 docs/design/短剧工作室/接口契约.md）。
 * Backend* 是后端原样的 snake_case 响应，其余是前端 camelCase 结构。
 */

/** 短剧整体状态 */
export type DramaStatus = "analyzing" | "outlined" | "scripting" | "ready" | "failed";

/** 流水线步骤 */
export type DramaStep =
  | "analyze"
  /** 分集规划（pipeline 自动发起，一批单元出一批分集目录） */
  | "plan"
  | "outline"
  | "scripts"
  | "breakdown"
  | "asset_prompts"
  | "storyboard"
  /** 合成整集（契约 §14）：服务端 ffmpeg，不扣积分 */
  | "compose";

/** 任务所在的线：pipeline 是系统自动研读 / 规划，user 是用户发起的全部任务 */
export type DramaLane = "pipeline" | "user";

/** 自动规划阶段：running 研读 / 规划中，paused 已暂停或失败，done 规划到范围终点（老项目也是 done） */
export type DramaPhase = "running" | "paused" | "done";

/** 规划暂停的原因：用户暂停 / 积分不足 / 规划失败 / 模型上下文不足 / 自定义提示词导致输出不合格 */
export type PauseCode = "user" | "credits" | "failed" | "context" | "custom_prompt";

/** 原文切出来的单元种类：集 / 场次 / 章 / 段 */
export type UnitKind = "episode" | "scene" | "chapter" | "chunk";

/** 提示词的四个环节：剧本改编 / 资产拆解 / 资产图提示词 / 视频提示词 */
export type PromptStep = "adapt" | "assets" | "asset_prompts" | "video";

/** 任务状态 */
export type DramaJobStatus = "queued" | "running" | "succeeded" | "failed" | "canceled";

/** 一集剧本的状态 */
export type ScriptStatus = "empty" | "generating" | "ready" | "failed";

/** 一集分镜脚本的状态（契约 §9.1） */
export type StoryboardStatus = "empty" | "generating" | "ready" | "failed";

/** 成片比例，空串表示未定 */
export type DramaRatio = "9:16" | "16:9" | "21:9" | "3:4" | "4:3" | "1:1" | "";

/** 原文类型，研读前为空串 */
export type DramaSourceKind = "script" | "novel" | "";

/** 开拍设置可选的画幅：只有三档 */
export type StartRatio = "9:16" | "16:9" | "4:3";

/** 提示词来源：内置 / 我的提示词 / 后台技能 */
export type PromptSource = "builtin" | "mine" | "skill";

/** 一个环节的提示词选择（提交给后端的结构）：mine 带 id，skill 带技能 name */
export interface PromptChoice {
  /** 来源 */
  source: PromptSource;
  /** source = mine 时的「我的提示词」id */
  id?: string;
  /** source = skill 时的技能 name */
  skill?: string;
}

/** 四个环节的提示词选择；省略的环节等于内置 */
export type PromptChoices = Partial<Record<PromptStep, PromptChoice>>;

/** 已建剧的一个环节提示词设置（快照来源与展示名） */
export interface PromptSetting extends PromptChoice {
  /** 展示名，如「内置」「我的 · 古风资产」「技能 · 跨集连续性」 */
  label: string;
  /** true 表示快照来源（我的提示词 / 技能）已被删或停用：快照照常可用，界面标红提示 */
  missing?: boolean;
}

/** 已建剧的四个环节提示词设置 */
export type PromptSettings = Record<PromptStep, PromptSetting>;

export interface BackendDramaSummary {
  id: string;
  title: string;
  status: DramaStatus;
  /** 自动规划阶段；老后端没有，按 done 处理 */
  phase?: DramaPhase;
  episode_count: number;
  locked_count: number;
  cover_url: string;
  updated_at: string;
}

export interface BackendEpisodeSummary {
  no: number;
  title: string;
  synopsis: string;
  script_status: ScriptStatus;
  locked: boolean;
  revision: number;
  updated_at: string;
  /** 二期字段（契约 §9.1）；后端还没带时按空处理 */
  storyboard_status?: StoryboardStatus;
  segment_count?: number;
  segment_seconds?: number;
  done_segment_count?: number;
  /** 三期字段（契约 §14）：成片与是否过期；后端还没带时按「没有成片」处理 */
  final?: BackendAssetDto | null;
  final_stale?: boolean;
  /** 开场钩子；规划前建的老集为空 */
  hook?: string;
  /** 结尾悬念；规划前建的老集为空 */
  cliffhanger?: string;
  /** 对应原文起始单元号，0 = 未关联单元 */
  unit_from?: number;
  /** 对应原文结束单元号，0 = 未关联单元 */
  unit_to?: number;
}

export interface BackendEpisodeView extends BackendEpisodeSummary {
  script_md: string;
}

export interface BackendJobView {
  id: string;
  step: DramaStep;
  /** analyze / outline 为空数组 */
  episodes: number[] | null;
  status: DramaJobStatus;
  progress: number;
  credits: number;
  error_code: number;
  error_message: string;
  created_at: string;
  finished_at: string | null;
  /** compose 成功时 `{asset, skipped_seqs}`；其他步骤为 null（契约 §14） */
  result?: { asset: BackendAssetDto; skipped_seqs: number[] | null } | null;
  /** 所在的线；老后端没有这个字段 */
  lane?: DramaLane;
  /** plan 任务对应的起始单元号 */
  batch_from?: number;
  /** plan 任务对应的结束单元号 */
  batch_to?: number;
}

/** 三类资产各自的图片模型 key，空串 = 不指定 */
export interface DramaImageModels {
  character: string;
  scene: string;
  prop: string;
}

export interface BackendDramaView {
  id: string;
  title: string;
  canvas_id: string;
  status: DramaStatus;
  source_kind: DramaSourceKind;
  genre: string;
  style_key: string;
  style_custom: string;
  ratio: DramaRatio;
  target_episodes: number;
  episode_seconds: number;
  text_model: string;
  video_model?: string;
  image_models?: Partial<DramaImageModels> | null;
  outline_md: string;
  characters_md: string;
  revision: number;
  episodes: BackendEpisodeSummary[] | null;
  active_jobs: BackendJobView[] | null;
  created_at: string;
  updated_at: string;
  /** 原文字数 */
  source_chars?: number;
  /** 原文单元总数 */
  units_total?: number;
  /** 单元种类 */
  unit_kind?: UnitKind;
  /** 自动规划阶段；老后端没有，按 done 处理 */
  phase?: DramaPhase;
  /** 已规划到第几个单元 */
  plan_cursor?: number;
  /** 规划范围终点（已归一为 units_total） */
  range_end?: number;
  /** 暂停原因文案 */
  pause_reason?: string;
  /** 暂停原因分类 */
  pause_code?: PauseCode | "";
  /** 四个环节当前的提示词设置 */
  prompt_settings?: PromptSettings;
}

export interface BackendDramaStyles {
  genres: { key: string; name: string }[] | null;
  styles: { key: string; name: string; prompt: string }[] | null;
}

/** 列表项 */
export interface DramaSummary {
  id: string;
  title: string;
  status: DramaStatus;
  /** 自动规划阶段：running / paused 时首页显示「规划中」「规划已暂停」 */
  phase: DramaPhase;
  episodeCount: number;
  /** 已拆解（锁定）的集数 */
  lockedCount: number;
  /** 一期可为空，前端用渐变占位 */
  coverUrl: string | null;
  updatedAt: string;
}

export interface EpisodeSummary {
  no: number;
  title: string;
  synopsis: string;
  scriptStatus: ScriptStatus;
  /** 已拆解资产，剧本只读 */
  locked: boolean;
  revision: number;
  updatedAt: string;
  /** 分镜脚本状态 */
  storyboardStatus: StoryboardStatus;
  /** 片段数 */
  segmentCount: number;
  /** 各片段时长之和（秒） */
  segmentSeconds: number;
  /** 有「采用」视频的片段数 */
  doneSegmentCount: number;
  /** 合成好的成片；没有为 null */
  final: AssetDto | null;
  /** 成片之后有片段改了采用 / 顺序 / 增删：提示「成片已过期，可重新合成」 */
  finalStale: boolean;
  /** 开场钩子，没有为空串 */
  hook: string;
  /** 结尾悬念，没有为空串 */
  cliffhanger: string;
  /** 对应原文起始单元号，null = 未关联单元 */
  unitFrom: number | null;
  /** 对应原文结束单元号，null = 未关联单元 */
  unitTo: number | null;
}

export interface Episode extends EpisodeSummary {
  scriptMd: string;
}

export interface DramaJob {
  id: string;
  step: DramaStep;
  /** 本次范围；analyze / outline 为空 */
  episodes: number[];
  status: DramaJobStatus;
  /** 0–100 */
  progress: number;
  /** 实际扣费，结束后才有值 */
  credits: number;
  errorCode: number;
  errorMessage: string;
  createdAt: string;
  finishedAt: string | null;
  /** compose 成功的结果；其他步骤为 null */
  result: DramaJobResult | null;
  /** 所在的线：pipeline 系统自动，user 用户发起 */
  lane: DramaLane;
  /** plan 任务对应的起始单元号；其他任务为 null */
  batchFrom: number | null;
  /** plan 任务对应的结束单元号；其他任务为 null */
  batchTo: number | null;
}

/** compose 任务的结果：成片素材与被跳过（没有采用视频）的片段 seq（从 0 起，界面显示 seq+1） */
export interface DramaJobResult {
  asset: AssetDto;
  skippedSeqs: number[];
}

export interface Drama {
  id: string;
  title: string;
  /** 短剧专属画布 ID，用现有画布接口读写 */
  canvasId: string;
  status: DramaStatus;
  sourceKind: DramaSourceKind;
  genre: string;
  styleKey: string;
  styleCustom: string;
  ratio: DramaRatio;
  /** 0 = 自动 */
  targetEpisodes: number;
  episodeSeconds: number;
  /** 空串 = 跟随后台「文本」类别第一个启用模型 */
  textModel: string;
  /** 建剧时选的视频模型；空串 = 没指定 */
  videoModel: string;
  /** 建剧时选的人物 / 场景 / 物品图片模型；空串 = 没指定 */
  imageModels: DramaImageModels;
  outlineMd: string;
  charactersMd: string;
  revision: number;
  episodes: EpisodeSummary[];
  activeJobs: DramaJob[];
  createdAt: string;
  updatedAt: string;
  /** 原文字数，老数据为 0 */
  sourceChars: number;
  /** 原文单元总数，老数据为 0 */
  unitsTotal: number;
  /** 单元种类；老数据按「集」 */
  unitKind: UnitKind;
  /** 自动规划阶段；老项目为 done */
  phase: DramaPhase;
  /** 已规划到第几个单元 */
  planCursor: number;
  /** 规划范围终点；老数据为 0 */
  rangeEnd: number;
  /** 暂停原因文案，没有为空串 */
  pauseReason: string;
  /** 暂停原因分类，没有为空串 */
  pauseCode: PauseCode | "";
  /** 四个环节当前的提示词设置；老数据全是内置 */
  promptSettings: PromptSettings;
}

export interface DramaStyles {
  genres: { key: string; name: string }[];
  styles: { key: string; name: string; prompt: string }[];
}

/** 分页列表（沿用 OKPage） */
export interface DramaPage {
  items: DramaSummary[];
  total: number;
  page: number;
  pageSize: number;
}

/** 新建短剧的请求（契约 §2）：原文已在预检时上传，这里只带草稿 token 与开拍设置 */
export interface CreateDramaInput {
  /** 预检返回的草稿 id */
  sourceToken: string;
  /** 用户确认的原文类型 */
  sourceKind: "script" | "novel";
  /** 0 = 全部；否则规划到第 N 个单元 */
  rangeEnd: number;
  /** 空串用文件名 */
  title?: string;
  /** 成片画幅 */
  ratio: StartRatio;
  /** 题材 */
  genre?: string;
  /** 画风 key */
  styleKey: string;
  /** 自定义画风描述 */
  styleCustom?: string;
  /** 单集时长（秒） */
  episodeSeconds: number;
  /** 文本模型 key；空串 = 默认模型 */
  textModel?: string;
  /** 视频模型 key；省略 = 不指定 */
  videoModel?: string;
  /** 人物 / 场景 / 物品资产用的图片模型 key；省略的项 = 不指定 */
  imageModels?: { character?: string; scene?: string; prop?: string };
  /** 四个环节的提示词选择；省略的环节 = 内置 */
  prompts?: PromptChoices;
}

/** PATCH /dramas/:id 可改字段 */
export interface UpdateDramaInput {
  revision: number;
  title?: string;
  genre?: string;
  styleKey?: string;
  styleCustom?: string;
  ratio?: DramaRatio;
  textModel?: string;
  outlineMd?: string;
  charactersMd?: string;
  /** 重新选择各环节的提示词：保存时重新快照正文，只影响之后提交的任务 */
  prompts?: PromptChoices;
}

/** PATCH /dramas/:id/episodes/:no 可改字段 */
export interface UpdateEpisodeInput {
  revision: number;
  title?: string;
  synopsis?: string;
  scriptMd?: string;
  /** 开场钩子 */
  hook?: string;
  /** 结尾悬念 */
  cliffhanger?: string;
}

/** 提交 / 估价任务的请求 */
export interface DramaJobInput {
  step: DramaStep;
  episodes: number[];
  /** 仅老项目没定比例时由 breakdown 带上；新项目比例建剧时已定，后端忽略 */
  ratio?: DramaRatio;
  /** storyboard：所选视频模型的单段时长上限（秒），缺省 15，范围 4–60 */
  maxSeconds?: number;
}

/** WebSocket drama.episode 事件载荷：该集剧本状态已保存，需重新读取短剧详情。 */
export interface DramaEpisodeEvent {
  /** 短剧 ID。 */
  drama_id: string;
  /** 已保存状态的集号，从 1 开始。 */
  episode_no: number;
}

/** WebSocket drama.job 事件载荷 */
export interface DramaJobEvent {
  dramaId: string;
  job: DramaJob;
}

/** 片段状态（由最近一条 take 推出） */
export type SegmentStatus = "idle" | "running" | "done" | "error";

export interface BackendSegmentTake {
  task_id: string;
  status: TaskStatus;
  asset: BackendAssetDto | null;
  error_message: string;
  created_at: string;
}

export interface BackendSegmentView {
  id: string;
  episode_no: number;
  seq: number;
  seconds: number;
  prompt: string;
  takes: BackendSegmentTake[] | null;
  chosen_task_id: string;
  status: SegmentStatus;
  revision: number;
  updated_at: string;
  /** 按格式存的转写稿；老后端可能没有 */
  adaptations?: Partial<Record<AdaptFormat, BackendSegmentAdaptation>> | null;
}

/** 需要转写的提示词格式（Seedance 是原稿本身，不存转写稿） */
export type AdaptFormat = "h3" | "generic";

export interface BackendSegmentAdaptation {
  prompt: string;
  source_hash: string;
  edited: boolean;
  updated_at: string;
}

/** 一份转写稿 */
export interface SegmentAdaptation {
  /** `@[名](节点id)` 写法的转写结果 */
  prompt: string;
  /** 转写时原稿全文的 SHA-256（小写 hex）；与当前原稿不同 = 已过期 */
  sourceHash: string;
  /** 用户手改过；重新转写前要确认覆盖 */
  edited: boolean;
  updatedAt: string;
}

/** 片段的一次视频生成结果 */
export interface SegmentTake {
  taskId: string;
  status: TaskStatus;
  asset: AssetDto | null;
  errorMessage: string;
  createdAt: string;
}

export interface Segment {
  id: string;
  episodeNo: number;
  /** 从 0 开始，按它排序 */
  seq: number;
  seconds: number;
  /** `@[名](短剧画布节点 id)` 形式的提示词 */
  prompt: string;
  /** 按创建时间正序 */
  takes: SegmentTake[];
  /** 采用的那一条；空串 = 没有 */
  chosenTaskId: string;
  status: SegmentStatus;
  revision: number;
  updatedAt: string;
  /** 按格式存的转写稿（原稿始终是 prompt，Seedance 格式） */
  adaptations: Partial<Record<AdaptFormat, SegmentAdaptation>>;
}

/** PATCH /drama-segments/:sid 可改字段 */
export interface UpdateSegmentInput {
  revision: number;
  prompt?: string;
  seconds?: number;
  chosenTaskId?: string;
  /** 手改某个格式的转写稿 */
  adaptation?: { format: AdaptFormat; prompt: string };
}

/** POST /drama-segments/:sid/adapt 请求体 */
export interface AdaptSegmentInput {
  format: AdaptFormat;
  revision: number;
}

/** POST /dramas/:id/episodes/:no/segments 请求体 */
export interface AddSegmentInput {
  prompt?: string;
  seconds?: number;
  afterSeq?: number;
}

/** generate 请求体：input 与 POST /generation-tasks 的 input 同构 */
export interface SegmentGenerateBody {
  model_id: string;
  input: Record<string, unknown>;
}

export type SegmentGenerateResult = { task: TaskView };

/** WebSocket drama.segment 事件载荷（原样 snake_case） */
export interface DramaSegmentEvent {
  drama_id: string;
  episode_no: number;
  segment: BackendSegmentView;
}

/** 预检里的一条处理记录 */
export interface SourceIssue {
  /** 种类：noise_removed / split_long / episode_too_long / empty / duplicate 等 */
  kind: string;
  /** 涉及的行数 / 单元数 */
  count: number;
  /** 给用户看的一句话 */
  message: string;
}

/** 预检切出来的一个单元（只有目录，没有正文） */
export interface SourceUnit {
  /** 单元号，从 1 起 */
  no: number;
  /** 标题 */
  title: string;
  /** 字数 */
  chars: number;
}

/** 自动部分（研读 + 规划）的预估 */
export interface SourceEstimate {
  /** 预计集数下限 */
  episodesMin: number;
  /** 预计集数上限 */
  episodesMax: number;
  /** 集数是否精确（剧本型输入） */
  episodesExact: boolean;
  /** 预计积分 */
  credits: number;
  /** 预计耗时（分钟） */
  minutes: number;
}

/** 预检结果：弹窗「开拍设置」的来源行与初始预估 */
export interface SourcePreflight {
  /** 草稿 id，24 小时有效 */
  sourceToken: string;
  /** 文件名；粘贴文本时由后端给默认名 */
  filename: string;
  /** 去首尾空白后的 Unicode 字符数 */
  chars: number;
  /** 识别结果；unknown 表示认不出，用户必须手选 */
  kindGuess: "script" | "novel" | "unknown";
  /** 一句话依据，界面直接显示 */
  kindSignals: string;
  /** 单元种类 */
  unitKind: UnitKind;
  /** 单元目录 */
  units: SourceUnit[];
  /** 单元总数 */
  unitsTotal: number;
  /** 处理记录 */
  issues: SourceIssue[];
  /** 范围 = 全部时的预估 */
  estimate: SourceEstimate;
}

/** 预检请求：file 与 text 二选一 */
export interface PreflightInput {
  /** 上传的文件 */
  file?: File;
  /** 粘贴的正文 */
  text?: string;
  /** 用户改了类型后带上，后端按新类型重新切分 */
  sourceKind?: "script" | "novel";
  /** 估价用的文本模型，空串 = 默认模型 */
  textModel?: string;
  /** 估价用的单集时长（秒） */
  episodeSeconds?: number;
}

/** 草稿的预估请求 */
export interface SourceEstimateInput {
  /** 用户选的类型 */
  sourceKind: "script" | "novel";
  /** 0 = 全部 */
  rangeEnd: number;
  /** 单集时长（秒） */
  episodeSeconds: number;
  /** 文本模型 key，空串 = 默认模型 */
  textModel: string;
}

/** 内置提示词的一页（剧本改编有「分集规划 / 分集剧本」两页，其他环节一页） */
export interface PromptPage {
  /** 页 key：plan / scripts / breakdown / asset_prompts / storyboard */
  key: string;
  /** 页签名 */
  label: string;
  /** 内置正文 */
  body: string;
}

/** 一个环节的内置提示词 */
export interface PromptTemplate {
  /** 环节 */
  step: PromptStep;
  /** 可替换的正文页 */
  pages: PromptPage[];
}

/** 「我的提示词」一项 */
export interface MyPrompt {
  /** id */
  id: string;
  /** 环节 */
  step: PromptStep;
  /** 名称，同环节内不重复 */
  name: string;
  /** 各页正文，键是该环节 pages[].key 的子集，缺的页用内置 */
  bodies: Record<string, string>;
  /** 乐观锁版本 */
  revision: number;
  /** 最近更新时间 */
  updatedAt: string;
}

/** 新建 / 修改「我的提示词」的内容 */
export interface MyPromptInput {
  /** 环节 */
  step: PromptStep;
  /** 名称，≤ 40 字 */
  name: string;
  /** 各页正文 */
  bodies: Record<string, string>;
  /** 修改时带上的乐观锁版本 */
  revision?: number;
}

/** 后台已启用且标了该环节的技能 */
export interface DramaSkill {
  /** 技能 name */
  name: string;
  /** 显示名 */
  title: string;
  /** 一句话说明 */
  description: string;
}

/** 一条原文单元（对应原文查看） */
export interface DramaUnit extends SourceUnit {
  /** 正文，仅读单个单元时有 */
  text?: string;
}

/** 估价结果：积分，以及 scripts 时选中集里上一集既没剧本也不在选择里的集号 */
export interface JobEstimate {
  /** 预计积分 */
  credits: number;
  /** 衔接缺口：这些集的上一集没有剧本 */
  gapEpisodes: number[];
}

/** 预检接口的后端原样响应（snake_case） */
export interface BackendSourcePreflight {
  /** 草稿 id */
  source_token: string;
  /** 文件名 */
  filename: string;
  /** 字数 */
  chars: number;
  /** 识别结果 */
  kind_guess: "script" | "novel" | "unknown";
  /** 一句话依据 */
  kind_signals: string;
  /** 单元种类 */
  unit_kind: UnitKind;
  /** 单元目录，可能为 null */
  units: SourceUnit[] | null;
  /** 单元总数 */
  units_total: number;
  /** 处理记录，可能为 null */
  issues: SourceIssue[] | null;
  /** 预估 */
  estimate: {
    episodes_min: number;
    episodes_max: number;
    episodes_exact: boolean;
    credits: number;
    minutes: number;
  };
}

import service from "@/utils/requests/service";

import type {
  AngleOption,
  AssetRef,
  BackendAssetRef,
  BackendMarketingOptions,
  BackendMethod,
  BackendProductFacts,
  BackendProject,
  BackendProjectSummary,
  BackendReverse,
  BackendReverseShot,
  BackendRound,
  BackendRoundSettings,
  BackendScript,
  BackendScriptPrompt,
  BackendStepRun,
  BackendTaskLite,
  CreateProjectInput,
  CreateRoundInput,
  MarketingOptions,
  MethodInfo,
  ProductFacts,
  Project,
  ProjectPage,
  ProjectSummary,
  Quote,
  QuoteInput,
  RenderAllResult,
  Reverse,
  ReverseShot,
  Round,
  RoundSettings,
  Script,
  ScriptBar,
  ScriptPrompt,
  StepRun,
  TaskLite,
  UpdateRoundSettingsInput,
} from "./type";

/** 新建一次出片 / 一键出片的幂等键；同一次点击的重试要沿用同一个 */
export const newIdempotencyKey = (): string => crypto.randomUUID();

const mapAssetRef = (raw: BackendAssetRef): AssetRef => ({ id: String(raw.id), url: raw.url });

/** 后端方法说明 -> 前端；没有方法为 null */
export const mapMethod = (raw: BackendMethod | null | undefined): MethodInfo | null =>
  raw
    ? {
        source: raw.source,
        skillName: raw.skill_name ?? "",
        skillTitle: raw.skill_title ?? "",
        version: raw.version ?? 0,
        fallback: raw.fallback ?? false,
      }
    : null;

/** 后端事实卡 -> 前端 */
export const mapProductFacts = (raw: BackendProductFacts): ProductFacts => ({
  name: raw.name,
  category: raw.category ?? "",
  anchors: raw.anchors ?? [],
  sellingPoints: (raw.selling_points ?? []).map((item) => ({
    text: item.text,
    proof: item.proof ?? "",
    source: item.source,
  })),
  prohibited: raw.prohibited ?? [],
  audience: raw.audience ?? "",
  cta: raw.cta ?? "",
});

/** 前端事实卡 -> 后端请求体 */
export const toBackendProductFacts = (facts: ProductFacts): BackendProductFacts => ({
  name: facts.name,
  category: facts.category,
  anchors: facts.anchors,
  selling_points: facts.sellingPoints.map((item) => ({
    text: item.text,
    proof: item.proof,
    source: item.source,
  })),
  prohibited: facts.prohibited,
  audience: facts.audience,
  cta: facts.cta,
});

/** 后端轮次设置 -> 前端 */
export const mapSettings = (raw: BackendRoundSettings): RoundSettings => ({
  platform: raw.platform,
  language: raw.language,
  angles: raw.angles ?? [],
  autoCount: raw.auto_count,
  videoModel: raw.video_model,
  textModel: raw.text_model ?? "",
  params: raw.params ?? {},
  description: raw.description ?? "",
});

/** 前端轮次设置 -> 后端请求体 */
export const toBackendSettings = (settings: RoundSettings): BackendRoundSettings => ({
  platform: settings.platform,
  language: settings.language,
  angles: settings.angles,
  auto_count: settings.autoCount,
  video_model: settings.videoModel,
  text_model: settings.textModel,
  params: settings.params,
  description: settings.description,
});

/** 后端环节运行 -> 前端 */
export const mapStepRun = (raw: BackendStepRun): StepRun => ({
  id: raw.id,
  step: raw.step,
  status: raw.status,
  method: mapMethod(raw.method),
  textModel: raw.text_model ?? "",
  error: raw.error ?? "",
  attempts: raw.attempts ?? 0,
  charged: raw.charged ?? 0,
});

const mapReverseShot = (raw: BackendReverseShot): ReverseShot => ({
  id: raw.id,
  start: raw.start,
  end: raw.end,
  isOpening3s: raw.is_opening_3s,
  shotSize: raw.shot_size ?? "",
  camera: raw.camera ?? "",
  subject: raw.subject ?? "",
  action: raw.action ?? "",
  scene: raw.scene ?? "",
  lighting: raw.lighting ?? "",
  person: raw.person ?? "",
  productInFrame: raw.product_in_frame ?? "",
  productRole: raw.product_role,
  dialogue: raw.dialogue ?? "",
  onscreenText: raw.onscreen_text ?? "",
  keyColors: raw.key_colors ?? [],
});

/** 后端拆解 -> 前端 */
export const mapReverse = (raw: BackendReverse): Reverse => ({
  overall: {
    style: raw.overall?.style ?? "",
    narrativeArc: raw.overall?.narrative_arc ?? "",
    whyViral: raw.overall?.why_viral ?? "",
    transcript: raw.overall?.transcript ?? "",
    category: raw.overall?.category ?? "",
  },
  shots: (raw.shots ?? []).map(mapReverseShot),
  brands: raw.brands ?? [],
  claims: raw.claims ?? [],
});

/** 前端拆解 -> 后端请求体（服务端只认 shots 的 action / dialogue / product_role / key_colors） */
export const toBackendReverse = (reverse: Reverse): BackendReverse => ({
  overall: {
    style: reverse.overall.style,
    narrative_arc: reverse.overall.narrativeArc,
    why_viral: reverse.overall.whyViral,
    transcript: reverse.overall.transcript,
    category: reverse.overall.category,
  },
  shots: reverse.shots.map((shot) => ({
    id: shot.id,
    start: shot.start,
    end: shot.end,
    is_opening_3s: shot.isOpening3s,
    shot_size: shot.shotSize,
    camera: shot.camera,
    subject: shot.subject,
    action: shot.action,
    scene: shot.scene,
    lighting: shot.lighting,
    person: shot.person,
    product_in_frame: shot.productInFrame,
    product_role: shot.productRole,
    dialogue: shot.dialogue,
    onscreen_text: shot.onscreenText,
    key_colors: shot.keyColors,
  })),
  brands: reverse.brands,
  claims: reverse.claims,
});

/** 后端出片任务摘要 -> 前端 */
export const mapTaskLite = (raw: BackendTaskLite): TaskLite => ({
  id: String(raw.id),
  taskRef: raw.task_ref ?? "",
  status: raw.status,
  progress: typeof raw.progress === "number" ? raw.progress : null,
  outputs: (raw.outputs ?? [])
    .filter((item) => !!item.url)
    .map((item) => ({ url: item.url as string, posterUrl: item.poster_url ?? "" })),
  errorMessage: raw.error_message ?? "",
});

/** 后端脚本 -> 前端 */
export const mapScript = (raw: BackendScript): Script => ({
  id: raw.id,
  roundId: raw.round_id,
  kind: raw.kind,
  angle: raw.angle,
  title: raw.title,
  hook: raw.hook ?? "",
  shots: (raw.shots ?? []).map((shot) => ({
    start: shot.start,
    end: shot.end,
    purpose: shot.purpose ?? "",
    shot: shot.shot ?? "",
    camera: shot.camera ?? "",
    scene: shot.scene ?? "",
    narration: shot.narration ?? "",
    refShot: shot.ref_shot ?? null,
  })),
  music: raw.music ?? "",
  voice: raw.voice ?? "",
  flags: (raw.flags ?? []).map((flag) => ({ shot: flag.shot, type: flag.type, text: flag.text })),
  status: raw.status,
  edited: raw.edited ?? false,
  method: mapMethod(raw.method),
  takes: (raw.takes ?? []).map(mapTaskLite),
  renderCredits: raw.render_credits ?? 0,
  error: raw.error ?? "",
  revision: raw.revision,
});

/** 后端轮次 -> 前端 */
export const mapRound = (raw: BackendRound): Round => ({
  id: raw.id,
  no: raw.no,
  status: raw.status,
  stage: raw.stage ?? "",
  error: raw.error ?? "",
  warnings: raw.warnings ?? [],
  settings: mapSettings(raw.settings),
  refVideo: raw.ref_video
    ? { id: String(raw.ref_video.id), url: raw.ref_video.url, duration: raw.ref_video.duration }
    : null,
  modelAsset: raw.model_asset ? mapAssetRef(raw.model_asset) : null,
  steps: (raw.steps ?? []).map(mapStepRun),
  planCount: raw.plan_count ?? 0,
  reverse: raw.reverse ? mapReverse(raw.reverse) : null,
  reverseRevision: raw.reverse_revision ?? 0,
  reverseEdited: raw.reverse_edited ?? false,
  reverseDirty: raw.reverse_dirty ?? false,
  scripts: (raw.scripts ?? []).map(mapScript),
  frozen: raw.frozen ?? 0,
  charged: raw.charged ?? 0,
  createdAt: raw.created_at,
});

/** 后端项目详情 -> 前端 */
export const mapProject = (raw: BackendProject): Project => ({
  id: raw.id,
  title: raw.title,
  product: raw.product ? mapProductFacts(raw.product) : null,
  productAssets: (raw.product_assets ?? []).map(mapAssetRef),
  modelAsset: raw.model_asset ? mapAssetRef(raw.model_asset) : null,
  revision: raw.revision,
  rounds: (raw.rounds ?? []).map(mapRound),
  createdAt: raw.created_at,
  updatedAt: raw.updated_at,
});

/** 后端列表项 -> 前端 */
export const mapProjectSummary = (raw: BackendProjectSummary): ProjectSummary => ({
  id: raw.id,
  title: raw.title,
  coverUrl: raw.cover_url ?? "",
  scripting: raw.scripting,
  rendering: raw.rendering ?? 0,
  lastRoundFailed: raw.last_round_failed ?? false,
  scriptCounts: {
    draft: raw.script_counts?.draft ?? 0,
    rendering: raw.script_counts?.rendering ?? 0,
    done: raw.script_counts?.done ?? 0,
    failed: raw.script_counts?.failed ?? 0,
  },
  bars: (raw.bars ?? []) as ScriptBar[],
  updatedAt: raw.updated_at,
});

/**
 * 平台与角度预设。
 * @returns 平台列表和内置角度
 */
export const getMarketingOptions = async (): Promise<MarketingOptions> => {
  const raw = await service.get<BackendMarketingOptions>("/marketing/options");
  return {
    platforms: (raw.platforms ?? []).map((item) => ({
      key: item.key,
      name: item.name,
      aspectRatio: item.aspect_ratio,
      language: item.language,
      style: item.style ?? "",
    })),
    angles: (raw.angles ?? []).map((item): AngleOption => ({ ...item })),
    refVideoMaxSeconds:
      typeof raw.ref_video_max_seconds === "number" && raw.ref_video_max_seconds > 0
        ? raw.ref_video_max_seconds
        : null,
  };
};

/**
 * 写脚本预估积分（各环节之和，不含出片）。预估失败不打扰用户，发送时由后端兜底。
 * @param input 项目、是否带参考视频与设置
 * @returns 总积分与分环节明细
 */
export const quoteMarketing = (input: QuoteInput): Promise<Quote> =>
  service.post<Quote>(
    "/marketing/quote",
    {
      project_id: input.projectId,
      has_ref: input.hasRef,
      settings: toBackendSettings(input.settings),
    },
    { silent: true },
  );

/**
 * 我的营销片列表，按更新时间倒序。
 * @param params 页码、每页条数与标题关键词
 * @returns 当前页与总数
 */
export const listMarketingProjects = async (
  params: { page?: number; pageSize?: number; keyword?: string } = {},
): Promise<ProjectPage> => {
  const res = await service.get<{
    list: BackendProjectSummary[] | null;
    total: number;
    page: number;
    page_size: number;
  }>("/marketing-projects", {
    page: params.page,
    page_size: params.pageSize,
    keyword: params.keyword,
  });
  return { items: (res.list ?? []).map(mapProjectSummary), total: res.total };
};

/**
 * 新建营销项目并开始第一轮写脚本。
 * @param input 商品图、模特图、参考视频与设置
 * @returns 项目详情（含第一轮，scripting）
 */
export const createMarketingProject = async (input: CreateProjectInput): Promise<Project> =>
  mapProject(
    await service.post<BackendProject>("/marketing-projects", {
      product_asset_ids: input.productAssetIds,
      model_asset_id: input.modelAssetId,
      ref_video_asset_id: input.refVideoAssetId,
      settings: toBackendSettings(input.settings),
    }),
  );

/**
 * 项目详情。
 * @param id 项目 ID
 * @param silent 失败不弹全局提示（后台对账时用）
 * @returns 项目详情
 */
export const getMarketingProject = async (id: string, silent = false): Promise<Project> =>
  mapProject(await service.get<BackendProject>(`/marketing-projects/${id}`, undefined, { silent }));

/**
 * 改项目标题、事实卡或商品图；revision 冲突返回 68009，换商品图时有轮次在写脚本返回 68007。
 * @param id 项目 ID
 * @param body 要改的字段与当前 revision
 * @returns 最新项目详情
 */
export const updateMarketingProject = async (
  id: string,
  body: { title?: string; product?: ProductFacts; productAssetIds?: number[]; revision: number },
): Promise<Project> =>
  mapProject(
    await service.put<BackendProject>(`/marketing-projects/${id}`, {
      title: body.title,
      product: body.product ? toBackendProductFacts(body.product) : undefined,
      product_asset_ids: body.productAssetIds,
      revision: body.revision,
    }),
  );

/**
 * 按项目当前商品图重新识别外观锚点：在最新一轮重跑事实卡环节，按文本模型计费，只替换外观锚点。
 * @param id 项目 ID
 * @returns 最新一轮（写脚本中）
 */
export const refreshMarketingFacts = async (id: string): Promise<Round> =>
  mapRound(await service.post<BackendRound>(`/marketing-projects/${id}/refresh-facts`));

/**
 * 改一轮的出片设置（视频模型、分辨率、换模特图），只影响之后的出片。
 * @param roundId 轮次 ID
 * @param input 新模型与参数（画幅、时长不变）、换用的模特图
 * @returns 最新轮次
 */
export const updateMarketingRoundSettings = async (
  roundId: string,
  input: UpdateRoundSettingsInput,
): Promise<Round> =>
  mapRound(
    await service.put<BackendRound>(`/marketing-rounds/${roundId}/settings`, {
      video_model: input.videoModel,
      params: input.params,
      model_asset_id: input.modelAssetId,
    }),
  );

/**
 * 删除项目；有进行中的任务返回 68010。
 * @param id 项目 ID
 */
export const deleteMarketingProject = async (id: string): Promise<void> => {
  await service.delete(`/marketing-projects/${id}`);
};

/**
 * 在已有项目上新开一轮；项目正在写脚本返回 68007。
 * @param id 项目 ID
 * @param input 模特图、参考视频与设置
 * @returns 新轮次
 */
export const createMarketingRound = async (id: string, input: CreateRoundInput): Promise<Round> =>
  mapRound(
    await service.post<BackendRound>(`/marketing-projects/${id}/rounds`, {
      model_asset_id: input.modelAssetId,
      ref_video_asset_id: input.refVideoAssetId,
      settings: toBackendSettings(input.settings),
    }),
  );

/**
 * 取消写脚本中的轮次，冻结积分退回。
 * @param roundId 轮次 ID
 * @returns 最新轮次
 */
export const cancelMarketingRound = async (roundId: string): Promise<Round> =>
  mapRound(await service.post<BackendRound>(`/marketing-rounds/${roundId}/cancel`));

/**
 * 保存拆解修改；revision 冲突返回 68009，内容不合法 68015。
 * @param roundId 轮次 ID
 * @param reverse 改后的拆解
 * @param revision 拆解当前 revision
 * @returns 最新轮次
 */
export const saveMarketingReverse = async (
  roundId: string,
  reverse: Reverse,
  revision: number,
): Promise<Round> =>
  mapRound(
    await service.put<BackendRound>(`/marketing-rounds/${roundId}/reverse`, {
      reverse: toBackendReverse(reverse),
      reverse_revision: revision,
    }),
  );

/**
 * 拆解恢复模型原稿。
 * @param roundId 轮次 ID
 * @returns 最新轮次
 */
export const restoreMarketingReverse = async (roundId: string): Promise<Round> =>
  mapRound(await service.post<BackendRound>(`/marketing-rounds/${roundId}/reverse/restore`));

/**
 * 按当前拆解重写跟拍版（只重跑跟拍环节，按文本模型计费）。
 * @param roundId 轮次 ID
 * @returns 最新轮次（跟拍脚本为 rewriting）
 */
export const refollowMarketingRound = async (roundId: string): Promise<Round> =>
  mapRound(await service.post<BackendRound>(`/marketing-rounds/${roundId}/refollow`));

/**
 * 重试一个失败的环节，只重跑它，方法用当前绑定。
 * @param stepRunId 环节运行记录 ID
 * @returns 最新轮次
 */
export const retryMarketingStep = async (stepRunId: string): Promise<Round> =>
  mapRound(await service.post<BackendRound>(`/marketing-step-runs/${stepRunId}/retry`));

/**
 * 保存脚本旁白（服务端重扫问题旁白）；revision 冲突返回 68009。
 * @param scriptId 脚本 ID
 * @param narrations 每个镜头的旁白，长度等于镜头数
 * @param revision 脚本当前 revision
 * @returns 最新脚本
 */
export const saveMarketingScript = async (
  scriptId: string,
  narrations: string[],
  revision: number,
): Promise<Script> =>
  mapScript(
    await service.put<BackendScript>(`/marketing-scripts/${scriptId}`, { narrations, revision }),
  );

/**
 * 脚本恢复模型原稿。
 * @param scriptId 脚本 ID
 * @returns 最新脚本
 */
export const restoreMarketingScript = async (scriptId: string): Promise<Script> =>
  mapScript(await service.post<BackendScript>(`/marketing-scripts/${scriptId}/restore`));

/**
 * 查看格式化后的视频提示词。
 * @param scriptId 脚本 ID
 * @param videoModel 视频模型 key；不传用本轮的
 * @returns 格式与提示词正文
 */
export const getMarketingPrompt = async (
  scriptId: string,
  videoModel?: string,
): Promise<ScriptPrompt> =>
  mapPrompt(
    await service.get<BackendScriptPrompt>(`/marketing-scripts/${scriptId}/prompt`, {
      video_model: videoModel,
    }),
  );

/** 后端提示词预览 -> 前端 */
export const mapPrompt = (raw: BackendScriptPrompt): ScriptPrompt => ({
  format: raw.format,
  prompt: raw.prompt,
  adaptation: raw.adaptation
    ? {
        prompt: raw.adaptation.prompt,
        stale: raw.adaptation.stale,
        edited: raw.adaptation.edited,
        updatedAt: raw.adaptation.updated_at,
      }
    : null,
  adaptCredits: raw.adapt_credits ?? 0,
});

/**
 * 把脚本的出片提示词转写成视频模型的写法（目前只有 H3），已有转写稿也重新转写；扣文本模型积分。
 * 积分不足 40001；转写失败 68018。
 * @param scriptId 脚本 ID
 * @param videoModel 视频模型 key；不传用本轮的
 * @returns 最新的提示词预览
 */
export const adaptMarketingScript = async (
  scriptId: string,
  videoModel?: string,
): Promise<ScriptPrompt> =>
  mapPrompt(
    await service.post<BackendScriptPrompt>(`/marketing-scripts/${scriptId}/adapt`, {
      video_model: videoModel,
    }),
  );

/**
 * 手改转写稿（标记已修改；原稿变了仍会过期，出片时重新转写）。
 * @param scriptId 脚本 ID
 * @param prompt 改后的转写稿
 * @returns 最新的提示词预览
 */
export const editMarketingAdaptation = async (
  scriptId: string,
  prompt: string,
): Promise<ScriptPrompt> =>
  mapPrompt(
    await service.put<BackendScriptPrompt>(`/marketing-scripts/${scriptId}/adaptation`, {
      format: "h3",
      prompt,
    }),
  );

/**
 * 单条出片。idempotencyKey 要在同一次点击的重试里沿用。
 * @param scriptId 脚本 ID
 * @param idempotencyKey 幂等键
 * @returns 最新脚本（rendering）
 */
export const renderMarketingScript = async (
  scriptId: string,
  idempotencyKey: string,
): Promise<Script> =>
  mapScript(
    await service.post<BackendScript>(`/marketing-scripts/${scriptId}/render`, {
      idempotency_key: idempotencyKey,
    }),
  );

/**
 * 全部出片：在出片中、旁白有问题或积分不足的脚本会被跳过。
 * @param roundId 轮次 ID
 * @param idempotencyKey 幂等键
 * @returns 最新轮次与被跳过的脚本
 */
export const renderMarketingRound = async (
  roundId: string,
  idempotencyKey: string,
): Promise<RenderAllResult> => {
  const raw = await service.post<{
    round: BackendRound;
    skipped: Array<{ script_id: string; reason: string }> | null;
  }>(`/marketing-rounds/${roundId}/render`, { idempotency_key: idempotencyKey });
  return {
    round: mapRound(raw.round),
    skipped: (raw.skipped ?? []).map((item) => ({ scriptId: item.script_id, reason: item.reason })),
  };
};

/**
 * 把脚本发到画布精修，新建画布并返回 ID。
 * @param scriptId 脚本 ID
 * @returns 新画布 ID
 */
export const sendMarketingScriptToCanvas = async (scriptId: string): Promise<string> =>
  (await service.post<{ canvas_id: string }>(`/marketing-scripts/${scriptId}/to-canvas`)).canvas_id;

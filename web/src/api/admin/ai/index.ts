import service from "@/utils/requests/service";
import type { TaskView } from "@/api/generation-task/type";
import { adminAiEndpoints as ep, saveBody } from "./endpoints";
import type {
  AdminMe,
  AdminOverview,
  AdminPage,
  AdminStats,
  AdminStatsDays,
  ChannelCreateRequest,
  ChannelListItem,
  ChannelOption,
  ChannelUpdateRequest,
  ChannelLoad,
  ChannelView,
  ConfigDetail,
  ConfigListItem,
  DeleteCheckResult,
  DeleteTarget,
  ImportModelsResult,
  ListChannelsParams,
  ListModelsParams,
  ListPluginsParams,
  ModelLimits,
  ModelListItem,
  PluginListItem,
  PluginUploadResult,
  PluginView,
  PullModelsResult,
  SaveModelResult,
  TestRunTrace,
  UpgradeModelsResult,
  ValidateResult,
} from "./type";

// ---------------------------------------------------------------- 当前用户

/**
 * 当前管理端角色；普通用户调用得到 403
 * @returns 用户 ID 与角色
 */
export const getAdminMe = () => service.get<AdminMe>(ep.me(), undefined);

// ---------------------------------------------------------------- 总览统计

/**
 * 总览页任务统计：每日任务量与按模型、按类型的调用占比
 * @param days 统计天数，7 或 30
 * @param silent 为 true 时失败不弹全局提示：后台定时刷新用，页面自己显示失败状态
 * @returns 统计数据
 */
export const getAdminStats = (days: AdminStatsDays, silent = false) =>
  service.get<AdminStats>(ep.stats(), { days }, silent ? { silent: true } : undefined);

/**
 * 总览页汇总：各类数量、可用数、待处理清单、渠道负载卡片要的精简渠道；一次请求，不拉全量清单
 * @param silent 为 true 时失败不弹全局提示：后台刷新用，页面自己显示失败状态
 * @returns 汇总；数组字段缺失时补成空数组
 */
export const getAdminOverview = async (silent = false): Promise<AdminOverview> => {
  const ov = await service.get<AdminOverview>(
    ep.overview(),
    undefined,
    silent ? { silent: true } : undefined,
  );
  return { ...ov, todos: ov.todos ?? [], channel_briefs: ov.channel_briefs ?? [] };
};

// ---------------------------------------------------------------- 插件

/** 去掉空值，免得把 `q=` 之类的空参数发给后端 */
const clean = (params: object) =>
  Object.fromEntries(Object.entries(params).filter(([, v]) => v !== undefined && v !== ""));

/** 分页响应兜底：后端对空列表可能返回 null */
const pageOf = <T>(
  page: AdminPage<T> | null | undefined,
  params: { page?: number; page_size?: number },
): AdminPage<T> => ({
  list: page?.list ?? [],
  total: page?.total ?? 0,
  page: page?.page ?? params.page ?? 1,
  page_size: page?.page_size ?? params.page_size ?? 20,
});

const withVersions = <T extends PluginView>(item: T): T => ({
  ...item,
  versions: item.versions ?? [],
});

/**
 * 插件分页列表（后端分页）；每行带版本、模型数、旧版模型数、在线模型数
 * @param params 搜索词、能力、来源、状态与分页
 * @returns 当前页插件与命中总数
 */
export const listPlugins = async (
  params: ListPluginsParams = {},
): Promise<AdminPage<PluginListItem>> => {
  const page = pageOf(
    await service.get<AdminPage<PluginListItem> | null>(ep.plugins(), clean(params)),
    params,
  );
  return { ...page, list: page.list.map(withVersions) };
};

/**
 * 全部插件（带全部版本和 meta，不分页）：模型弹窗的协议列表、协议设置项表单用
 * @returns 插件列表；versions 缺失时补成空数组
 */
export const listPluginOptions = async (): Promise<PluginView[]> =>
  ((await service.get<PluginView[] | null>(ep.pluginOptions(), undefined)) ?? []).map(withVersions);

/**
 * 插件详情（带全部版本）：深链 ?key= 打开插件弹窗时用，不依赖列表
 * @param key 插件 key
 * @returns 插件视图
 */
export const getPlugin = async (key: string): Promise<PluginView> =>
  withVersions(await service.get<PluginView>(ep.plugin(key), undefined));

/**
 * 上传插件文件（super_admin）；预检不通过也返回 200，结果在 accepted / issues 里。
 * 版本号被自动调整时结果带 version_adjusted，没有调整时为 null
 * @param file .js 文件（≤512KB）
 * @returns 预检结果
 */
export const uploadPlugin = async (file: File): Promise<PluginUploadResult> => {
  const form = new FormData();
  form.append("file", file);
  const result = await service.upload<PluginUploadResult | null>(ep.plugins(), form);
  return {
    accepted: !!result?.accepted,
    issues: result?.issues ?? [],
    version: result?.version ?? null,
    version_adjusted: result?.version_adjusted ?? null,
  };
};

/**
 * 插件启停（super_admin）
 * @param key 插件 key
 * @param enabled 是否启用
 */
export const setPluginEnabled = (key: string, enabled: boolean) =>
  service.put<unknown>(ep.pluginEnabled(key), { enabled });

/**
 * 删除插件版本（super_admin）；被模型或进行中的任务引用时 409
 * @param key 插件 key
 * @param version semver 版本号
 */
export const deletePluginVersion = (key: string, version: string) =>
  service.delete<unknown>(ep.pluginVersion(key, version));

/**
 * 批量把协议固定在该插件旧版本上的模型升到指定版本（super_admin）；逐个处理，失败不中断
 * @param key 插件 key
 * @param version 目标版本；不传升到最新版本
 * @returns 升级成功与失败的模型
 */
export const upgradePluginModels = async (
  key: string,
  version?: string,
): Promise<UpgradeModelsResult> => {
  const result = await service.post<UpgradeModelsResult | null>(
    ep.pluginUpgradeModels(key),
    version ? { version } : {},
  );
  return {
    to_version: result?.to_version ?? version ?? "",
    upgraded: result?.upgraded ?? [],
    failed: result?.failed ?? [],
  };
};

// ---------------------------------------------------------------- 删除

/**
 * 删除预检：列出谁在引用它（只给界面看，真正的判断以删除接口事务内为准）
 * @param target 插件 / 渠道 / 模型
 * @param key 对象 key
 * @returns 阻断原因；为空表示可以删
 */
export const checkDelete = async (
  target: DeleteTarget,
  key: string,
): Promise<DeleteCheckResult> => {
  const result = await service.get<DeleteCheckResult | null>(
    ep.deleteCheck(target, key),
    undefined,
  );
  return {
    blockers: (result?.blockers ?? []).map((item) => ({ ...item, refs: item.refs ?? [] })),
  };
};

/**
 * 删除插件（整个，含全部版本）/ 渠道（连同 Key 与 Secret Key）/ 模型（连同全部历史版本，彻底删除）；仍被引用时 409
 * @param target 插件 / 渠道 / 模型
 * @param key 对象 key
 */
export const deleteTarget = (target: DeleteTarget, key: string) =>
  service.delete<unknown>(ep.remove(target, key));

// ---------------------------------------------------------------- 渠道

/**
 * 渠道分页列表（后端分页，渠道顺序）；每行带健康状态和模型数
 * @param params 搜索词、状态与分页
 * @returns 当前页渠道与命中总数
 */
export const listChannels = async (
  params: ListChannelsParams = {},
): Promise<AdminPage<ChannelListItem>> =>
  pageOf(
    await service.get<AdminPage<ChannelListItem> | null>(ep.channels(), clean(params)),
    params,
  );

/**
 * 全部渠道（带健康状态，不分页，渠道顺序）：主从页左栏、模型弹窗选渠道、概览用
 * @returns 渠道列表
 */
export const listChannelOptions = async (): Promise<ChannelOption[]> =>
  (await service.get<ChannelOption[] | null>(ep.channelOptions(), undefined)) ?? [];

/**
 * 各渠道当前的任务负载（生成中 / 排队数）
 * @param silent 为 true 时失败不弹全局提示：定时轮询用，页面自己显示“刷新失败”
 * @returns 有未完成任务的渠道的负载
 */
export const listChannelLoads = async (silent = false) =>
  (await service.get<ChannelLoad[] | null>(
    ep.channelLoads(),
    undefined,
    silent ? { silent: true } : undefined,
  )) ?? [];

/**
 * 渠道详情
 * @param key 渠道 key
 * @returns 渠道视图
 */
export const getChannel = (key: string) => service.get<ChannelView>(ep.channel(key), undefined);

/**
 * 新建渠道（super_admin）
 * @param data 渠道字段
 * @returns 新建的渠道
 */
export const createChannel = (data: ChannelCreateRequest) =>
  service.post<ChannelView>(ep.channels(), data);

/**
 * 更新渠道（super_admin），字段不传表示不改
 * @param key 渠道 key
 * @param data 要改的字段
 * @returns 更新后的渠道
 */
export const updateChannel = (key: string, data: ChannelUpdateRequest) =>
  service.put<ChannelView>(ep.channel(key), data);

/**
 * 设置渠道 API Key（super_admin）；只写，设置后任何接口都读不回明文
 * @param key 渠道 key
 * @param value Key 明文
 */
export const setChannelSecret = (key: string, value: string) =>
  service.put<unknown>(ep.channelSecret(key), { value });

/**
 * 设置或清除渠道 Secret Key（super_admin）：可选的第二凭证，供 AK/SK 签名类协议用；只写
 * @param key 渠道 key
 * @param value Secret Key 明文；空串表示清除
 */
export const setChannelSecretKey = (key: string, value: string) =>
  service.put<unknown>(ep.channelSecretKey(key), { value });

/**
 * 调整渠道顺序（super_admin）：keys 必须恰好是全部渠道的 key
 * @param keys 全部渠道的新顺序
 */
export const setChannelOrder = (keys: string[]) => service.put<unknown>(ep.channelSort(), { keys });

/**
 * 调整渠道内模型的顺序（admin 也能调）：keys 必须恰好是该渠道下全部模型的 key
 * @param key 渠道 key
 * @param keys 该渠道下全部模型的新顺序
 */
export const setChannelModelOrder = (key: string, keys: string[]) =>
  service.put<unknown>(ep.channelModelSort(key), { keys });

/**
 * 向上游拉取模型清单（admin 也能调）：宿主内置的通用请求，不落库
 * @param key 渠道 key
 * @returns 上游返回的模型，exists 标出渠道下已有的
 */
export const pullChannelModels = async (key: string): Promise<PullModelsResult> => {
  const result = await service.post<PullModelsResult | null>(ep.channelPullModels(key), undefined);
  const models = result?.models ?? [];
  return { models, total: result?.total ?? models.length };
};

/**
 * 把所选的上游模型导入成“待配置”模型（admin 也能调）；渠道下已有的跳过
 * @param key 渠道 key
 * @param ids 要导入的上游模型 id
 * @returns 新建与跳过的模型
 */
export const importChannelModels = async (
  key: string,
  ids: string[],
): Promise<ImportModelsResult> => {
  const result = await service.post<ImportModelsResult | null>(ep.channelImportModels(key), {
    ids,
  });
  return { created: result?.created ?? [], skipped: result?.skipped ?? [] };
};

// ---------------------------------------------------------------- 模型

/**
 * 模型分页列表（后端分页）；每行带状态、原因和渠道名
 * @param params 搜索词、类型、渠道、状态与分页
 * @returns 当前页模型与命中总数
 */
export const listModels = async (
  params: ListModelsParams = {},
): Promise<AdminPage<ModelListItem>> =>
  pageOf(await service.get<AdminPage<ModelListItem> | null>(ep.models(), clean(params)), params);

/**
 * 全部模型的列表项（不分页，不含状态）：概览、渠道导入弹窗判重用
 * @returns 模型列表项
 */
export const listModelOptions = async (): Promise<ConfigListItem[]> =>
  (await service.get<ConfigListItem[] | null>(ep.modelOptions(), undefined)) ?? [];

/**
 * 按上游模型名识别上下文窗口和最大输出：带渠道时先问该渠道的上游，退回内置模型库；都查不到 found 为 false（不是错误）
 * @param name 上游模型名
 * @param channel 模型所在渠道的 key，可选
 * @returns 识别出的规格，只是建议
 */
export const getModelLimits = async (name: string, channel?: string): Promise<ModelLimits> =>
  (await service.get<ModelLimits | null>(ep.modelLimits(), {
    name,
    ...(channel ? { channel } : {}),
  })) ?? { found: false, context_window: 0, max_output: 0 };

/**
 * 模型详情（配置正文 + 启用状态）
 * @param key 模型 key
 * @returns 模型详情
 */
export const getModelDetail = (key: string) => service.get<ConfigDetail>(ep.model(key), undefined);

/**
 * 新建模型：保存配置，默认未启用，启用后用户才能用
 * @param config 配置正文
 * @param note 备注
 * @returns 保存结果
 */
export const createModel = async (config: unknown, note?: string) =>
  normalizeSave(await service.post<SaveModelResult>(ep.models(), saveBody(config, note)));

/**
 * 更新模型：已启用的模型保存即生效（校验不过会被后端拒绝）
 * @param key 模型 key
 * @param config 配置正文
 * @param note 备注
 * @returns 保存结果
 */
export const updateModel = async (key: string, config: unknown, note?: string) =>
  normalizeSave(await service.put<SaveModelResult>(ep.model(key), saveBody(config, note)));

const normalizeSave = (result: SaveModelResult): SaveModelResult => ({
  ...result,
  issues: result?.issues ?? [],
});

/**
 * 校验模型配置；config 不传就校验已保存的配置
 * @param key 模型 key
 * @param config 待校验的配置正文
 * @returns 校验结果
 */
export const validateModel = async (key: string, config?: unknown): Promise<ValidateResult> => {
  const result = await service.post<ValidateResult>(
    ep.modelValidate(key),
    config === undefined ? {} : { body: config },
  );
  return { valid: !!result?.valid, issues: result?.issues ?? [] };
};

/**
 * 模型启用 / 停用：启用前后端会检查配置、渠道与 Key，不通过会报错
 * @param key 模型 key
 * @param enabled 是否启用
 */
export const setModelEnabled = (key: string, enabled: boolean) =>
  service.put<unknown>(ep.modelEnabled(key), { enabled });

/**
 * 模型排序
 * @param key 模型 key
 * @param sort 排序值，越小越靠前
 */
export const setModelSort = (key: string, sort: number) =>
  service.put<unknown>(ep.modelSort(key), { sort });

/**
 * dry-run：渲染插件返回的请求描述（已校验，Key 脱敏），不发送；注意后端目前不含宿主注入后的鉴权头，界面上不要叫“最终请求”
 * @param key 模型 key
 * @param input 示例输入
 * @returns 自由结构的 JSON
 */
export const dryRunModel = (key: string, input: unknown) =>
  service.post<unknown>(ep.modelDryRun(key), { input });

/**
 * 试跑（is_test 任务，不扣积分）
 * @param key 模型 key
 * @param input 示例输入
 * @returns 试跑任务视图
 */
export const testRunModel = (key: string, input: unknown) =>
  service.post<TaskView>(ep.modelTestRun(key), { input });

/**
 * 查询试跑任务
 * @param taskId 任务 ID
 * @returns 任务视图
 */
export const getTestRun = (taskId: number | string) =>
  service.get<TaskView>(ep.testRunResult(taskId), undefined);

/**
 * 试跑追踪：每次钩子的输入 / 输出 / utils.log，每次 HTTP 的请求与响应（均已脱敏）
 * @param taskId 任务 ID
 * @returns 追踪步骤；还没有追踪时是空数组
 */
export const getTestRunTrace = async (taskId: number | string): Promise<TestRunTrace> => {
  const result = await service.get<TestRunTrace | null>(ep.testRunTrace(taskId), undefined);
  return { steps: result?.steps ?? [] };
};

/**
 * 模型配置的 JSON Schema
 * @returns JSON Schema
 */
export const getModelSchema = () => service.get<unknown>(ep.modelSchema(), undefined);

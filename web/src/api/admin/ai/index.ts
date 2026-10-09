import service from "@/utils/requests/service";
import type { TaskView } from "@/api/generation-task/type";
import { adminAiEndpoints as ep, saveBody } from "./endpoints";
import type {
  AdminMe,
  ChannelCheckDraftRequest,
  ChannelCheckResult,
  ChannelCreateRequest,
  ChannelImportResult,
  ChannelUpdateRequest,
  ChannelLoad,
  ChannelView,
  ConfigDetail,
  ConfigListItem,
  DeleteCheckResult,
  DeleteTarget,
  PluginUploadResult,
  PluginView,
  SaveModelResult,
  TestRunTrace,
  ValidateResult,
} from "./type";

// ---------------------------------------------------------------- 当前用户

/**
 * 当前管理端角色；普通用户调用得到 403
 * @returns 用户 ID 与角色
 */
export const getAdminMe = () => service.get<AdminMe>(ep.me(), undefined);

// ---------------------------------------------------------------- 插件

/**
 * 插件与版本列表；versions 缺失时补成空数组
 * @returns 插件列表
 */
export const listPlugins = async (): Promise<PluginView[]> => {
  const list = await service.get<PluginView[] | null>(ep.plugins(), undefined);
  return (list ?? []).map((item) => ({ ...item, versions: item.versions ?? [] }));
};

/**
 * 上传插件文件（super_admin）；预检不通过也返回 200，结果在 accepted / issues 里
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
 * 删除插件版本（super_admin）；被渠道或进行中的任务引用时 409
 * @param key 插件 key
 * @param version semver 版本号
 */
export const deletePluginVersion = (key: string, version: string) =>
  service.delete<unknown>(ep.pluginVersion(key, version));

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
 * 删除插件（整个，含全部版本）/ 渠道（连同 Key）/ 模型（连同全部历史版本，彻底删除）；仍被引用时 409
 * @param target 插件 / 渠道 / 模型
 * @param key 对象 key
 */
export const deleteTarget = (target: DeleteTarget, key: string) =>
  service.delete<unknown>(ep.remove(target, key));

// ---------------------------------------------------------------- 渠道

/**
 * 渠道列表
 * @returns 渠道列表
 */
export const listChannels = async () =>
  (await service.get<ChannelView[] | null>(ep.channels(), undefined)) ?? [];

/**
 * 各渠道当前的任务负载（生成中 / 排队数）
 * @returns 有未完成任务的渠道的负载
 */
export const listChannelLoads = async () =>
  (await service.get<ChannelLoad[] | null>(ep.channelLoads(), undefined)) ?? [];

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
 * 设置渠道 Key（super_admin）；只写，设置后任何接口都读不回明文
 * @param key 渠道 key
 * @param value Key 明文
 */
export const setChannelSecret = (key: string, value: string) =>
  service.put<unknown>(ep.channelSecret(key), { value });

/**
 * 连通性检查（super_admin）
 * @param key 渠道 key
 * @returns 是否连通、说明与耗时
 */
export const checkChannel = (key: string) =>
  service.post<ChannelCheckResult>(ep.channelCheck(key), undefined);

/**
 * 保存前的连通性检查（super_admin）：用表单里还没保存的地址、插件版本、设置与 Key 检查，不落库
 * @param body 渠道草稿；编辑已有渠道时带 existing_key，Key 留空则用已保存的
 * @returns 是否连通、说明与耗时
 */
export const checkChannelDraft = (body: ChannelCheckDraftRequest) =>
  service.post<ChannelCheckResult>(ep.channelCheckDraft(), body);

/**
 * 从渠道导入模型草稿（admin 也能调）；只预填编辑器，不落库
 * @param key 渠道 key
 * @param args 按插件 meta.import.args 填的参数
 * @returns 草稿列表
 */
export const importFromChannel = async (
  key: string,
  args: Record<string, unknown>,
): Promise<ChannelImportResult> => {
  const result = await service.post<ChannelImportResult | null>(ep.channelImport(key), { args });
  return { drafts: result?.drafts ?? [] };
};

// ---------------------------------------------------------------- 模型

/**
 * 模型列表
 * @returns 模型列表
 */
export const listModels = async () =>
  (await service.get<ConfigListItem[] | null>(ep.models(), undefined)) ?? [];

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

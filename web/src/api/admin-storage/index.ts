import service from "@/utils/requests/service";
import { adminStorageEndpoints as ep } from "./endpoints";
import type {
  ProbeResult,
  StorageCreateRequest,
  StorageDeleteCheck,
  StoragePreset,
  StorageSecretRequest,
  StorageTestRequest,
  StorageUpdateRequest,
  StorageView,
} from "./type";

/**
 * 存储列表；后端可能返回 null，统一补成空数组
 * @returns 全部存储（含内置本地磁盘）
 */
export const listStorages = async (): Promise<StorageView[]> =>
  (await service.get<StorageView[] | null>(ep.list(), undefined)) ?? [];

/**
 * 服务商预设：名称、地域列表、直传方式都来自这里；后端可能返回 null，统一补成空数组
 * @returns 预设列表，顺序即页面展示顺序
 */
export const listStoragePresets = async (): Promise<StoragePreset[]> => {
  const list = await service.get<StoragePreset[] | null>(ep.presets(), undefined);
  return (list ?? []).map((item) => ({ ...item, regions: item.regions ?? [] }));
};

/**
 * 存储详情（409 冲突后用它重新拉取最新版本）
 * @param id 存储 ID
 * @returns 存储视图
 */
export const getStorage = (id: number) => service.get<StorageView>(ep.detail(id), undefined);

/**
 * 测试一份未保存的配置（super_admin）：Secret 放在请求体里，不落库
 * @param body 连接与凭证
 * @returns 分步测试结果
 */
export const testStorageDraft = async (body: StorageTestRequest): Promise<ProbeResult> =>
  normalizeProbe(await service.post<ProbeResult | null>(ep.test(), body));

/**
 * 新建存储（super_admin）：保存前后端会自动测试；测试不通过仍会保存，但 check.ok 为 false 且不能设为默认
 * @param body 名称、连接、凭证与访问设置
 * @returns 新建的存储视图
 */
export const createStorage = (body: StorageCreateRequest) =>
  service.post<StorageView>(ep.create(), body);

/**
 * 更新存储（super_admin），整份表单提交，必须带 version；版本冲突返回 409 / 51009，已有素材时改定位字段返回 409 / 51005
 * @param id 存储 ID
 * @param body 整份表单
 * @returns 更新后的存储视图
 */
export const updateStorage = (id: number, body: StorageUpdateRequest) =>
  service.put<StorageView>(ep.update(id), body);

/**
 * 替换凭证（super_admin）：AccessKey ID 与 Secret 必须一起换；后端先用新凭证测试，不通过则什么都不改（400 / 51007）
 * @param id 存储 ID
 * @param body 新的 AccessKey ID 与 Secret，Secret 只写不读
 * @returns 更新后的存储视图
 */
export const replaceStorageSecret = (id: number, body: StorageSecretRequest) =>
  service.put<StorageView>(ep.secret(id), body);

/**
 * 用已存的密钥重新测试一套存储（super_admin），结果会记录到存储的 check 里
 * @param id 存储 ID
 * @returns 分步测试结果
 */
export const checkStorage = async (id: number): Promise<ProbeResult> =>
  normalizeProbe(await service.post<ProbeResult | null>(ep.check(id), undefined));

/**
 * 设为默认存储（super_admin）：只影响之后的新上传和新生成素材；最近测试未通过返回 409 / 51010，内置本地磁盘不受此限制
 * @param id 存储 ID
 */
export const setDefaultStorage = (id: number) => service.put<unknown>(ep.setDefault(), { id });

/**
 * 删除预检（super_admin）：返回能不能删以及不能删的原因
 * @param id 存储 ID
 * @returns 预检结果
 */
export const getStorageDeleteCheck = (id: number) =>
  service.get<StorageDeleteCheck>(ep.deleteCheck(id), undefined);

/**
 * 删除存储（super_admin）：内置、默认、被素材引用的存储返回 409；桶里的文件不会被清理，密钥一并删除
 * @param id 存储 ID
 */
export const deleteStorage = (id: number) => service.delete<unknown>(ep.remove(id));

/** 探针结果的 steps 可能是 null，统一补成数组 */
const normalizeProbe = (result: ProbeResult | null | undefined): ProbeResult => ({
  ok: !!result?.ok,
  steps: result?.steps ?? [],
});

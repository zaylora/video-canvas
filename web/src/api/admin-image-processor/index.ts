import service from "@/utils/requests/service";
import { adminImageProcessorEndpoints as ep } from "./endpoints";
import type {
  ProcessorCreateRequest,
  ProcessorPreset,
  ProcessorPublishRequest,
  ProcessorUpdateRequest,
  ProcessorView,
} from "./type";

/**
 * 厂商预设：名称、可绑定的存储、能力与可选格式都来自这里；后端可能返回 null，统一补成空数组
 * @returns 预设列表，顺序即页面展示顺序
 */
export const listProcessorPresets = async (): Promise<ProcessorPreset[]> => {
  const list = await service.get<ProcessorPreset[] | null>(ep.presets(), undefined);
  return (list ?? []).map((item) => ({ ...item, formats: item.formats ?? [] }));
};

/**
 * 处理服务列表；后端可能返回 null，统一补成空数组
 * @returns 全部处理服务
 */
export const listImageProcessors = async (): Promise<ProcessorView[]> =>
  (await service.get<ProcessorView[] | null>(ep.list(), undefined)) ?? [];

/**
 * 处理服务详情（409 冲突后用它重新拉取最新版本）
 * @param id 处理服务 ID
 * @returns 处理服务视图
 */
export const getImageProcessor = (id: number) =>
  service.get<ProcessorView>(ep.detail(id), undefined);

/**
 * 新建处理服务（super_admin）：只创建草稿，不影响线上
 * @param body 名称、厂商、绑定存储与参数
 * @returns 新建的处理服务视图
 */
export const createImageProcessor = (body: ProcessorCreateRequest) =>
  service.post<ProcessorView>(ep.create(), body);

/**
 * 保存草稿（super_admin）：整份提交，必须带 version；已发布的线上配置不受影响，只会产生未发布草稿
 * @param id 处理服务 ID
 * @param body 版本号、名称与参数
 * @returns 更新后的处理服务视图；版本冲突返回 409 / 52004
 */
export const updateImageProcessor = (id: number, body: ProcessorUpdateRequest) =>
  service.put<ProcessorView>(ep.update(id), body);

/**
 * 校验与试跑（super_admin）：结果写入视图的 check；校验未通过也返回 200，要看 check.ok
 * @param id 处理服务 ID
 * @returns 带最新 check 的处理服务视图
 */
export const checkImageProcessor = (id: number) =>
  service.post<ProcessorView>(ep.check(id), undefined);

/**
 * 发布当前草稿（super_admin）：要求 check.ok 且 check.version 等于 version；同存储已有已发布的会被自动停用
 * @param id 处理服务 ID
 * @param body 要发布的版本号
 * @returns 发布后的处理服务视图
 */
export const publishImageProcessor = (id: number, body: ProcessorPublishRequest) =>
  service.post<ProcessorView>(ep.publish(id), body);

/**
 * 回滚到上一个已发布版本（super_admin）
 * @param id 处理服务 ID
 * @returns 回滚后的处理服务视图
 */
export const rollbackImageProcessor = (id: number) =>
  service.post<ProcessorView>(ep.rollback(id), undefined);

/**
 * 停用已发布的处理服务（super_admin）：该存储回退原图 / 占位
 * @param id 处理服务 ID
 */
export const disableImageProcessor = (id: number) =>
  service.post<unknown>(ep.disable(id), undefined);

/**
 * 删除处理服务（super_admin）：只允许删除草稿或已停用的，已发布的返回 409 / 52008
 * @param id 处理服务 ID
 */
export const deleteImageProcessor = (id: number) => service.delete<unknown>(ep.remove(id));

import service from "@/utils/requests/service";
import type {
  CreateShowcaseItemBody,
  ShowcaseAdminItem,
  ShowcaseAdminView,
  ShowcaseLibraryPage,
  ShowcaseSettings,
  UpdateShowcaseItemBody,
} from "./type";

/**
 * 读取登录页展示的设置和全部作品（admin 可读）
 * @returns 设置与作品列表，作品为 null 时兜底成空数组
 */
export const getShowcaseAdmin = async (): Promise<ShowcaseAdminView> => {
  const view = await service.get<ShowcaseAdminView>("/admin/settings/showcase", undefined);
  return { ...view, items: view.items ?? [] };
};

/**
 * 新增一条作品（super_admin）：视频要先用 uploadAsset 传好
 * @param body 视频素材、封面素材、提示词等
 * @returns 新作品
 */
export const createShowcaseItem = (body: CreateShowcaseItemBody) =>
  service.post<ShowcaseAdminItem>("/admin/settings/showcase/items", body);

/**
 * 修改一条作品（super_admin）
 * @param id 作品 ID
 * @param body 要改的字段
 * @returns 修改后的作品
 */
export const updateShowcaseItem = (id: number, body: UpdateShowcaseItemBody) =>
  service.put<ShowcaseAdminItem>(`/admin/settings/showcase/items/${id}`, body);

/**
 * 删除一条作品（super_admin）：只删展示条目，不删素材
 * @param id 作品 ID
 */
export const deleteShowcaseItem = (id: number) =>
  service.delete<null>(`/admin/settings/showcase/items/${id}`);

/**
 * 保存作品的轮播顺序（super_admin）
 * @param ids 全部作品 ID，按新的顺序，必须不多不少
 */
export const reorderShowcase = (ids: number[]) =>
  service.put<null>("/admin/settings/showcase/order", { ids });

/**
 * 保存轮播设置（super_admin）
 * @param body 播放时长（4 到 15 秒）、登录页开关、省流量开关
 * @returns 保存后的设置
 */
export const updateShowcaseSettings = (body: ShowcaseSettings) =>
  service.put<ShowcaseSettings>("/admin/settings/showcase/settings", body);

/**
 * 读取素材库一页：平台生成的视频，按生成时间倒序，提示词和模型已从生成记录带出（admin 可读）
 * @param page 页码，从 1 开始
 * @param pageSize 每页条数，最多 100
 * @returns 本页视频和总数，视频为 null 时兜底成空数组
 */
export const getShowcaseLibrary = async (
  page: number,
  pageSize: number,
): Promise<ShowcaseLibraryPage> => {
  const result = await service.get<ShowcaseLibraryPage>("/admin/settings/showcase/library", {
    page,
    page_size: pageSize,
  });
  return { ...result, items: result.items ?? [] };
};

import service from "@/utils/requests/service";
import type { AnnouncementAdminItem, AnnouncementAdminView, SaveAnnouncementBody } from "./type";

/**
 * 读取全部公告（含停用），按排序升序（admin 可读）
 * @returns 公告列表，items 为 null 时兜底成空数组
 */
export const getAnnouncementsAdmin = async (): Promise<AnnouncementAdminView> => {
  const view = await service.get<AnnouncementAdminView>("/admin/settings/announcements");
  return { items: view.items ?? [] };
};

/**
 * 新建公告（admin）：排在最后
 * @param body 公告内容
 * @returns 新公告
 */
export const createAnnouncement = (body: SaveAnnouncementBody) =>
  service.post<AnnouncementAdminItem>("/admin/settings/announcements", body);

/**
 * 修改公告（admin）：整体替换，会刷新版本，用户已关闭的会重新看到
 * @param id 公告 ID
 * @param body 全部字段
 * @returns 修改后的公告
 */
export const updateAnnouncement = (id: number, body: SaveAnnouncementBody) =>
  service.put<AnnouncementAdminItem>(`/admin/settings/announcements/${id}`, body);

/**
 * 启用或停用公告（admin）：不刷新版本
 * @param id 公告 ID
 * @param enabled 是否启用
 * @returns 修改后的公告
 */
export const setAnnouncementEnabled = (id: number, enabled: boolean) =>
  service.put<AnnouncementAdminItem>(`/admin/settings/announcements/${id}/enabled`, { enabled });

/**
 * 删除公告（admin）
 * @param id 公告 ID
 */
export const deleteAnnouncement = (id: number) =>
  service.delete<null>(`/admin/settings/announcements/${id}`);

/**
 * 保存公告顺序（admin）
 * @param ids 全部公告 ID，按新的顺序，必须不多不少
 */
export const reorderAnnouncements = (ids: number[]) =>
  service.put<null>("/admin/settings/announcements/order", { ids });

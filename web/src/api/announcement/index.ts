import service from "@/utils/requests/service";
import type { AnnouncementDto, BackendAnnouncementDto, BackendAnnouncementListDto } from "./type";

/**
 * 后端公告（snake_case、RFC 3339 时间）适配成前端 AnnouncementDto
 * @param raw 后端原始公告
 */
export const mapAnnouncement = (raw: BackendAnnouncementDto): AnnouncementDto => {
  const ends = raw.ends_at ? Date.parse(raw.ends_at) : NaN;
  return {
    id: raw.id,
    title: raw.title,
    endsAt: Number.isFinite(ends) ? ends : null,
    version: raw.version,
  };
};

/**
 * 读取首页公告：生效中的，按后台排序，最多 5 条。
 * 公告条没有它页面照常可用，所以失败不弹全局提示，调用方保留上一次的内容。
 * @returns 公告列表
 */
export const getAnnouncements = async (): Promise<AnnouncementDto[]> => {
  const raw = await service.get<BackendAnnouncementListDto>("/announcements", undefined, {
    silent: true,
  });
  return (raw.items ?? []).map(mapAnnouncement);
};

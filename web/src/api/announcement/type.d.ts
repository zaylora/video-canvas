/** 首页公告（前端使用，camelCase） */
export interface AnnouncementDto {
  /** 公告 ID */
  id: number;
  /** 标题，一句话 */
  title: string;
  /** 结束时间（毫秒时间戳），到点前端自己隐藏；null 表示不过期 */
  endsAt: number | null;
  /** 内容版本（updated_at 毫秒），关闭记录按 id + version 匹配，改了内容会重新出现 */
  version: number;
}

/** 后端 GET /announcements 的原始公告（snake_case） */
export interface BackendAnnouncementDto {
  /** 公告 ID */
  id: number;
  /** 标题 */
  title: string;
  /** 结束时间（RFC 3339），null 表示不过期 */
  ends_at: string | null;
  /** 内容版本 */
  version: number;
}

/** 后端 GET /announcements 的原始响应 */
export interface BackendAnnouncementListDto {
  /** 生效中的公告，最多 5 条 */
  items: BackendAnnouncementDto[] | null;
}

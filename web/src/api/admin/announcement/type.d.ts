/** 公告在后台列表里的派生状态 */
export type AnnouncementStatus = "live" | "scheduled" | "expired" | "disabled" | "overflow";

/** 后台看到的一条公告（GET /admin/settings/announcements 的 items） */
export interface AnnouncementAdminItem {
  /** 公告 ID */
  id: number;
  /** 标题 */
  title: string;
  /** 是否启用 */
  enabled: boolean;
  /** 开始时间（RFC 3339），null = 立即 */
  starts_at: string | null;
  /** 结束时间（RFC 3339），null = 不过期 */
  ends_at: string | null;
  /** 排序，越小越前 */
  sort: number;
  /** 派生状态 */
  status: AnnouncementStatus;
  /** 创建时间 */
  created_at: string;
  /** 最后修改时间 */
  updated_at: string;
}

/** 后台公告列表 */
export interface AnnouncementAdminView {
  /** 全部未删除公告，按排序升序 */
  items: AnnouncementAdminItem[];
}

/** 新建 / 编辑公告的请求体（编辑是整体替换） */
export interface SaveAnnouncementBody {
  /** 标题，1–60 字 */
  title: string;
  /** 启用 */
  enabled: boolean;
  /** 开始时间（RFC 3339），null = 立即 */
  starts_at: string | null;
  /** 结束时间（RFC 3339），null = 不过期 */
  ends_at: string | null;
}

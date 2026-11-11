import type {
  AnnouncementAdminItem,
  AnnouncementStatus,
  SaveAnnouncementBody,
} from "@/api/admin/announcement/type";

/** 标题的字数上限（按字符数算，和后端一致） */
export const TITLE_MAX = 60;

/** 编辑表单：时间用 <input type="datetime-local"> 的本地时间字符串，空串表示不限 */
export interface AnnouncementForm {
  title: string;
  enabled: boolean;
  startsAt: string;
  endsAt: string;
}

/** 表单各字段的错误文案 */
export type AnnouncementFormErrors = Partial<Record<keyof AnnouncementForm, string>>;

/** 状态的中文名与标签语气 */
export const STATUS_META: Record<
  AnnouncementStatus,
  { label: string; tone: "success" | "info" | "neutral" | "warning" }
> = {
  live: { label: "展示中", tone: "success" },
  scheduled: { label: "待上线", tone: "info" },
  expired: { label: "已过期", tone: "neutral" },
  disabled: { label: "已停用", tone: "neutral" },
  overflow: { label: "超出上限", tone: "warning" },
};

/** 按字符数（不是 UTF-16 码元）算字数，和后端「≤60 字」一致 */
export const charCount = (text: string) => [...text].length;

const pad = (n: number) => String(n).padStart(2, "0");

/**
 * ISO 时间转成 datetime-local 输入框的值（浏览器本地时区，精确到分钟）
 * @param iso 后端返回的时间，null 表示不限
 */
export function toLocalInput(iso: string | null): string {
  if (!iso) return "";
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return "";
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`;
}

/**
 * datetime-local 输入框的值（浏览器本地时区）转成 ISO；空串或非法值返回 null
 * @param value 输入框的值
 */
export function fromLocalInput(value: string): string | null {
  if (!value) return null;
  const d = new Date(value);
  return Number.isNaN(d.getTime()) ? null : d.toISOString();
}

/** 列表里显示的短时间：月-日 时:分（浏览器本地时区） */
const shortTime = (iso: string) => {
  const d = new Date(iso);
  return `${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}`;
};

/**
 * 列表里的生效时间：都没有是「不限」，缺开始写「立即」，缺结束写「不限」
 * @param startsAt 开始时间
 * @param endsAt 结束时间
 */
export function windowLabel(startsAt: string | null, endsAt: string | null): string {
  if (!startsAt && !endsAt) return "不限";
  return `${startsAt ? shortTime(startsAt) : "立即"} → ${endsAt ? shortTime(endsAt) : "不限"}`;
}

/** 新建表单的默认值：启用、不限时 */
export function emptyAnnouncementForm(): AnnouncementForm {
  return {
    title: "",
    enabled: true,
    startsAt: "",
    endsAt: "",
  };
}

/**
 * 编辑时从后台条目还原表单
 * @param item 后台公告
 */
export function formFromItem(item: AnnouncementAdminItem): AnnouncementForm {
  return {
    title: item.title,
    enabled: item.enabled,
    startsAt: toLocalInput(item.starts_at),
    endsAt: toLocalInput(item.ends_at),
  };
}

/**
 * 表单转成接口请求体：文字去首尾空白，空时间转 null
 * @param form 表单
 */
export function formToBody(form: AnnouncementForm): SaveAnnouncementBody {
  return {
    title: form.title.trim(),
    enabled: form.enabled,
    starts_at: fromLocalInput(form.startsAt),
    ends_at: fromLocalInput(form.endsAt),
  };
}

/**
 * 校验表单，规则和后端一致；保存前拦一道，把错误显示在对应字段下面
 * @param form 表单
 * @returns 各字段的错误，没有错误时是空对象
 */
export function validateAnnouncementForm(form: AnnouncementForm): AnnouncementFormErrors {
  const errors: AnnouncementFormErrors = {};
  const titleLen = charCount(form.title.trim());
  if (titleLen < 1 || titleLen > TITLE_MAX) errors.title = `标题需要 1–${TITLE_MAX} 个字`;
  const starts = fromLocalInput(form.startsAt);
  const ends = fromLocalInput(form.endsAt);
  if (starts && ends && Date.parse(ends) <= Date.parse(starts)) {
    errors.endsAt = "结束时间要晚于开始时间";
  }
  return errors;
}

import type { MeDto, MeStatsDto } from "@/api/me/type";

/** 个人中心的 tab：概览 / 积分 / 资料 / 安全，顺序即界面顺序 */
export const PROFILE_TABS = ["overview", "credits", "profile", "security"] as const;

/** 个人中心的一个 tab */
export type ProfileTab = (typeof PROFILE_TABS)[number];

/** 昵称最多几个字符（按字符计，不按字节） */
export const NICKNAME_MAX = 32;

/** 头像原图上限：10MB；裁剪导出后远小于后端的 2MB 上限 */
export const AVATAR_MAX_BYTES = 10 * 1024 * 1024;

/** 头像允许的原图类型，与后端内容嗅探白名单一致 */
export const AVATAR_TYPES = ["image/png", "image/jpeg", "image/webp", "image/gif"];

/**
 * 是否含控制字符：C0（含换行）、DEL 与 C1，与后端 unicode.IsControl 一致
 * @param value 输入
 */
const hasControlChar = (value: string) =>
  [...value].some((char) => {
    const code = char.codePointAt(0) ?? 0;
    return code <= 0x1f || (code >= 0x7f && code <= 0x9f);
  });

/**
 * 解析地址栏的 tab 参数，非法时回到概览
 * @param value URLSearchParams.get("tab") 的结果
 */
export function parseProfileTab(value: string | null): ProfileTab {
  return PROFILE_TABS.find((tab) => tab === value) ?? "overview";
}

/**
 * 对外显示的名字：昵称为空时回落到用户名
 * @param me 当前用户；未加载时为 null
 */
export function displayName(me: Pick<MeDto, "nickname" | "username"> | null): string {
  if (!me) return "";
  return me.nickname.trim() || me.username;
}

/**
 * 昵称输入的即时状态：去掉首尾空白后 0–32 个字符，不能有控制字符；允许清空
 * @param value 输入框里的原始值
 * @param username 用户名，清空时提示会回落显示它
 * @returns 是否合法与提示文案（合法时是计数或回落说明）
 */
export function nicknameState(value: string, username: string): { ok: boolean; message: string } {
  const trimmed = value.trim();
  const length = [...trimmed].length;
  if (length > NICKNAME_MAX)
    return { ok: false, message: `最多 ${NICKNAME_MAX} 个字，当前 ${length}` };
  if (hasControlChar(value)) return { ok: false, message: "不能包含换行或控制字符" };
  return {
    ok: true,
    message: trimmed ? `${length} / ${NICKNAME_MAX}` : `留空时显示为 ${username}`,
  };
}

/**
 * 昵称是否有变化：首尾空白不算变化
 * @param value 输入框里的原始值
 * @param current 当前保存的昵称
 */
export function nicknameChanged(value: string, current: string): boolean {
  return value.trim() !== current;
}

/**
 * 首字母头像里的字：取第一个字符（按码点，emoji 不被劈开）并大写
 * @param name 显示名
 */
export function avatarInitial(name: string): string {
  const first = [...name.trim()][0];
  return first ? first.toUpperCase() : "?";
}

/**
 * 首字母头像的底色：按 user_id 哈希出固定色相，同一个人在哪都是同一个颜色。
 * 亮度和彩度固定，白字在深浅主题下都看得清
 * @param id 用户 ID
 * @returns CSS 颜色
 */
export function avatarColor(id: string): string {
  let hash = 0;
  for (const char of id) hash = (hash * 31 + (char.codePointAt(0) ?? 0)) >>> 0;
  const hue = (hash * 137) % 360;
  return `oklch(0.6 0.13 ${hue})`;
}

/**
 * 头像原图的前端校验：类型与大小，不符合时就地报错、不打开裁剪框
 * @param file 用户选的文件
 * @returns 错误文案；通过返回 null
 */
export function validateAvatarFile(file: { type: string; size: number }): string | null {
  if (!AVATAR_TYPES.includes(file.type)) {
    return `不支持的格式（${file.type || "未知类型"}），请选择 PNG / JPEG / WebP / GIF`;
  }
  if (file.size > AVATAR_MAX_BYTES) {
    return `文件 ${(file.size / 1024 / 1024).toFixed(1)}MB，超过 10MB`;
  }
  return null;
}

/**
 * 上传头像时的文件名：按实际导出的编码选扩展名（浏览器不支持 WebP 编码时是 PNG）
 * @param mimeType canvas 导出的 Blob 类型
 */
export function avatarExportName(mimeType: string): string {
  return mimeType === "image/webp" ? "avatar.webp" : "avatar.png";
}

/**
 * 成功率：success / (success + failed)，保留一位小数
 * @param stats 统计数字
 * @returns 例如「93.0%」；分母为 0 时返回 null，界面显示「—」
 */
export function successRate(stats: Pick<MeStatsDto, "success" | "failed">): string | null {
  const total = stats.success + stats.failed;
  if (total <= 0) return null;
  return `${((stats.success / total) * 100).toFixed(1)}%`;
}

/**
 * 身份卡上的加入时间：本地日期「2025-03-12 加入」
 * @param createdAt 注册时间（ISO）
 */
export function joinedText(createdAt: string): string {
  const time = Date.parse(createdAt);
  if (!createdAt || Number.isNaN(time)) return "";
  const date = new Date(time);
  const pad = (n: number) => String(n).padStart(2, "0");
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())} 加入`;
}

/**
 * 头像上传失败时裁剪框里的就地提示；裁剪框不关，用户可以直接重试
 * @param code ApiError.code
 * @param message 后端或拦截器给的文案
 */
export function avatarUploadError(code: unknown, message: string): string {
  switch (code) {
    case 55005:
      return message || "头像格式不支持，请上传 PNG / JPEG / WebP / GIF";
    case 55006:
      return "头像文件过大";
    case 10000:
      return "头像服务暂不可用，请稍后重试";
    default:
      return `上传失败：${message || "请重试"}`;
  }
}

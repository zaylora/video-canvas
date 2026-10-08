import type { LoginResponse } from "@/api/auth";
import service from "@/utils/requests/service";
import type {
  ActivityDto,
  BackendLedgerItemDto,
  BackendLedgerPageDto,
  BackendMeDto,
  BackendMeStatsDto,
  ChangePasswordRequest,
  LedgerFilter,
  LedgerItemDto,
  LedgerPageDto,
  LedgerPageSize,
  MeDto,
  MeStatsDto,
} from "./type";

/** 后端用 0 或 null 表示「没有关联」，前端统一成 null，并把数字 id 转成字符串 */
const idOrNull = (value: number | null | undefined) =>
  typeof value === "number" && value > 0 ? String(value) : null;

/**
 * 后端 MeView 适配成前端 MeDto
 * @param raw 后端原始结构
 * @returns 前端使用的资料
 */
export const mapMe = (raw: BackendMeDto): MeDto => ({
  id: String(raw.id),
  username: raw.username,
  nickname: raw.nickname ?? "",
  email: raw.email ?? "",
  role: raw.role,
  avatarUrl: raw.avatar_url ? raw.avatar_url : null,
  createdAt: raw.created_at,
  emailVerifiedAt: raw.email_verified_at ?? null,
});

/**
 * 后端统计适配成前端结构
 * @param raw 后端原始结构
 * @returns 概览页统计
 */
export const mapStats = (raw: BackendMeStatsDto): MeStatsDto => ({
  total: raw.total ?? 0,
  success: raw.success ?? 0,
  failed: raw.failed ?? 0,
  last7d: raw.last7d ?? 0,
  spentCredits: raw.spent_credits ?? 0,
  canvasCount: raw.canvas_count ?? 0,
});

/**
 * 一条后端流水适配成前端结构；不带操作人
 * @param raw 后端原始结构
 * @returns 前端使用的流水
 */
export const mapLedgerItem = (raw: BackendLedgerItemDto): LedgerItemDto => ({
  id: String(raw.id),
  type: raw.type,
  amount: raw.amount,
  taskId: idOrNull(raw.task_id),
  agentCallId: idOrNull(raw.agent_call_id),
  note: raw.note ?? "",
  createdAt: raw.created_at,
});

/**
 * 一页后端流水适配成前端结构；items 为 null 时兜底成空数组
 * @param raw 后端原始结构
 * @returns 前端使用的一页流水
 */
export const mapLedgerPage = (raw: BackendLedgerPageDto): LedgerPageDto => ({
  items: (raw.items ?? []).map(mapLedgerItem),
  total: raw.total ?? 0,
  page: raw.page,
  pageSize: raw.page_size,
});

/**
 * 浏览器当前的 IANA 时区，热力图按它分天；拿不到时回落到 Asia/Shanghai（与后端一致）
 * @returns 时区名，例如 Asia/Shanghai
 */
export const browserTimeZone = () => {
  try {
    return Intl.DateTimeFormat().resolvedOptions().timeZone || "Asia/Shanghai";
  } catch {
    return "Asia/Shanghai";
  }
};

/**
 * 头像上传的请求体：字段名固定为 file
 * @param file 已裁剪导出的头像图片
 * @param fileName 文件名，带扩展名
 * @returns multipart 表单
 */
export const buildAvatarForm = (file: Blob, fileName: string) => {
  const form = new FormData();
  form.append("file", file, fileName);
  return form;
};

/**
 * 获取当前登录用户的资料
 * @returns 当前用户资料
 */
export const getMe = async () => mapMe(await service.get<BackendMeDto>("/me"));

/**
 * 修改昵称；空串表示清空，展示回落到用户名
 * @param data 去掉首尾空白后的昵称，最多 32 个字符
 * @returns 更新后的资料
 */
export const patchMe = async (data: { nickname: string }) =>
  mapMe(await service.patch<BackendMeDto>("/me", data));

/**
 * 上传头像（前端已裁成 512×512）；替换后旧文件由后端尽力删除
 * @param file 头像图片，WebP 或 PNG
 * @param fileName 文件名，带扩展名
 * @returns 更新后的资料
 */
export const uploadAvatar = async (file: Blob, fileName: string) =>
  mapMe(await service.upload<BackendMeDto>("/me/avatar", buildAvatarForm(file, fileName)));

/**
 * 移除头像，恢复首字母头像
 * @returns 更新后的资料
 */
export const deleteAvatar = async () => mapMe(await service.delete<BackendMeDto>("/me/avatar"));

/**
 * 修改密码。成功后其他设备的登录立即失效，响应里是给当前浏览器续签的新令牌，与登录响应同构
 * @param data 当前密码与新密码
 * @returns 新令牌、过期时间与角色
 */
export const changePassword = (data: ChangePasswordRequest) =>
  service.put<LoginResponse>("/me/password", data);

/**
 * 概览页的统计数字
 * @returns 累计生成、成功失败、近 7 天、消耗积分与画布数
 */
export const getMeStats = async () => mapStats(await service.get<BackendMeStatsDto>("/me/stats"));

/**
 * 热力图数据：按浏览器时区分天，只返回有生成的日子
 * @param year 自然年；不传表示最近一年
 * @returns 区间、可选年份与每日计数
 */
export const getMeActivity = async (year?: number | null) => {
  const data = await service.get<ActivityDto>("/me/activity", {
    tz: browserTimeZone(),
    ...(year ? { year } : {}),
  });
  return { ...data, years: data.years ?? [], days: data.days ?? [] };
};

/**
 * 我的积分流水，按时间倒序的页码分页；页码超出范围时返回空 items 和真实 total
 * @param params 筛选类型、页码（从 1 开始）、每页条数
 * @returns 一页流水与总条数
 */
export const getMeLedger = async (params: {
  type: LedgerFilter;
  page: number;
  pageSize: LedgerPageSize;
}) =>
  mapLedgerPage(
    await service.get<BackendLedgerPageDto>("/me/credits/ledger", {
      type: params.type,
      page: params.page,
      page_size: params.pageSize,
    }),
  );

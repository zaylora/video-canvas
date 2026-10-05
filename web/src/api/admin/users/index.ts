import service from "@/utils/requests/service";
import type {
  AdjustCreditsRequest,
  BatchCreditsRequest,
  BatchResult,
  BatchStatusRequest,
  CursorPage,
  LedgerFilter,
  ListUsersParams,
  LoginFilter,
  ResetPasswordRequest,
  ResetPasswordResult,
  SetLimitRequest,
  SetStatusRequest,
  TaskFilter,
  UserCredits,
  UserDetail,
  UserLedgerItem,
  UserLoginItem,
  UserPage,
  UserRole,
  UserTaskItem,
} from "./type";

/** 契约见 docs/design/用户管理/接口契约.md 第 3 节；前缀 /api/v1 由 axios baseURL 提供 */
const P = "/admin/users";

const withId = (id: number) => `${P}/${id}`;

/** 去掉空值，免得把 `status=` 之类的空参数发给后端 */
const clean = (params: Record<string, unknown>) =>
  Object.fromEntries(Object.entries(params).filter(([, v]) => v !== undefined && v !== ""));

/** 游标分页的返回值兜底：后端对空列表可能返回 null */
const cursorPage = <T>(page: CursorPage<T> | null | undefined): CursorPage<T> => ({
  items: page?.items ?? [],
  next_cursor: page?.next_cursor || null,
});

/**
 * 用户列表（后端分页，按 id 倒序）
 * @param params 搜索词、状态、角色与分页
 * @returns 当前页用户与满足条件的总数；list 缺失时补成空数组
 */
export const listUsers = async (params: ListUsersParams = {}): Promise<UserPage> => {
  const page = await service.get<UserPage | null>(P, clean({ ...params }));
  return {
    list: page?.list ?? [],
    total: page?.total ?? 0,
    page: page?.page ?? params.page ?? 1,
    page_size: page?.page_size ?? params.page_size ?? 20,
  };
};

/**
 * 用户详情，含统计与最近 3 条管理员操作；用户不存在时后端返回 ErrUserNotFound
 * @param id 用户 ID
 * @returns 用户详情，recent_audits 缺失时补成空数组
 */
export const getUser = async (id: number): Promise<UserDetail> => {
  const detail = await service.get<UserDetail>(withId(id), undefined);
  return { ...detail, recent_audits: detail.recent_audits ?? [] };
};

/**
 * 生成记录（读 generation_tasks），游标分页
 * @param id 用户 ID
 * @param query status 状态筛选，cursor 上一页的 next_cursor，limit 每页条数
 * @returns 一页生成记录
 */
export const listUserTasks = async (
  id: number,
  query: { status?: TaskFilter; cursor?: string | null; limit?: number } = {},
): Promise<CursorPage<UserTaskItem>> =>
  cursorPage(
    await service.get<CursorPage<UserTaskItem> | null>(
      `${withId(id)}/tasks`,
      clean({ ...query, cursor: query.cursor ?? undefined }),
    ),
  );

/**
 * 积分流水，游标分页
 * @param id 用户 ID
 * @param query type 类型筛选（all / admin / task），cursor 上一页的 next_cursor，limit 每页条数
 * @returns 一页积分流水
 */
export const listUserLedger = async (
  id: number,
  query: { type?: LedgerFilter; cursor?: string | null; limit?: number } = {},
): Promise<CursorPage<UserLedgerItem>> =>
  cursorPage(
    await service.get<CursorPage<UserLedgerItem> | null>(
      `${withId(id)}/ledger`,
      clean({ ...query, cursor: query.cursor ?? undefined }),
    ),
  );

/**
 * 登录与注册记录，游标分页
 * @param id 用户 ID
 * @param query result 结果筛选（all / fail），cursor 上一页的 next_cursor，limit 每页条数
 * @returns 一页登录记录
 */
export const listUserLogins = async (
  id: number,
  query: { result?: LoginFilter; cursor?: string | null; limit?: number } = {},
): Promise<CursorPage<UserLoginItem>> =>
  cursorPage(
    await service.get<CursorPage<UserLoginItem> | null>(
      `${withId(id)}/logins`,
      clean({ ...query, cursor: query.cursor ?? undefined }),
    ),
  );

/**
 * 调整积分：增加 / 扣减 / 把可用积分设为指定值，写流水与审计
 * @param id 用户 ID
 * @param body 方式、数值与备注（备注必填）
 * @returns 调整后的积分账户
 */
export const adjustUserCredits = (id: number, body: AdjustCreditsRequest) =>
  service.post<UserCredits>(`${withId(id)}/credits`, body);

/**
 * 设置单用户并发上限
 * @param id 用户 ID
 * @param body max_active_tasks 为 1 到 64，null 表示改回全局默认
 */
export const setUserLimit = (id: number, body: SetLimitRequest) =>
  service.put<unknown>(`${withId(id)}/limits`, body);

/**
 * 封禁或启用账号；封禁会断开该用户的 WebSocket
 * @param id 用户 ID
 * @param body 目标状态；cancel_active 为 true 时同时取消进行中的任务并退还冻结（不可撤销）
 */
export const setUserStatus = (id: number, body: SetStatusRequest) =>
  service.put<unknown>(`${withId(id)}/status`, body);

/**
 * 修改角色（仅 super_admin）
 * @param id 用户 ID
 * @param role 目标角色
 */
export const setUserRole = (id: number, role: UserRole) =>
  service.put<unknown>(`${withId(id)}/role`, { role });

/**
 * 重置密码（仅 super_admin）；该用户已登录的设备会立即失效
 * @param id 用户 ID
 * @param body 缺省 new_password 时由后端生成临时密码
 * @returns 临时密码，只返回这一次
 */
export const resetUserPassword = (id: number, body: ResetPasswordRequest = {}) =>
  service.post<ResetPasswordResult>(`${withId(id)}/reset-password`, body);

/**
 * 批量发积分（只增不减），每个用户独立判断权限，失败的写在 results 里
 * @param body 用户 ID 列表、每人增加的积分与备注
 * @returns 逐项结果
 */
export const batchAddCredits = async (body: BatchCreditsRequest): Promise<BatchResult> => ({
  results: (await service.post<BatchResult | null>(`${P}/batch/credits`, body))?.results ?? [],
});

/**
 * 批量封禁 / 启用（不支持取消任务），每个用户独立判断权限
 * @param body 用户 ID 列表与目标状态
 * @returns 逐项结果
 */
export const batchSetStatus = async (body: BatchStatusRequest): Promise<BatchResult> => ({
  results: (await service.put<BatchResult | null>(`${P}/batch/status`, body))?.results ?? [],
});

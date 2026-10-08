/** 账号角色 */
export type MeRole = "user" | "admin" | "super_admin";

/** 后端 GET /me 等接口返回的 MeView（snake_case、数字 id） */
export interface BackendMeDto {
  /** 用户 ID */
  id: number;
  /** 登录用户名，不可修改 */
  username: string;
  /** 昵称；空串表示没设置，展示时回落到 username */
  nickname: string;
  /** 邮箱；老账号可能为空串 */
  email: string;
  /** 角色 */
  role: MeRole;
  /** 头像地址（/files/<key>）；空串表示没有头像 */
  avatar_url: string;
  /** 注册时间（ISO） */
  created_at: string;
  /** 邮箱验证时间（ISO）；null 表示没验证过 */
  email_verified_at: string | null;
}

/** 当前登录用户的资料 */
export interface MeDto {
  /** 用户 ID */
  id: string;
  /** 登录用户名，不可修改 */
  username: string;
  /** 昵称；空串表示没设置 */
  nickname: string;
  /** 邮箱；可能为空串 */
  email: string;
  /** 角色 */
  role: MeRole;
  /** 头像地址；null 表示没有头像，用首字母兜底 */
  avatarUrl: string | null;
  /** 注册时间（ISO） */
  createdAt: string;
  /** 邮箱验证时间（ISO）；null 表示没验证过 */
  emailVerifiedAt: string | null;
}

/** 后端 GET /me/stats 的原始结构 */
export interface BackendMeStatsDto {
  /** 累计生成任务数（不含试跑，含进行中与已取消） */
  total: number;
  /** 成功的任务数 */
  success: number;
  /** 失败的任务数 */
  failed: number;
  /** 近 7 天提交的任务数 */
  last7d: number;
  /** 累计结算消耗的积分 */
  spent_credits: number;
  /** 未删除的画布数 */
  canvas_count: number;
}

/** 概览页的统计数字 */
export interface MeStatsDto {
  /** 累计生成任务数（不含试跑，含进行中与已取消） */
  total: number;
  /** 成功的任务数 */
  success: number;
  /** 失败的任务数 */
  failed: number;
  /** 近 7 天提交的任务数 */
  last7d: number;
  /** 累计结算消耗的积分 */
  spentCredits: number;
  /** 未删除的画布数 */
  canvasCount: number;
}

/** 热力图上一天的生成次数，按任务类型拆分 */
export interface ActivityDay {
  /** 用户时区下的日期，YYYY-MM-DD */
  date: string;
  /** 当天提交的任务总数 */
  count: number;
  /** 图片任务数 */
  image: number;
  /** 视频任务数 */
  video: number;
  /** 音频任务数 */
  audio: number;
  /** 文本任务数 */
  text: number;
}

/** GET /me/activity 的响应：字段已是前端可直接用的形状，不做映射 */
export interface ActivityDto {
  /** 实际使用的 IANA 时区；传入非法时区时后端回落到 Asia/Shanghai */
  tz: string;
  /** 区间起点（含），YYYY-MM-DD */
  start: string;
  /** 区间终点（含），YYYY-MM-DD；最近一年时是用户时区的今天 */
  end: string;
  /** 可切换的自然年：注册年份到今年 */
  years: number[];
  /** 区间内的任务总数 */
  total: number;
  /** 只含 count > 0 的日子，其余日期由前端补齐 */
  days: ActivityDay[];
}

/** 流水筛选：全部 / 生成任务 / 管理员调整 */
export type LedgerFilter = "all" | "task" | "admin";

/** 流水类型：initial 赠送、freeze 冻结、settle 结算、refund 退款、admin_adjust 管理员调整 */
export type LedgerType = "initial" | "freeze" | "settle" | "refund" | "admin_adjust";

/** 每页条数：后端只接受这三档 */
export type LedgerPageSize = 10 | 20 | 50;

/** 后端返回的一条流水（不含操作人） */
export interface BackendLedgerItemDto {
  /** 流水 ID */
  id: number;
  /** 流水类型 */
  type: LedgerType;
  /** 积分数量：settle / freeze / refund 为正数，admin_adjust 带正负号 */
  amount: number;
  /** 关联任务 ID；没有时为 null 或 0 */
  task_id: number | null;
  /** 关联的 Agent 对话调用 ID；没有时为 null 或 0 */
  agent_call_id: number | null;
  /** 备注：只有管理员调整非空 */
  note: string;
  /** 创建时间（ISO） */
  created_at: string;
}

/** 后端流水分页响应 */
export interface BackendLedgerPageDto {
  /** 当前页的流水；页码超出范围时为空 */
  items: BackendLedgerItemDto[] | null;
  /** 当前筛选下的总条数 */
  total: number;
  /** 当前页码，从 1 开始 */
  page: number;
  /** 每页条数 */
  page_size: number;
}

/** 一条积分流水 */
export interface LedgerItemDto {
  /** 流水 ID */
  id: string;
  /** 流水类型 */
  type: LedgerType;
  /** 积分数量：settle / freeze / refund 为正数，admin_adjust 带正负号 */
  amount: number;
  /** 关联任务 ID；null 表示没有 */
  taskId: string | null;
  /** 关联的 Agent 对话调用 ID；null 表示没有 */
  agentCallId: string | null;
  /** 备注：只有管理员调整非空，按纯文本展示 */
  note: string;
  /** 创建时间（ISO） */
  createdAt: string;
}

/** 一页积分流水 */
export interface LedgerPageDto {
  /** 当前页的流水 */
  items: LedgerItemDto[];
  /** 当前筛选下的总条数 */
  total: number;
  /** 当前页码，从 1 开始 */
  page: number;
  /** 每页条数 */
  pageSize: number;
}

/** 修改密码的请求体 */
export interface ChangePasswordRequest {
  /** 当前密码 */
  old_password: string;
  /** 新密码 */
  new_password: string;
}

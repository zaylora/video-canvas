/** 用户角色：user 普通用户，admin 运营，super_admin 运维 */
export type UserRole = "user" | "admin" | "super_admin";

/** 账号状态：disabled 的账号不能登录，也不能提交任务 */
export type UserStatus = "active" | "disabled";

/** 用户列表项（GET /admin/users 的每一行） */
export interface UserListItem {
  /** 用户 ID */
  id: number;
  /** 用户名 */
  username: string;
  /** 邮箱；老账号可能为空串 */
  email: string;
  /** 角色 */
  role: UserRole;
  /** 账号状态 */
  status: UserStatus;
  /** 积分余额（含冻结部分） */
  balance: number;
  /** 被进行中任务冻结的积分 */
  frozen: number;
  /** 可用积分 = balance - frozen；界面一律以它为准 */
  available: number;
  /** 老用户可能还没有积分账户：此时 balance 为 0 */
  has_credit_account: boolean;
  /** 单用户并发上限覆盖；null 表示使用全局默认 */
  max_active_tasks: number | null;
  /** 实际生效的并发上限 */
  effective_max_active_tasks: number;
  /** 进行中的任务数 */
  active_tasks: number;
  /** 最近登录时间（ISO）；null 表示从未登录 */
  last_login_at: string | null;
  /** 注册时间（ISO） */
  created_at: string;
}

/** 管理员对账号的最近操作（来自审计日志，详情里最多 3 条） */
export interface UserAuditBrief {
  /** 操作人 ID */
  actor_id: number;
  /** 操作人用户名 */
  actor_name: string;
  /** 动作，如 user.credit_adjust、user.ban */
  action: string;
  /** 动作详情；后端按动作写入 JSON，可能是字符串或对象 */
  detail_json: string | Record<string, unknown> | null;
  /** 操作时间（ISO） */
  created_at: string;
}

/** 用户详情（GET /admin/users/:id） */
export interface UserDetail extends UserListItem {
  /** 邮箱验证时间（ISO）；null 表示未验证 */
  email_verified_at: string | null;
  /** 生成任务总数 */
  task_total: number;
  /** 成功的任务数 */
  task_success: number;
  /** 失败的任务数 */
  task_failed: number;
  /** 累计消耗的积分 */
  spent_credits: number;
  /** 近 7 天生成的任务数 */
  tasks_last_7d: number;
  /** 最近的管理员操作，最多 3 条 */
  recent_audits: UserAuditBrief[];
}

/** 用户列表查询参数 */
export interface ListUsersParams {
  /** 用户名 / 邮箱模糊匹配 */
  q?: string;
  /** 状态筛选；不传为全部 */
  status?: UserStatus;
  /** 角色筛选；不传为全部 */
  role?: UserRole;
  /** 页码，从 1 开始 */
  page?: number;
  /** 每页条数，最大 100 */
  page_size?: number;
}

/** 分页列表（后端 response.OKPage 的 data） */
export interface UserPage {
  /** 当前页的用户 */
  list: UserListItem[];
  /** 满足筛选条件的总数 */
  total: number;
  /** 当前页码 */
  page: number;
  /** 每页条数 */
  page_size: number;
}

/** 游标分页：next_cursor 为空表示没有更多 */
export interface CursorPage<T> {
  /** 本页记录 */
  items: T[];
  /** 下一页游标；null 或空串表示已到末尾 */
  next_cursor: string | null;
}

/** 生成记录的状态筛选 */
export type TaskFilter = "all" | "success" | "failed" | "running";

/** 一条生成记录 */
export interface UserTaskItem {
  /** 任务 ID */
  id: number;
  /** 类型：video / image / audio / text */
  kind: string;
  /** 模型 key */
  model_key: string;
  /** 模型显示名 */
  model_name: string;
  /** 任务状态：pending / queued / running / finalizing / succeeded / failed / canceled / expired */
  status: string;
  /** 预估积分（提交时冻结的额度） */
  credits: number;
  /** 实际扣费积分；未结算为 0 */
  charged_credits: number;
  /** 失败原因；成功时为空串 */
  error: string;
  /** 耗时（毫秒）；未完成为 null */
  duration_ms: number | null;
  /** 创建时间（ISO） */
  created_at: string;
}

/** 积分流水的类型筛选 */
export type LedgerFilter = "all" | "admin" | "task";

/** 流水类型：initial 赠送、freeze 冻结、settle 结算、refund 退款、admin_adjust 管理员调整 */
export type LedgerType = "initial" | "freeze" | "settle" | "refund" | "admin_adjust";

/** 一条积分流水 */
export interface UserLedgerItem {
  /** 流水 ID */
  id: number;
  /** 流水类型 */
  type: LedgerType;
  /** 变动额，带正负号 */
  amount: number;
  /** 关联任务 ID；管理员调整和赠送为 null */
  task_id: number | null;
  /** 备注；管理员调整必填 */
  note: string;
  /** 操作人 ID；系统流水为 null */
  operator_id: number | null;
  /** 操作人用户名；系统流水为空串 */
  operator_name: string;
  /** 创建时间（ISO） */
  created_at: string;
}

/** 登录记录的结果筛选 */
export type LoginFilter = "all" | "fail";

/** 一条登录 / 注册记录 */
export interface UserLoginItem {
  /** 记录 ID */
  id: number;
  /** login 登录，register 注册 */
  kind: "login" | "register";
  /** ok 成功，badpw 密码错误，blocked 账号已停用 */
  result: "ok" | "badpw" | "blocked";
  /** 来源 IP */
  ip: string;
  /** 原始 User-Agent */
  user_agent: string;
  /** 发生时间（ISO） */
  created_at: string;
}

/** 积分调整方式：add 增加，sub 扣减，set 把可用积分设为指定值 */
export type CreditMode = "add" | "sub" | "set";

/** POST /admin/users/:id/credits 请求体 */
export interface AdjustCreditsRequest {
  /** 调整方式 */
  mode: CreditMode;
  /** 数值（整数，>= 0；add / sub 必须 > 0） */
  amount: number;
  /** 备注，1 到 100 字，写入流水和审计 */
  note: string;
}

/** 积分账户快照 */
export interface UserCredits {
  /** 余额（含冻结） */
  balance: number;
  /** 冻结额 */
  frozen: number;
  /** 可用积分 */
  available: number;
}

/** PUT /admin/users/:id/limits 请求体 */
export interface SetLimitRequest {
  /** 并发上限 1 到 64；null 表示改回全局默认 */
  max_active_tasks: number | null;
}

/** PUT /admin/users/:id/status 请求体 */
export interface SetStatusRequest {
  /** 目标状态 */
  status: UserStatus;
  /** 封禁时同时取消进行中的任务并退还冻结积分；仅 status=disabled 有效，不可撤销 */
  cancel_active?: boolean;
}

/** POST /admin/users/:id/reset-password 请求体 */
export interface ResetPasswordRequest {
  /** 指定新密码；缺省由后端生成临时密码 */
  new_password?: string;
}

/** 重置密码的结果 */
export interface ResetPasswordResult {
  /** 临时密码，只返回这一次 */
  temp_password: string;
}

/** 批量接口里每个用户的结果 */
export interface BatchResultItem {
  /** 用户 ID */
  id: number;
  /** 是否成功 */
  ok: boolean;
  /** 失败或跳过的原因 */
  error?: string;
}

/** 批量接口的响应 */
export interface BatchResult {
  /** 逐项结果，顺序与请求的 ids 一致 */
  results: BatchResultItem[];
}

/** POST /admin/users/batch/credits 请求体（只增不减） */
export interface BatchCreditsRequest {
  /** 用户 ID 列表 */
  ids: number[];
  /** 每人增加的积分，> 0 */
  amount: number;
  /** 备注，1 到 100 字 */
  note: string;
}

/** PUT /admin/users/batch/status 请求体（不支持取消任务） */
export interface BatchStatusRequest {
  /** 用户 ID 列表 */
  ids: number[];
  /** 目标状态 */
  status: UserStatus;
}

import type { AdminRole } from "@/api/admin-ai/type";
import type {
  CreditMode,
  LedgerType,
  UserListItem,
  UserRole,
  UserStatus,
} from "@/api/admin-users/type";

/**
 * 用户管理的纯逻辑：权限禁用原因、积分调整校验与预览、批量跳过规则、时间与 UA 文案。
 * 规则的出处是 docs/design/用户管理/接口契约.md 第 3、4 节；后端仍会在 service 层再判断一次，
 * 这里只是让界面提前禁用并说明原因。
 */

/** 角色显示名 */
export const USER_ROLE_LABEL: Record<UserRole, string> = {
  user: "普通用户",
  admin: "管理员",
  super_admin: "超级管理员",
};

/** 角色 Tag 的语气：普通默认、管理员 info、超级管理员 warning */
export const USER_ROLE_TONE: Record<UserRole, "neutral" | "info" | "warning"> = {
  user: "neutral",
  admin: "info",
  super_admin: "warning",
};

/** 账号状态显示名 */
export const USER_STATUS_LABEL: Record<UserStatus, string> = {
  active: "正常",
  disabled: "已停用",
};

/** 积分流水类型显示名 */
export const LEDGER_TYPE_LABEL: Record<LedgerType, string> = {
  initial: "赠送",
  freeze: "冻结",
  settle: "结算",
  refund: "退款",
  admin_adjust: "管理员调整",
};

/** 管理员动作显示名（审计日志的 action） */
export const AUDIT_ACTION_LABEL: Record<string, string> = {
  "user.credit_adjust": "调整了积分",
  "user.ban": "封禁了账号",
  "user.unban": "启用了账号",
  "user.role": "修改了角色",
  "user.reset_password": "重置了密码",
  "user.limit": "调整了并发上限",
};

/** 当前操作者：只需要 id 与管理端角色 */
export type Actor = { id: number; role: AdminRole };

/** 被操作的账号：只需要 id 与角色 */
export type Target = { id: number; role: UserRole };

/** 需要判断权限的动作；cancel 是「封禁并取消任务」，规则与封禁相同 */
export type UserAction = "ban" | "cancel" | "role" | "reset" | "credit" | "limit";

/**
 * 某个动作被禁用的原因，null 表示可以操作。判断顺序：先看是不是仅 super_admin，
 * 再看是不是对自己，最后看 admin 能不能碰管理员账号。
 * @param actor 当前管理员；null 表示身份还没加载
 * @param target 被操作的账号
 * @param action 动作
 * @returns 给 ReasonTooltip 用的中文原因，或 null
 */
export function denyReason(actor: Actor | null, target: Target, action: UserAction): string | null {
  if (!actor) return "正在确认管理权限";
  const self = actor.id === target.id;
  const superAdmin = actor.role === "super_admin";
  if ((action === "role" || action === "reset") && !superAdmin) return "仅超级管理员可用";
  if (self && (action === "ban" || action === "cancel" || action === "role")) {
    return "不能对自己执行此操作";
  }
  if (self && action === "credit" && !superAdmin) return "admin 不能给自己调整积分";
  if (!self && !superAdmin && target.role !== "user") return "admin 不能操作管理员账号";
  return null;
}

/** 后端备注上限（字） */
export const NOTE_MAX = 100;

/** 数值输入最多 9 位：远小于安全整数，也挡住手滑多按的 0 */
const AMOUNT_MAX_DIGITS = 9;

/**
 * 把输入框文本解析成非负整数
 * @param text 输入框内容
 * @returns 整数；空、含非数字字符或过长时为 null
 */
export function parseAmount(text: string): number | null {
  const value = text.trim();
  if (!/^\d+$/.test(value) || value.length > AMOUNT_MAX_DIGITS) return null;
  return Number(value);
}

/** 积分预览的结果 */
export type CreditPreview = {
  /** 调整后的可用积分；没填数值时为 null */
  after: number | null;
  /** 不能提交的原因（只有扣减超额会给出文字） */
  error: string | null;
  /** 数值本身是否可以提交（备注另算） */
  valid: boolean;
};

/**
 * 积分调整的预览与校验，三种方式都以可用积分为准：
 * 增加 / 扣减在可用积分上加减，「设为」把可用积分设为指定值（后端换算为 余额 = 指定值 + 冻结额）。
 * @param input 方式、数值（null 表示没填）、当前可用积分
 */
export function creditPreview(input: {
  mode: CreditMode;
  amount: number | null;
  available: number;
}): CreditPreview {
  const { mode, amount, available } = input;
  if (amount === null) return { after: null, error: null, valid: false };
  if (mode === "set") return { after: amount, error: null, valid: true };
  if (mode === "add") return { after: available + amount, error: null, valid: amount > 0 };
  if (amount > available) {
    return { after: available - amount, error: `最多可扣 ${available}`, valid: false };
  }
  return { after: available - amount, error: null, valid: amount > 0 };
}

/**
 * 可用积分的变化量（带正负号）
 * @param mode 调整方式
 * @param amount 数值
 * @param available 调整前的可用积分
 */
export function creditDelta(mode: CreditMode, amount: number, available: number): number {
  if (mode === "add") return amount;
  if (mode === "sub") return -amount;
  return amount - available;
}

/**
 * 积分调整的「撤销」请求：反向的 add / sub，备注写「撤销：原备注」，不删除原流水
 * @param delta 原调整造成的可用积分变化量
 * @param note 原备注
 * @returns 请求体；变化量为 0 时没有可撤销的，返回 null
 */
export function creditUndoRequest(
  delta: number,
  note: string,
): { mode: "add" | "sub"; amount: number; note: string } | null {
  if (delta === 0) return null;
  return {
    mode: delta > 0 ? "sub" : "add",
    amount: Math.abs(delta),
    note: `撤销：${note}`.slice(0, NOTE_MAX),
  };
}

/**
 * 备注校验：必填，1 到 100 字
 * @returns 错误文案；合法为 null
 */
export function validateNote(note: string): string | null {
  const value = note.trim();
  if (!value) return "请填写备注";
  if (value.length > NOTE_MAX) return `备注最多 ${NOTE_MAX} 字`;
  return null;
}

/**
 * 点快捷档：当前没有数值就直接填入，有数值则累加（+100 再点 +100 = 200）
 * @param current 输入框当前文本
 * @param chip 快捷档的数值
 * @returns 新的输入框文本
 */
export function stackChip(current: string, chip: number): string {
  return String((parseAmount(current) ?? 0) + chip);
}

/** 并发上限的范围，与后端一致 */
export const LIMIT_RANGE = { min: 1, max: 64 } as const;

/**
 * 并发上限校验
 * @param text 输入框文本
 * @param useDefault 是否选了「使用全局默认」
 * @returns value 为要提交的 max_active_tasks（默认为 null），valid 表示能否保存
 */
export function validateLimit(
  text: string,
  useDefault: boolean,
): { value: number | null; valid: boolean } {
  if (useDefault) return { value: null, valid: true };
  const value = parseAmount(text);
  const valid = value !== null && value >= LIMIT_RANGE.min && value <= LIMIT_RANGE.max;
  return { value: valid ? value : null, valid };
}

/** 并发列的展示数据 */
export type ConcurrencyView = {
  /** 进行中的任务数 */
  active: number;
  /** 实际生效的上限 */
  limit: number;
  /** 是否用满（进度条与数字变警告色） */
  full: boolean;
  /** 是否是单用户自定义上限 */
  custom: boolean;
  /** 进度 0 到 1 */
  ratio: number;
};

/** 并发列与抽屉摘要的展示数据 */
export function concurrencyView(
  user: Pick<UserListItem, "active_tasks" | "effective_max_active_tasks" | "max_active_tasks">,
): ConcurrencyView {
  const limit = user.effective_max_active_tasks;
  const active = user.active_tasks;
  return {
    active,
    limit,
    full: limit > 0 && active >= limit,
    custom: user.max_active_tasks !== null,
    ratio: limit > 0 ? Math.min(1, active / limit) : 0,
  };
}

/** 批量操作的种类 */
export type BatchKind = "ban" | "unban" | "credit";

/** 批量计划里用到的用户字段 */
export type BatchUser = Pick<UserListItem, "id" | "role" | "status" | "username">;

/** 批量计划：哪些执行、哪些因权限跳过、哪些本来就是目标状态 */
export type BatchPlan<T extends BatchUser> = {
  /** 会提交给后端的用户 */
  run: T[];
  /** 无权限而跳过的用户与原因 */
  skipped: { user: T; reason: string }[];
  /** 已经是目标状态，不用处理，也不算跳过 */
  noop: T[];
  /** 给 toast 用的「已跳过 N 个：原因」，没有跳过为空串 */
  skipText: string;
};

/**
 * 批量操作的跳过规则：选中里当前身份无权操作的账号（admin 选中了管理员、选中了自己）自动跳过。
 * 批量里对自己的封禁 / 启用说「不能封禁自己」，其余沿用 denyReason。
 * @param actor 当前管理员
 * @param users 勾选的用户
 * @param kind 批量封禁 / 启用 / 发积分
 */
export function batchPlan<T extends BatchUser>(
  actor: Actor | null,
  users: T[],
  kind: BatchKind,
): BatchPlan<T> {
  const run: T[] = [];
  const noop: T[] = [];
  const skipped: { user: T; reason: string }[] = [];
  for (const user of users) {
    let reason = denyReason(actor, user, kind === "credit" ? "credit" : "ban");
    if (reason && actor && user.id === actor.id && kind !== "credit") reason = "不能封禁自己";
    if (reason) {
      skipped.push({ user, reason });
      continue;
    }
    const already =
      (kind === "ban" && user.status === "disabled") ||
      (kind === "unban" && user.status === "active");
    (already ? noop : run).push(user);
  }
  const reasons = [...new Set(skipped.map((item) => item.reason))];
  const skipText = skipped.length ? `已跳过 ${skipped.length} 个：${reasons.join("；")}` : "";
  return { run, skipped, noop, skipText };
}

const pad = (n: number) => String(n).padStart(2, "0");

const clock = (date: Date) => `${pad(date.getHours())}:${pad(date.getMinutes())}`;

const startOfDay = (date: Date) =>
  new Date(date.getFullYear(), date.getMonth(), date.getDate()).getTime();

const DAY = 86_400_000;

/**
 * 相对时间：同一天「今天 10:21」、前一天「昨天 22:40」，再早是「3 天前」「1 周前」「2 月前」「2 年前」。
 * 按自然日算而不是 24 小时，昨天 23:59 在凌晨 00:05 看也是「昨天」。
 * @param value ISO 时间；null 表示从未登录
 * @param now 当前时间（测试注入）
 */
export function formatRelative(value: string | null | undefined, now: Date = new Date()): string {
  if (!value) return "从未登录";
  const time = Date.parse(value);
  if (Number.isNaN(time)) return value;
  const date = new Date(time);
  const days = Math.round((startOfDay(now) - startOfDay(date)) / DAY);
  if (days <= 0) return `今天 ${clock(date)}`;
  if (days === 1) return `昨天 ${clock(date)}`;
  if (days < 7) return `${days} 天前`;
  if (days < 30) return `${Math.floor(days / 7)} 周前`;
  if (days < 365) return `${Math.floor(days / 30)} 月前`;
  return `${Math.floor(days / 365)} 年前`;
}

/**
 * 绝对时间「2026-10-05 09:03」，给 hover 提示和详情用
 * @param value ISO 时间；空返回「—」
 */
export function formatAbsolute(value: string | null | undefined): string {
  if (!value) return "—";
  const time = Date.parse(value);
  if (Number.isNaN(time)) return value;
  const d = new Date(time);
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${clock(d)}`;
}

/**
 * 「9月20日」；不是今年的带年份「2025年9月20日」
 * @param value ISO 时间
 * @param now 当前时间（测试注入）
 */
export function formatMonthDay(value: string | null | undefined, now: Date = new Date()): string {
  if (!value) return "—";
  const time = Date.parse(value);
  if (Number.isNaN(time)) return value;
  const d = new Date(time);
  const md = `${d.getMonth() + 1}月${d.getDate()}日`;
  return d.getFullYear() === now.getFullYear() ? md : `${d.getFullYear()}年${md}`;
}

/**
 * 把 User-Agent 解析成「Chrome · macOS」。只认常见浏览器与系统，认不出的统一写「未知设备」，
 * 不引入 UA 解析库：这里只是给运营一个大致印象。
 * @param ua 原始 User-Agent
 */
export function parseUserAgent(ua: string): string {
  if (!ua) return "未知设备";
  const browser = /Edg(e|A|iOS)?\//.test(ua)
    ? "Edge"
    : /OPR\/|Opera/.test(ua)
      ? "Opera"
      : /Firefox\/|FxiOS\//.test(ua)
        ? "Firefox"
        : /Chrome\/|CriOS\//.test(ua)
          ? "Chrome"
          : /Safari\//.test(ua)
            ? "Safari"
            : null;
  const os = /iPhone|iPad|iPod/.test(ua)
    ? "iOS"
    : /Android/.test(ua)
      ? "Android"
      : /Windows/.test(ua)
        ? "Windows"
        : /Mac OS X|Macintosh/.test(ua)
          ? "macOS"
          : /Linux|X11/.test(ua)
            ? "Linux"
            : null;
  if (!browser && !os) return "未知设备";
  return [browser, os].filter(Boolean).join(" · ");
}

/** 登录记录结果的展示：文案与 Tag 语气 */
export function loginResultView(
  kind: "login" | "register",
  result: "ok" | "badpw" | "blocked",
): { label: string; tone: "success" | "danger" | "info" } {
  if (kind === "register") return { label: "注册", tone: "info" };
  if (result === "badpw") return { label: "密码错误", tone: "danger" };
  if (result === "blocked") return { label: "账号已停用", tone: "danger" };
  return { label: "成功", tone: "success" };
}

/** 生成任务状态的展示：文案、Tag 语气，以及是否进行中 */
export function taskStatusView(status: string): {
  label: string;
  tone: "success" | "danger" | "info" | "neutral";
  running: boolean;
} {
  switch (status) {
    case "succeeded":
      return { label: "成功", tone: "success", running: false };
    case "failed":
      return { label: "失败", tone: "danger", running: false };
    case "canceled":
      return { label: "已取消", tone: "neutral", running: false };
    case "expired":
      return { label: "已超时", tone: "danger", running: false };
    default:
      return { label: "进行中", tone: "info", running: true };
  }
}

/**
 * 取审计详情里的备注：detail_json 可能是 JSON 字符串或对象，取不到返回空串
 * @param detail 审计的 detail_json
 */
export function auditNote(detail: unknown): string {
  let value: unknown = detail;
  if (typeof detail === "string") {
    try {
      value = JSON.parse(detail);
    } catch {
      return "";
    }
  }
  if (typeof value !== "object" || value === null) return "";
  const note = (value as Record<string, unknown>).note;
  return typeof note === "string" ? note : "";
}

/** 后端 ErrUserNotFound 的业务码 */
const USER_NOT_FOUND_CODE = 20001;

/**
 * 是不是「用户不存在」：404 或业务码 20001。URL 里带了已被删除的 ?user= 时，抽屉据此显示就地提示
 * @param error 请求抛出的错误
 */
export function isUserNotFound(error: unknown): boolean {
  if (typeof error !== "object" || error === null) return false;
  const e = error as { code?: unknown; status?: unknown };
  return e.status === 404 || e.code === USER_NOT_FOUND_CODE;
}

/**
 * 任务耗时：不足 1 分钟写「34s」，超过写「2m5s」；未完成（null）写「—」
 * @param ms 耗时毫秒
 */
export function formatDurationMs(ms: number | null | undefined): string {
  if (ms === null || ms === undefined) return "—";
  const seconds = Math.round(ms / 1000);
  if (seconds < 60) return `${seconds}s`;
  return `${Math.floor(seconds / 60)}m${seconds % 60}s`;
}

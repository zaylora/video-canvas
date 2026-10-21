import type {
  AgentTokenItem,
  IssueAgentTokenReq,
  IssuedAgentToken,
} from "@/api/admin/agent-token/type.d";

/** 用途最多多少字（与后端一致） */
export const TOKEN_NAME_MAX = 64;
/** 有效期最短天数 */
export const TOKEN_DAYS_MIN = 1;
/** 有效期最长天数：令牌放在本机 .env 里，泄露的风险靠过期兜底 */
export const TOKEN_DAYS_MAX = 90;
/** 签发表单里有效期的默认天数 */
export const TOKEN_DAYS_DEFAULT = 30;

/** 签发表单里用户填的原始值（有效期是输入框里的文本） */
export type IssueFormValues = {
  /** 用途 */
  name: string;
  /** 有效期（天）的输入文本 */
  days: string;
};

/** 校验的结果：通过给出规范化后的请求，不通过给出每个字段的错误 */
export type IssueFormResult =
  | { ok: true; value: IssueAgentTokenReq }
  | { ok: false; errors: { name?: string; days?: string } };

/**
 * 签发表单校验，规则与后端一致：用途 1–64 字（去首尾空格后，按字数算），有效期是 1–90 的整数
 * @param form 表单里的原始值
 * @returns 通过时是可直接发给接口的请求；否则是各字段的错误文案
 */
export function validateIssueForm(form: IssueFormValues): IssueFormResult {
  const errors: { name?: string; days?: string } = {};
  const name = form.name.trim();
  if (!name) errors.name = "请填写用途";
  else if ([...name].length > TOKEN_NAME_MAX) errors.name = `用途最多 ${TOKEN_NAME_MAX} 个字`;

  const text = form.days.trim();
  const days = Number(text);
  if (!/^\d+$/.test(text) || days < TOKEN_DAYS_MIN || days > TOKEN_DAYS_MAX)
    errors.days = `有效期必须是 ${TOKEN_DAYS_MIN}–${TOKEN_DAYS_MAX} 天`;

  if (errors.name || errors.days) return { ok: false, errors };
  return { ok: true, value: { name, days } };
}

/** 一次签发的结果：校验没过没有发请求；通过了就是接口的返回 */
export type RunIssueResult =
  | { ok: true; result: IssuedAgentToken }
  | { ok: false; errors: { name?: string; days?: string } };

/**
 * 签发：先本地校验，不通过就不发请求；通过后发规范化的请求。接口失败照常抛出（全局提示由拦截器弹）
 * @param form 表单里的原始值
 * @param issue 真正发请求的函数
 */
export async function runIssue(
  form: IssueFormValues,
  issue: (req: IssueAgentTokenReq) => Promise<IssuedAgentToken>,
): Promise<RunIssueResult> {
  const checked = validateIssueForm(form);
  if (!checked.ok) return checked;
  return { ok: true, result: await issue(checked.value) };
}

/** 令牌状态：有效 / 已注销 / 已过期（已注销的令牌列表里不再显示，这个状态只是兜底） */
export type TokenStatus = "active" | "revoked" | "expired";

/**
 * 令牌当前状态：注销优先于过期（注销过的就算过期了也显示已注销）
 * @param item 令牌记录
 * @param now 当前时间（毫秒）
 */
export function tokenStatus(item: AgentTokenItem, now: number): TokenStatus {
  if (item.revoked_at) return "revoked";
  const expires = Date.parse(item.expires_at);
  return !Number.isNaN(expires) && expires <= now ? "expired" : "active";
}

/** 令牌页的状态 */
export type TokenPageState = {
  /** 列表加载状态：loading 首次加载中，ready 已有数据，error 一次都没加载成功 */
  status: "loading" | "ready" | "error";
  /** 全部令牌，只有记录，没有明文 */
  items: AgentTokenItem[];
  /** 刚签发的令牌（含明文）：用户点「我已保存」之后清掉，只存在于内存 */
  issued: IssuedAgentToken | null;
};

/** 令牌页的事件 */
export type TokenPageAction =
  /** 列表拿到了 */
  | { type: "loaded"; items: AgentTokenItem[] }
  /** 列表没拿到 */
  | { type: "load-failed" }
  /** 签发成功 */
  | { type: "issued"; result: IssuedAgentToken }
  /** 用户点了「我已保存」 */
  | { type: "dismissed" }
  /** 注销成功：这个令牌从列表里消失，at 是注销时间 */
  | { type: "revoked"; id: number; at: string };

/** 令牌页的初始状态 */
export const initialTokenPage: TokenPageState = { status: "loading", items: [], issued: null };

/**
 * 令牌页的状态转移。明文只经过 issued 这一个字段：列表里只有记录，重新加载列表不会让明文回来或消失
 */
export function tokenPageReducer(state: TokenPageState, action: TokenPageAction): TokenPageState {
  switch (action.type) {
    case "loaded":
      // 已注销的令牌不显示（后端列表本来就不返回，这里再兜一层）
      return { ...state, status: "ready", items: action.items.filter((i) => !i.revoked_at) };
    case "load-failed":
      return state.status === "ready" ? state : { ...state, status: "error" };
    case "issued":
      return {
        status: "ready",
        items: [action.result.item, ...state.items.filter((i) => i.id !== action.result.item.id)],
        issued: action.result,
      };
    case "dismissed":
      return { ...state, issued: null };
    case "revoked":
      return { ...state, items: state.items.filter((i) => i.id !== action.id) };
  }
}

/** 令牌状态的显示文案 */
export const TOKEN_STATUS_LABEL: Record<TokenStatus, string> = {
  active: "有效",
  revoked: "已注销",
  expired: "已过期",
};

const pad = (n: number) => String(n).padStart(2, "0");

/**
 * 到期日显示成「2026-11-15」；解析不了原样返回
 * @param iso 后端给的 RFC3339 时间
 */
export function formatTokenDay(iso: string): string {
  const date = new Date(iso);
  if (Number.isNaN(date.getTime())) return iso;
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}`;
}

/**
 * 最近使用时间显示成「2026-10-20 22:10」；从未使用显示「从未使用」
 * @param iso 后端给的 RFC3339 时间，从未使用为 null
 */
export function formatTokenLastUsed(iso: string | null): string {
  if (!iso) return "从未使用";
  const date = new Date(iso);
  if (Number.isNaN(date.getTime())) return iso;
  return `${formatTokenDay(iso)} ${pad(date.getHours())}:${pad(date.getMinutes())}`;
}

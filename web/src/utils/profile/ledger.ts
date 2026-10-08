import type { LedgerItemDto, LedgerPageSize, LedgerType } from "@/api/me/type";

/** 每页条数的可选值，后端只接受这三档 */
export const LEDGER_PAGE_SIZES: LedgerPageSize[] = [10, 20, 50];

/** 默认每页条数 */
export const DEFAULT_LEDGER_PAGE_SIZE: LedgerPageSize = 20;

/** 页码条最多占几个位置（含省略号） */
const MAX_PAGE_SLOTS = 7;

/** 流水类型的中文名 */
export const LEDGER_TYPE_LABEL: Record<LedgerType, string> = {
  initial: "初始积分",
  freeze: "冻结",
  settle: "生成结算",
  refund: "解冻退回",
  admin_adjust: "管理员调整",
};

/** 页码条里的一项：页码或省略号 */
export type PageSlot = number | "…";

/**
 * 页码条要显示的页码：不超过 7 页时全部显示；否则首页、末页常显，当前页两侧各一页，
 * 其余用省略号折叠。省略号只省一页时直接显示那一页（省略号本身也占一个位置，省它没意义）。
 * @param current 当前页，从 1 开始
 * @param last 总页数
 * @returns 页码与省略号的序列，例如 [1, "…", 5, 6, 7, "…", 12]
 */
export function pageList(current: number, last: number): PageSlot[] {
  if (last <= MAX_PAGE_SLOTS) return Array.from({ length: last }, (_, i) => i + 1);
  let lo = Math.max(2, Math.min(current - 1, last - 4));
  let hi = Math.min(last - 1, Math.max(current + 1, 5));
  if (lo === 3) lo = 2;
  if (hi === last - 2) hi = last - 1;
  const out: PageSlot[] = [1];
  if (lo > 2) out.push("…");
  for (let page = lo; page <= hi; page += 1) out.push(page);
  if (hi < last - 1) out.push("…");
  out.push(last);
  return out;
}

/**
 * 总页数，至少 1 页
 * @param total 总条数
 * @param pageSize 每页条数
 */
export function lastPageOf(total: number, pageSize: number): number {
  return Math.max(1, Math.ceil(total / pageSize));
}

/**
 * 解析地址栏里的 page 参数：只接受正整数，其余回到第 1 页
 * @param value URLSearchParams.get("page") 的结果
 */
export function parsePageParam(value: string | null): number {
  if (!value || !/^\d+$/.test(value)) return 1;
  const page = Number(value);
  return page >= 1 ? page : 1;
}

/**
 * 解析每页条数：不在 10 / 20 / 50 里的回到默认 20
 * @param value 原始值
 */
export function parsePageSize(value: string | null): LedgerPageSize {
  const size = Number(value);
  return LEDGER_PAGE_SIZES.find((item) => item === size) ?? DEFAULT_LEDGER_PAGE_SIZE;
}

/** 金额的颜色：positive 绿色、default 正文色、muted 灰色 */
export type AmountTone = "positive" | "default" | "muted";

/**
 * 金额按「余额变化」展示：只有余额真的变了才带正负号。
 * 冻结、退回只动冻结额，灰色显示为「冻结 n / 解冻 n」，免得一次生成看起来被扣了两次。
 * @param item 一条流水
 * @returns 金额文案与颜色
 */
export function ledgerAmount(item: Pick<LedgerItemDto, "type" | "amount">): {
  text: string;
  tone: AmountTone;
} {
  const abs = Math.abs(item.amount);
  switch (item.type) {
    case "freeze":
      return { text: `冻结 ${abs}`, tone: "muted" };
    case "refund":
      return { text: `解冻 ${abs}`, tone: "muted" };
    case "settle":
      return { text: `−${abs}`, tone: "default" };
    case "initial":
      return { text: `+${abs}`, tone: "positive" };
    default:
      if (item.amount > 0) return { text: `+${abs}`, tone: "positive" };
      if (item.amount < 0) return { text: `−${abs}`, tone: "default" };
      return { text: "0", tone: "default" };
  }
}

/**
 * 流水行的副标题：关联任务显示「任务 #id」，Agent 调用显示「Agent 对话」，
 * 管理员调整显示备注原文（note 为 true，界面按纯文本一行截断、悬停看全文；不显示操作人），初始积分显示「注册赠送」
 * @param item 一条流水
 * @returns 副标题文案，以及它是不是管理员备注
 */
export function ledgerSubtitle(item: LedgerItemDto): { text: string; note: boolean } {
  if (item.taskId) return { text: `任务 #${item.taskId}`, note: false };
  if (item.agentCallId) return { text: "Agent 对话", note: false };
  if (item.type === "admin_adjust") return { text: item.note, note: true };
  if (item.type === "initial") return { text: "注册赠送", note: false };
  return { text: "", note: false };
}

/**
 * 流水时间：浏览器时区下的「2026-10-09 14:20」；解析失败原样返回
 * @param value 创建时间（ISO）
 */
export function formatLedgerTime(value: string): string {
  const time = Date.parse(value);
  if (Number.isNaN(time)) return value;
  const date = new Date(time);
  const pad = (n: number) => String(n).padStart(2, "0");
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())} ${pad(date.getHours())}:${pad(date.getMinutes())}`;
}

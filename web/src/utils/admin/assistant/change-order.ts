import type { AdminRole } from "@/api/admin/ai/type";
import type {
  AdminChangeItem,
  AdminPricingBasis,
  AdminChangeItemResult,
  AdminChangePayload,
  AdminChangeResult,
  AgentApprovalDto,
  DecideAgentApprovalReq,
} from "@/api/agent/type";
import { ACTIVE_RUN_STATUSES } from "@/utils/agent/session-state";

/**
 * 后台助手「变更单」（审批种类 admin_change）的纯逻辑：哪些条目能勾选、勾选计数、
 * 决定请求体、读审批与执行结果。契约见 docs/design/后台Agent助手/一期接口契约.md 第 3 节。
 */

/** 变更单的整体状态：待审核 / 应用中 / 已应用 / 部分应用 / 全部未应用 / 已拒绝 / 已过期 */
export type OrderPhase =
  | "pending"
  | "applying"
  | "applied"
  | "partial"
  | "failed"
  | "rejected"
  | "expired";

/**
 * 当前角色能不能确认要求某角色的条目：super_admin 什么都能确认，admin 只能确认 admin 级的
 * @param required 条目要求的角色
 * @param role 当前角色，未确认时为 null
 */
export function roleSatisfies(required: AdminRole, role: AdminRole | null): boolean {
  if (role === "super_admin") return true;
  return role === "admin" && required === "admin";
}

/**
 * 条目能不能勾选：校验通过（status 为 ok）且角色够。
 * 其余的条目照样展示（让人看到助手哪里没做对），但不能勾
 * @param item 变更条目
 * @param role 当前角色
 */
export function isPickable(item: AdminChangeItem, role: AdminRole | null): boolean {
  return item.status === "ok" && roleSatisfies(item.required_role, role);
}

/**
 * 条目有没有实际改动：至少一个字段改前改后不同（按 JSON 内容比较）。
 * 取整后价格没变的条目就是没有改动，应用了也没有效果
 * @param item 变更条目
 */
export function hasEffect(item: AdminChangeItem): boolean {
  return item.diff.some(
    (d) => JSON.stringify(d.before ?? null) !== JSON.stringify(d.after ?? null),
  );
}

/**
 * 默认勾选：所有能勾选、且有实际改动的条目。只看 diff，不依赖提醒文案
 * @param items 变更条目
 * @param role 当前角色
 * @returns 被勾选的条目下标
 */
export function defaultSelection(items: AdminChangeItem[], role: AdminRole | null): Set<number> {
  const picked = new Set<number>();
  items.forEach((item, index) => {
    if (isPickable(item, role) && hasEffect(item)) picked.add(index);
  });
  return resolveSelection(items, role, picked);
}

/**
 * 清除不可选或依赖未选的条目，反复收敛以覆盖多级依赖及任意条目顺序。
 * @param items 同一张单的条目
 * @param role 当前角色
 * @param selected 用户勾选的条目下标
 * @returns 满足权限、状态和依赖的新集合
 */
export function resolveSelection(
  items: AdminChangeItem[],
  role: AdminRole | null,
  selected: ReadonlySet<number>,
): Set<number> {
  const next = new Set([...selected].filter((i) => items[i] && isPickable(items[i], role)));
  let changed: boolean;
  do {
    changed = false;
    for (const i of next) {
      if ((items[i].depends_on ?? []).some((dependency) => !next.has(dependency))) {
        next.delete(i);
        changed = true;
      }
    }
  } while (changed);
  return next;
}

/**
 * 已选条数：只算能勾选的条目
 * @param items 变更条目
 * @param role 当前角色
 * @param selected 勾选的下标
 */
export function selectedCount(
  items: AdminChangeItem[],
  role: AdminRole | null,
  selected: ReadonlySet<number>,
): number {
  return resolveSelection(items, role, selected).size;
}

/**
 * 切换一个条目的勾选，返回新集合
 * @param selected 当前勾选
 * @param index 条目下标
 * @param items 同单条目，传入时清除依赖未选的下游条目
 * @param role 当前角色
 */
export function toggleSelection(
  selected: ReadonlySet<number>,
  index: number,
  items?: AdminChangeItem[],
  role: AdminRole | null = null,
): Set<number> {
  const next = new Set(selected);
  if (next.has(index)) next.delete(index);
  else next.add(index);
  return items ? resolveSelection(items, role, next) : next;
}

/**
 * 「应用 n 项变更」的请求体：每一项都明确给出 approve，没勾选或不能勾选的为 false
 * @param items 变更条目
 * @param role 当前角色
 * @param selected 勾选的下标
 */
export function buildApproveDecision(
  items: AdminChangeItem[],
  role: AdminRole | null,
  selected: ReadonlySet<number>,
): DecideAgentApprovalReq {
  const picked = resolveSelection(items, role, selected);
  return {
    decision: "approve",
    items: items.map((_, index) => ({
      index,
      approve: picked.has(index),
    })),
  };
}

/**
 * 拒绝的请求体：理由是给助手看的，去掉首尾空格，空的不带
 * @param answer 拒绝原因
 */
export function buildRejectDecision(answer: string): DecideAgentApprovalReq {
  const text = answer.trim();
  return text ? { decision: "reject", answer: text } : { decision: "reject" };
}

/**
 * 「回滚」按钮发出的消息：助手据此出一张反向变更单
 * @param orderNo 要回滚的变更单号
 */
export const rollbackMessage = (orderNo: string) => `回滚变更单 ${orderNo}`;

const arr = <T>(v: unknown): T[] => (Array.isArray(v) ? (v as T[]) : []);

/**
 * 读 admin_change 审批的内容；种类不对或没有条目返回 null。条目里缺的数组字段补成空数组
 * @param approval 审批
 */
export function adminChangePayload(approval: AgentApprovalDto): AdminChangePayload | null {
  if (approval.kind !== "admin_change") return null;
  const p = approval.payload as Partial<AdminChangePayload> | undefined;
  if (!p || !Array.isArray(p.items) || p.items.length === 0) return null;
  return {
    order_no: typeof p.order_no === "string" ? p.order_no : "",
    title: typeof p.title === "string" ? p.title : "",
    reason: typeof p.reason === "string" ? p.reason : "",
    ...(typeof p.revert_of === "string" && p.revert_of && { revert_of: p.revert_of }),
    items: p.items.map((raw) => {
      const it = raw as Partial<AdminChangeItem>;
      return {
        target: it.target ?? "model",
        action: it.action,
        key: it.key ?? "",
        label: it.label ?? it.key ?? "",
        live: it.live === true,
        status: it.status ?? "ok",
        issues: arr<string>(it.issues),
        warnings: arr<string>(it.warnings),
        required_role: it.required_role === "super_admin" ? "super_admin" : "admin",
        diff: arr(it.diff),
        basis: arr(it.basis),
        depends_on: arr<number>(it.depends_on),
        detail: it.detail,
      };
    }),
  };
}

/**
 * 读执行结果；执行前、形状不对都是 null
 * @param approval 审批
 */
export function adminChangeResult(approval: AgentApprovalDto): AdminChangeResult | null {
  const r = approval.result as Partial<AdminChangeResult> | null | undefined;
  if (!r || !Array.isArray(r.items)) return null;
  return {
    items: r.items,
    applied_by: typeof r.applied_by === "string" ? r.applied_by : "",
    applied_at: typeof r.applied_at === "string" ? r.applied_at : "",
    audit_ids: arr<number>(r.audit_ids),
  };
}

/**
 * 某一项的执行结果
 * @param result 执行结果
 * @param index 条目下标
 * @returns 没有（还没执行、这一项没出现在结果里）返回 null
 */
export function itemOutcome(
  result: AdminChangeResult | null,
  index: number,
): AdminChangeItemResult | null {
  return result?.items.find((x) => x.index === index) ?? null;
}

/**
 * 统计各种结果的条数（被拒绝的不算）
 * @param result 执行结果
 */
export function resultCounts(result: AdminChangeResult): {
  applied: number;
  failed: number;
  skipped: number;
} {
  const count = (status: AdminChangeItemResult["status"]) =>
    result.items.filter((x) => x.status === status).length;
  return { applied: count("applied"), failed: count("failed"), skipped: count("skipped") };
}

/**
 * 整张变更单的状态，由审批状态推出
 * @param approval 审批
 */
export function orderPhase(approval: AgentApprovalDto): OrderPhase {
  switch (approval.status) {
    case "pending":
      return "pending";
    case "executed":
      return "applied";
    case "partially_approved":
      return "partial";
    case "failed":
      return "failed";
    case "rejected":
      return "rejected";
    case "expired":
      return "expired";
    default:
      return "applying";
  }
}

/**
 * 能不能回滚：至少有一条已应用，且这张单本身不是回滚单
 * @param approval 审批
 */
export function canRollback(approval: AgentApprovalDto): boolean {
  const payload = adminChangePayload(approval);
  const result = adminChangeResult(approval);
  if (!payload || !result || payload.revert_of) return false;
  return (
    result.items.some((x) => x.status === "applied") &&
    payload.items.every(
      (x) =>
        !["user", "register_settings", "smtp_settings", "storage", "image_processor"].includes(
          x.target,
        ),
    )
  );
}

/**
 * 差异里一个值的显示：没有显示破折号，字符串、数字原样，数组和对象是紧凑 JSON
 * @param value 差异里的值
 */
export function formatDiffValue(value: unknown): string {
  if (value === null || value === undefined) return "—";
  if (typeof value === "string") return value;
  if (typeof value === "number" || typeof value === "boolean") return String(value);
  return JSON.stringify(value);
}

/** 计算依据里的一行：标签、值，值是链接时带 href */
export type BasisRow = {
  /** 标签 */
  label: string;
  /** 值 */
  value: string;
  /** 来源链接，只放 http(s) */
  href?: string;
};

/** 数字去掉浮点尾巴后的文本 */
const num = (n: number) => String(Math.round(n * 1e6) / 1e6);

/**
 * 把一条价格计算依据展开成键值行，给「计算依据」表格画。
 * 成本加价：上游报价、来源、汇率、积分换算、人民币、成本、加价率、售价；相对调价：原价、调整、售价
 * @param basis 计算依据
 */
export function basisRows(basis: AdminPricingBasis): BasisRow[] {
  const rows: BasisRow[] = [];
  if (basis.mode === "relative") {
    if (basis.from !== undefined) rows.push({ label: "原价", value: `${num(basis.from)} 积分` });
    if (basis.pct !== undefined)
      rows.push({ label: "调整", value: `${basis.pct > 0 ? "+" : ""}${num(basis.pct)}%` });
    rows.push({ label: "售价", value: `${num(basis.price)} 积分` });
    return rows;
  }
  const up = basis.upstream;
  if (up) {
    rows.push({ label: "上游报价", value: `${num(up.amount)} ${up.currency} / ${up.unit}` });
    if (up.note) rows.push({ label: "备注", value: up.note });
    rows.push({
      label: "来源",
      value: up.source_url,
      ...(/^https?:\/\//i.test(up.source_url) && { href: up.source_url }),
    });
  }
  if (basis.fx) {
    const parts = [
      `USD/CNY ${num(basis.fx.rate)}`,
      basis.fx.date,
      basis.fx.source === "override" ? "手动覆盖" : "实时",
      basis.fx.stale ? "汇率可能过期" : null,
    ].filter(Boolean);
    rows.push({ label: "汇率", value: parts.join(" · ") });
  }
  if (basis.credits_per_yuan !== undefined)
    rows.push({ label: "积分换算", value: `1 元 = ${num(basis.credits_per_yuan)} 积分` });
  if (basis.yuan !== undefined) rows.push({ label: "人民币", value: `¥${basis.yuan.toFixed(4)}` });
  if (basis.cost !== undefined) rows.push({ label: "成本", value: `${num(basis.cost)} 积分` });
  if (basis.markup_pct !== undefined)
    rows.push({ label: "加价率", value: `${num(basis.markup_pct)}%` });
  rows.push({ label: "售价", value: `${num(basis.price)} 积分` });
  return rows;
}

/**
 * 变更单是不是还能决定：审批待决定，且它所在的那一轮运行还在等它。
 * 运行已经结束（被新要求替换、被停止、过期）而审批还停在待决定的，是作废的单，只读
 * @param approval 审批
 * @param runStatus 它所在运行的状态，没有记录时为 undefined（按还能决定处理）
 */
export function isOrderActionable(
  approval: AgentApprovalDto,
  runStatus: string | undefined,
): boolean {
  return (
    approval.status === "pending" &&
    (runStatus === undefined || (ACTIVE_RUN_STATUSES as readonly string[]).includes(runStatus))
  );
}

/**
 * 时刻 HH:mm（本地时间）
 * @param at 毫秒时间戳
 */
export function clock(at: number): string {
  const d = new Date(at);
  return `${String(d.getHours()).padStart(2, "0")}:${String(d.getMinutes()).padStart(2, "0")}`;
}

/**
 * 变更单的一句话标题：标题优先，没有标题用单号；内容读不出来时是「变更单」
 * @param approval 审批
 */
export function adminChangeTitle(approval: AgentApprovalDto): string {
  const payload = adminChangePayload(approval);
  if (!payload) return "变更单";
  return payload.title || (payload.order_no ? `变更单 ${payload.order_no}` : "变更单");
}

/**
 * 决定之后记录行上的结果摘要：已应用 / 失败 / 未应用条数（0 的不写，已应用总写）、操作人、时间。
 * 还没有执行结果（待审核、被拒绝、应用中）返回 null
 * @param approval 审批
 */
export function resultSummary(approval: AgentApprovalDto): string | null {
  const result = adminChangeResult(approval);
  if (!result) return null;
  const counts = resultCounts(result);
  const parts = [`已应用 ${counts.applied} 项`];
  if (counts.failed > 0) parts.push(`失败 ${counts.failed} 项`);
  if (counts.skipped > 0) parts.push(`跳过 ${counts.skipped} 项`);
  if (result.applied_by) parts.push(result.applied_by);
  const at = Date.parse(result.applied_at);
  if (!Number.isNaN(at)) parts.push(clock(at));
  return parts.join(" · ");
}

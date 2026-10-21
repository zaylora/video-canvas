import type {
  AgentRunDigest,
  AgentRunListQuery,
  AgentStatsGroup,
} from "@/api/admin/agent-run/type.d";
import type { TagTone } from "@/components/admin-ui/tag";

/**
 * 后台「运行记录」页的纯逻辑：筛选 → 查询参数、状态与反馈的显示、版本对比卡片的涨跌判断、列表行徽标。
 * 契约见 docs/design/Agent运行观测/Agent运行观测.md 第 6.7、6.8 节。
 */

// ---------------------------------------------------------------- 筛选

/** 时间范围预设：today 今天（本地 0 点起）、7d 近 7 天、30d 近 30 天、all 不限 */
export type RunRange = "today" | "7d" | "30d" | "all";

/** 状态分组：把后端十来个状态收成管理员会用的几类 */
export type RunStatusGroup = "all" | "succeeded" | "failed" | "canceled" | "waiting";

/** 反馈筛选：all 不限、up 点赞、down 点踩、none 没评价 */
export type RunFeedbackChoice = "all" | "up" | "down" | "none";

/** 筛选栏里的全部条件；Harness、模型是空串表示不限，用户 ID 是输入框里生效的文本 */
export type RunFilters = {
  /** 时间范围 */
  range: RunRange;
  /** 状态分组 */
  status: RunStatusGroup;
  /** 反馈 */
  feedback: RunFeedbackChoice;
  /** 只看被撤销过的 */
  undone: boolean;
  /** Harness 版本（harness_key），空串不限 */
  harnessKey: string;
  /** 模型（model_key），空串不限 */
  modelKey: string;
  /** 用户 ID 的文本，空串不限 */
  userId: string;
};

/** 默认筛选：什么都不限 */
export const DEFAULT_RUN_FILTERS: RunFilters = {
  range: "all",
  status: "all",
  feedback: "all",
  undone: false,
  harnessKey: "",
  modelKey: "",
  userId: "",
};

/** 时间范围下拉：[值, 文字] */
export const RUN_RANGE_OPTIONS: ReadonlyArray<readonly [RunRange, string]> = [
  ["all", "全部时间"],
  ["today", "今天"],
  ["7d", "近 7 天"],
  ["30d", "近 30 天"],
];

/** 状态下拉：[值, 文字] */
export const RUN_STATUS_OPTIONS: ReadonlyArray<readonly [RunStatusGroup, string]> = [
  ["all", "全部状态"],
  ["succeeded", "成功"],
  ["failed", "失败类"],
  ["canceled", "已停止"],
  ["waiting", "等待中"],
];

/** 反馈下拉：[值, 文字] */
export const RUN_FEEDBACK_OPTIONS: ReadonlyArray<readonly [RunFeedbackChoice, string]> = [
  ["all", "全部反馈"],
  ["up", "👍 有帮助"],
  ["down", "👎 没帮上忙"],
  ["none", "未评价"],
];

/**
 * 状态分组对应的后端状态：失败类与后端算「失败结束」的集合一致（失败、超时、中断、步数或预算用尽）；
 * 等待中是等审批和等回答
 */
const STATUS_GROUP_STATUSES: Record<Exclude<RunStatusGroup, "all">, string[]> = {
  succeeded: ["succeeded"],
  failed: ["failed", "timeout", "interrupted", "step_limit", "budget_exhausted"],
  canceled: ["canceled"],
  waiting: ["waiting_approval", "waiting_input"],
};

const DAY_MS = 24 * 60 * 60 * 1000;

/**
 * 时间范围的起点
 * @param range 预设
 * @param now 当前时间（毫秒）
 * @returns RFC3339；不限返回 undefined
 */
function rangeSince(range: RunRange, now: number): string | undefined {
  if (range === "all") return undefined;
  if (range === "today") {
    const start = new Date(now);
    start.setHours(0, 0, 0, 0);
    return start.toISOString();
  }
  return new Date(now - (range === "7d" ? 7 : 30) * DAY_MS).toISOString();
}

/**
 * 用户 ID 输入框的规整：接受纯数字，可带 # 前缀和首尾空格，去掉前导 0
 * @param text 输入框里的文本
 * @returns 合法的正整数文本；不合法返回空串（页面把输入框清掉）
 */
export function normalizeUserIdInput(text: string): string {
  const match = /^#?(\d+)$/.exec(text.trim());
  if (!match) return "";
  const id = Number(match[1]);
  return Number.isSafeInteger(id) && id > 0 ? String(id) : "";
}

/**
 * 筛选 → 列表接口的查询参数。不含 cursor 和 limit：筛选一变就从第一页重新查，游标由分页 hook 追加
 * @param filters 筛选栏的条件
 * @param now 当前时间（毫秒），用来算时间预设的起点
 * @returns 只含已生效条件的查询参数
 */
export function runListQuery(
  filters: RunFilters,
  now: number,
  paging?: { page: number; pageSize: number },
): AgentRunListQuery {
  const query: AgentRunListQuery = {};
  if (paging) {
    query.page = paging.page;
    query.limit = paging.pageSize;
  }
  const since = rangeSince(filters.range, now);
  if (since) query.since = since;
  if (filters.status !== "all") query.status = STATUS_GROUP_STATUSES[filters.status].join(",");
  if (filters.feedback !== "all") query.feedback = filters.feedback;
  if (filters.undone) query.undone = true;
  if (filters.harnessKey) query.harness_key = filters.harnessKey;
  if (filters.modelKey) query.model_key = filters.modelKey;
  const userId = normalizeUserIdInput(filters.userId);
  if (userId) query.user_id = Number(userId);
  return query;
}

/**
 * 有没有任何筛选在生效（决定「重置筛选」是否可点、空状态里是否给重置）
 * @param filters 筛选栏的条件
 */
export function hasActiveRunFilters(filters: RunFilters): boolean {
  return (Object.keys(DEFAULT_RUN_FILTERS) as (keyof RunFilters)[]).some(
    (key) => filters[key] !== DEFAULT_RUN_FILTERS[key],
  );
}

/**
 * 点版本卡片：按该版本筛选；再点同一张取消
 * @param filters 当前筛选
 * @param harnessKey 被点的卡片的 harness_key
 */
export function toggleHarnessFilter(filters: RunFilters, harnessKey: string): RunFilters {
  return { ...filters, harnessKey: filters.harnessKey === harnessKey ? "" : harnessKey };
}

// ---------------------------------------------------------------- 状态与反馈

/** 状态的显示：中文名 + Tag 色调 */
export type RunStatusView = { label: string; tone: TagTone };

const STATUS_VIEW: Record<string, RunStatusView> = {
  succeeded: { label: "成功", tone: "success" },
  failed: { label: "失败", tone: "danger" },
  timeout: { label: "超时", tone: "danger" },
  interrupted: { label: "已中断", tone: "danger" },
  step_limit: { label: "步数用尽", tone: "warning" },
  budget_exhausted: { label: "预算用尽", tone: "warning" },
  canceled: { label: "已停止", tone: "neutral" },
  waiting_approval: { label: "等待审批", tone: "info" },
  waiting_input: { label: "等待回答", tone: "info" },
  queued: { label: "排队中", tone: "info" },
  running: { label: "运行中", tone: "info" },
  expired: { label: "已过期", tone: "neutral" },
};

/**
 * 运行状态 → 中文名和色调；后端新增了页面不认识的状态时原样显示、用中性色，不会白屏
 * @param status 后端状态值
 */
export function runStatusView(status: string): RunStatusView {
  return STATUS_VIEW[status] ?? { label: status, tone: "neutral" };
}

/** 点踩原因标签的中文（与用户端评价条的选项一致） */
const FEEDBACK_REASON_LABEL: Record<string, string> = {
  misread: "没理解我的需求",
  wrong_edit: "画布改错了",
  slow: "步骤太多或太慢",
  bad_output: "生成结果不好",
  other: "其他",
};

/**
 * 点踩原因标签 → 中文；后端新增的标签原样返回
 * @param key 标签值
 */
export function feedbackReasonLabel(key: string): string {
  return FEEDBACK_REASON_LABEL[key] ?? key;
}

// ---------------------------------------------------------------- Harness / 模型下拉

/**
 * Harness 版本的显示名
 * @param promptVersion 系统提示词版本
 * @param key harness_key（取前 6 位）
 * @returns 如「v10 · a1b2c3」
 */
export function harnessLabel(promptVersion: number, key: string): string {
  return `v${promptVersion} · ${key.slice(0, 6)}`;
}

type SelectOptions = Array<readonly [value: string, label: string]>;

/**
 * 在选项末尾补上当前选中但不在统计窗口里的值，免得下拉显示成空白（比如从别处带来的老版本）
 */
function withSelected(options: SelectOptions, selected: string, label: string): SelectOptions {
  if (!selected || options.some(([value]) => value === selected)) return options;
  return [...options, [selected, label]];
}

/**
 * Harness 下拉的选项：首项「全部」，其后按统计的顺序（最近运行的在前）
 * @param groups 按 harness_key 分组的统计
 * @param selected 当前选中的 harness_key，不在统计里也会保留
 */
export function harnessOptions(groups: AgentStatsGroup[], selected = ""): SelectOptions {
  const options: SelectOptions = [
    ["", "全部"],
    ...groups.map((g) => [g.key, harnessLabel(g.prompt_version, g.key)] as const),
  ];
  return withSelected(options, selected, selected.slice(0, 6));
}

/**
 * 模型下拉的选项：首项「全部」，其后按统计的顺序
 * @param groups 按 model_key 分组的统计
 * @param selected 当前选中的 model_key，不在统计里也会保留
 */
export function modelOptions(groups: AgentStatsGroup[], selected = ""): SelectOptions {
  const options: SelectOptions = [["", "全部"], ...groups.map((g) => [g.key, g.key] as const)];
  return withSelected(options, selected, selected);
}

// ---------------------------------------------------------------- 版本对比

/** 样本数低于它的版本只显示数值，不给「变好 / 变坏」的结论（设计 6.7） */
export const MIN_SAMPLE = 30;

/** 两版相差小于这么多个百分点算持平 */
export const FLAT_THRESHOLD_PP = 2;

/** 版本对比关心的三个「越低越好」的比率 */
export type CompareMetric = "down_rate" | "undo_rate" | "fail_end_rate";

/** 一项比率相对上一版的变化 */
export type MetricDelta = {
  /** 哪个指标 */
  metric: CompareMetric;
  /** 较新版减较旧版，单位是百分点，保留一位小数 */
  diffPp: number;
  /** 结论：降低为好 */
  verdict: "better" | "worse" | "flat";
};

const COMPARE_METRICS: CompareMetric[] = ["down_rate", "undo_rate", "fail_end_rate"];

/** 比率指标的中文名 */
export const COMPARE_METRIC_LABEL: Record<CompareMetric, string> = {
  down_rate: "点踩率",
  undo_rate: "撤销率",
  fail_end_rate: "失败结束率",
};

/**
 * 较新一版相对较旧一版的涨跌。任一边样本不足就不给结论
 * @param newer 较新的版本
 * @param older 较旧的版本
 * @returns 三项比率的变化；样本不足返回 null
 */
export function compareVersions(
  newer: AgentStatsGroup,
  older: AgentStatsGroup,
): MetricDelta[] | null {
  if (newer.n < MIN_SAMPLE || older.n < MIN_SAMPLE) return null;
  return COMPARE_METRICS.map((metric) => {
    // 先取到 0.1 个百分点再比较，避免 0.12 − 0.10 = 0.0199999… 这类浮点误差跨过 2 个百分点的边界；+ 0 把 -0 变成 0
    const diffPp = Math.round((newer[metric] - older[metric]) * 1000) / 10 + 0;
    const verdict = Math.abs(diffPp) < FLAT_THRESHOLD_PP ? "flat" : diffPp < 0 ? "better" : "worse";
    return { metric, diffPp, verdict };
  });
}

/** 版本卡片要显示的内容 */
export type VersionCardView = {
  /** 这一版的统计 */
  group: AgentStatsGroup;
  /** 版本名，如「v10 · a1b2c3」 */
  label: string;
  /** 样本不足（n < 30） */
  insufficient: boolean;
  /** 相对相邻较旧一张的变化；样本不足、没有较旧一张时为 null */
  deltas: MetricDelta[] | null;
};

/** 版本对比最多显示几张卡片 */
export const VERSION_CARD_COUNT = 3;

/**
 * 统计分组 → 版本卡片：取最近的 3 个版本（后端已按最近运行时间倒序），每张只和相邻较旧的一张比
 * @param groups 按 harness_key 分组的统计
 */
export function buildVersionCards(groups: AgentStatsGroup[]): VersionCardView[] {
  const top = groups.slice(0, VERSION_CARD_COUNT);
  return top.map((group, i) => {
    const older = top[i + 1];
    return {
      group,
      label: harnessLabel(group.prompt_version, group.key),
      insufficient: group.n < MIN_SAMPLE,
      deltas: older ? compareVersions(group, older) : null,
    };
  });
}

/**
 * 比率显示成百分比
 * @param rate 0–1 的小数
 * @returns 如「12.3%」
 */
export function formatRate(rate: number): string {
  return `${(rate * 100).toFixed(1)}%`;
}

/**
 * 平均积分显示成一位小数
 * @param credits 平均积分
 */
export function formatCredits(credits: number): string {
  return credits.toFixed(1);
}

// ---------------------------------------------------------------- 列表行徽标

/** 请求首句旁的小徽标 */
export type RunRowBadge = {
  /** 徽标种类，同时用作 key */
  key: "tool_errors" | "steer" | "rejected";
  /** 徽标上的短文字 */
  text: string;
  /** 悬浮说明 */
  title: string;
};

/**
 * 工具报错、用户插话、审批被拒大于 0 时，在请求首句旁给一个小徽标，扫一眼就知道这次运行不太顺
 * @param digest 运行摘要
 */
export function runRowBadges(digest: AgentRunDigest): RunRowBadge[] {
  const badges: RunRowBadge[] = [];
  if (digest.tool_errors > 0)
    badges.push({
      key: "tool_errors",
      text: `报错 ${digest.tool_errors}`,
      title: `工具报错 ${digest.tool_errors} 次`,
    });
  if (digest.steer_count > 0)
    badges.push({
      key: "steer",
      text: `插话 ${digest.steer_count}`,
      title: `用户中途插话 ${digest.steer_count} 次`,
    });
  if (digest.approvals_rejected > 0)
    badges.push({
      key: "rejected",
      text: `拒审 ${digest.approvals_rejected}`,
      title: `用户拒绝了 ${digest.approvals_rejected} 次审批`,
    });
  return badges;
}

/**
 * 统计窗口的起点：版本对比卡片和两个下拉都看最近 30 天
 * @param now 当前时间（毫秒）
 */
export function statsSince(now: number): string {
  return new Date(now - 30 * DAY_MS).toISOString();
}

/**
 * 总页数：向上取整，没有数据也算 1 页
 * @param total 命中总数
 * @param pageSize 每页条数
 */
export function pageCount(total: number, pageSize: number): number {
  return Math.max(1, Math.ceil(total / pageSize));
}

/**
 * 把页码修正到 1 到总页数之间：改了每页条数或数据变少后，当前页可能已经越界
 * @param page 想去的页码
 * @param total 命中总数
 * @param pageSize 每页条数
 */
export function clampPage(page: number, total: number, pageSize: number): number {
  return Math.min(Math.max(1, page), pageCount(total, pageSize));
}

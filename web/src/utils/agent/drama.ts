import type { AgentEventDto, AgentMode, AgentSessionDto } from "@/api/agent/type";

/**
 * 成功后要重新拉 `GET /dramas/:id` 的短剧助手工具（契约 §15）：
 * 改大纲 / 改集没有专门的推送事件；改片段、提交任务虽有 drama.segment / drama.job，
 * 但集摘要（片段数、时长、成片是否过期）要靠详情刷新。只读的 drama_read 不刷新。
 */
export const DRAMA_REFRESH_TOOLS: readonly string[] = [
  "drama_update_outline",
  "drama_update_episode",
  "drama_update_segment",
  "drama_run_job",
];

/** 这条 Agent 事件是不是「短剧助手的写工具成功结束」：是就该刷新短剧详情；别的画布、失败的调用、其他事件都不算 */
export function dramaRefreshFor(event: AgentEventDto, canvasId: string): boolean {
  if (event.type !== "tool.end" || event.canvas_id !== canvasId) return false;
  if (event.data?.is_error === true) return false;
  return typeof event.data?.name === "string" && DRAMA_REFRESH_TOOLS.includes(event.data.name);
}

/**
 * 当前入口看得到的会话：短剧助手只看 drama 会话；画布 Agent 不看 drama 会话
 * （它们在短剧画布上建，普通模式的运行不能续它们）。没有 drama 会话时原样返回，引用不变。
 */
export function visibleSessions(sessions: AgentSessionDto[], mode: AgentMode): AgentSessionDto[] {
  if (mode === "drama") return sessions.filter((s) => s.mode === "drama");
  return sessions.some((s) => s.mode === "drama")
    ? sessions.filter((s) => s.mode !== "drama")
    : sessions;
}

/** 通用修改审批（非生成 / 删除 / 提问）的内容：标题、理由、修改前后的文本摘要 */
export type EditPayload = { title: string; reason: string; before: string; after: string };

/** 修改审批的内容；字段缺失或类型不对一律补空串 */
export function editPayload(raw: unknown): EditPayload {
  const p = (raw ?? {}) as Record<string, unknown>;
  const text = (v: unknown) => (typeof v === "string" ? v : "");
  return {
    title: text(p.title),
    reason: text(p.reason),
    before: text(p.before),
    after: text(p.after),
  };
}

/** 修改审批的一句话标题 */
export const editTitle = (payload: EditPayload): string => payload.title || "申请修改";

/** 修改审批里一个字段的前后对比 */
export type EditChange = {
  /** 字段的中文名 */
  label: string;
  before: string;
  after: string;
};

/** 短剧助手写工具的审批里认识的字段（按展示顺序）：后端把整个对象序列化成 JSON 放进 before / after */
const FIELD_LABELS: readonly [key: string, label: string, unit?: string][] = [
  ["title", "标题"],
  ["synopsis", "梗概"],
  ["script_md", "剧本"],
  ["outline_md", "故事大纲"],
  ["characters_md", "人物传记"],
  ["prompt", "提示词"],
  ["seconds", "时长", " 秒"],
];

/** 解析成普通对象；不是 JSON 或不是对象返回 null */
function parseObject(text: string): Record<string, unknown> | null {
  try {
    const value: unknown = JSON.parse(text);
    return value !== null && typeof value === "object" && !Array.isArray(value)
      ? (value as Record<string, unknown>)
      : null;
  } catch {
    return null;
  }
}

/**
 * 把修改审批的 before / after 拆成改动字段清单，只列真正变了的字段（后端连没改的字段也带着）。
 * 不是 JSON、或里面没有认识的字段（如提交任务的审批）返回 null，调用方照旧显示原文；
 * 认识的字段都没变返回空数组。
 */
export function editChanges(payload: Pick<EditPayload, "before" | "after">): EditChange[] | null {
  const before = parseObject(payload.before);
  const after = parseObject(payload.after);
  if (!before || !after) return null;
  const known = FIELD_LABELS.filter(([key]) => key in before || key in after);
  if (known.length === 0) return null;
  const show = (value: unknown, unit = "") =>
    value === undefined || value === null ? "" : `${String(value)}${unit}`;
  return known
    .filter(([key]) => show(before[key]) !== show(after[key]))
    .map(([key, label, unit]) => ({
      label,
      before: show(before[key], unit),
      after: show(after[key], unit),
    }));
}

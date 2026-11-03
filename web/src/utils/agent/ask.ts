import type {
  AgentApprovalDto,
  AgentAskDecidedAnswer,
  AgentAskPayload,
  AgentAskQuestion,
  DecideAgentApprovalReq,
} from "@/api/agent/type";

/** 「其他，我来说」在草稿里的占位键，不会和选项文字撞上 */
export const CUSTOM = "\u0000custom";

/** 决定请求里 answer 的字数上限（后端 binding 的 max） */
const ANSWER_MAX = 2000;

const NUMBER_RE = /^[+-]?(\d+\.?\d*|\.\d+)$/;

/** 一道题填到哪了。选项和模型按「选项文字 / 模型 key」记，follow 与自定义的输入另存 */
export interface AskDraft {
  /** 选中的选项文字（choice）、模型 key（model）或 CUSTOM */
  picked: string[];
  /** 选项带 follow 时，选中后输入的文字，按选项文字记 */
  follow: Record<string, string>;
  /** 「其他，我来说」输入的文字 */
  custom: string;
  /** text / url / number 的输入 */
  text: string;
}

export const emptyDraft = (): AskDraft => ({ picked: [], follow: {}, custom: "", text: "" });

/** 一道题当前的状态：empty 没填好（含选了 follow 选项却还没写字）；ok 填好了；bad 填了但不合格（数字不是数字、链接不是 http(s)） */
export type AskState = "empty" | "ok" | "bad";

/**
 * 提问的各题。新提问带 questions；历史里的旧单题提问没有，按顶层字段还原成一题（必答，id 为 "1"）。
 */
export function askQuestions(p: AgentAskPayload): AgentAskQuestion[] {
  if (p.questions?.length) return p.questions;
  return [
    {
      id: "1",
      question: p.question,
      kind: p.kind,
      required: true,
      options: (p.options ?? []).map((label) => ({ label })),
      allow_custom: p.allow_custom,
      model_kind: p.model_kind,
      models: p.models,
    },
  ];
}

/**
 * 单题里「点一下就提交」能搞定的：选模型、不多选且没有 follow 输入框的选项题。
 * 这种提问沿用点选即提交的快速选项，不出表单和提交按钮。
 */
export function isQuickAsk(qs: AgentAskQuestion[]): boolean {
  const q = qs[0];
  if (qs.length !== 1 || !q) return false;
  if (q.kind === "model") return true;
  return q.kind === "choice" && !q.multi && !(q.options ?? []).some((o) => o.follow);
}

/**
 * 这道题选中的某一项最终的回答文字；选了 follow 却没写字时为空。
 * follow 选项写成「选项：输入」：几个 follow 选项的输入长得差不多（名称、链接、价格），只给输入模型分不清选的是哪项
 */
function pickedText(q: AgentAskQuestion, d: AskDraft, key: string): string {
  if (key === CUSTOM) return d.custom.trim();
  const opt = q.options?.find((o) => o.label === key);
  if (!opt?.follow) return key;
  const text = (d.follow[key] ?? "").trim();
  return text && `${key}：${text}`;
}

/** 一道题的回答值：单选与文字题是字符串，多选是数组；没填好是 null */
export function answerValue(q: AgentAskQuestion, d: AskDraft): string | string[] | null {
  if (q.kind === "text" || q.kind === "url" || q.kind === "number") {
    const text = d.text.trim();
    return text || null;
  }
  if (q.kind === "model") return d.picked[0] ?? null;
  const values = d.picked.map((key) => pickedText(q, d, key));
  if (values.length === 0 || values.some((v) => !v)) return null;
  return q.multi ? [...new Set(values)] : values[0];
}

/** 一道题填没填、填得对不对 */
export function askState(q: AgentAskQuestion, d: AskDraft): AskState {
  if (q.kind === "text" || q.kind === "url" || q.kind === "number") {
    const text = d.text.trim();
    if (!text) return "empty";
    if (q.kind === "number") return NUMBER_RE.test(text) ? "ok" : "bad";
    if (q.kind === "url") return isHttpUrl(text) ? "ok" : "bad";
    return "ok";
  }
  return answerValue(q, d) === null ? "empty" : "ok";
}

function isHttpUrl(text: string): boolean {
  try {
    const u = new URL(text);
    return (u.protocol === "http:" || u.protocol === "https:") && u.host !== "";
  } catch {
    return false;
  }
}

/** 必填题都答好、选填题要么没填要么填对，才能提交 */
export function canSubmit(qs: AgentAskQuestion[], drafts: Record<string, AskDraft>): boolean {
  return qs.every((q) => {
    const state = askState(q, drafts[q.id] ?? emptyDraft());
    return q.required ? state === "ok" : state !== "bad";
  });
}

/** 已经答好的题数，给「n / N」进度用 */
export function answeredCount(qs: AgentAskQuestion[], drafts: Record<string, AskDraft>): number {
  return qs.filter((q) => askState(q, drafts[q.id] ?? emptyDraft()) === "ok").length;
}

/** 回答值的可读文字：选模型带上 key（Agent 要拿它建节点），多选用顿号连起来 */
export function readableAnswer(q: AgentAskQuestion, value: string | string[]): string {
  if (q.kind === "model") {
    const m = q.models?.find((x) => x.key === value);
    return m ? `${m.name}（模型 key：${m.key}）` : String(value);
  }
  return Array.isArray(value) ? value.join("、") : value;
}

/**
 * 提交时的决定：逐题的 answers，加上拼好的一句 answer（单题就是回答本身，多题是「问题：回答」用分号连起来），
 * 旧后端和旧的展示只认 answer。没答的选填题不放进 answers。
 */
export function buildAskDecision(
  qs: AgentAskQuestion[],
  drafts: Record<string, AskDraft>,
): DecideAgentApprovalReq {
  const answers: NonNullable<DecideAgentApprovalReq["answers"]> = [];
  const sentences: string[] = [];
  for (const q of qs) {
    const d = drafts[q.id] ?? emptyDraft();
    if (askState(q, d) !== "ok") continue;
    const value = answerValue(q, d);
    if (value === null) continue;
    answers.push({ id: q.id, value });
    sentences.push(
      qs.length === 1 ? readableAnswer(q, value) : `${q.question}：${readableAnswer(q, value)}`,
    );
  }
  const answer = Array.from(sentences.join("；")).slice(0, ANSWER_MAX).join("");
  return { decision: "approve", answer, answers };
}

/** 提问记录行里的一行：问题和回答，没答的回答为空 */
export interface AskAnsweredRow {
  question: string;
  answer: string;
}

/** 逐题的回答记录；旧决定（只有一句 answer）还原成一行；没有任何回答时是空数组 */
export function askAnsweredRows(approval: AgentApprovalDto): AskAnsweredRow[] {
  const decision = approval.decision;
  const list = decision?.answers;
  if (Array.isArray(list) && list.length > 0)
    return (list as AgentAskDecidedAnswer[]).map((a) => ({
      question: a.question,
      answer: typeof a.answer === "string" ? a.answer : "",
    }));
  const answer = typeof decision?.answer === "string" ? decision.answer : "";
  if (!answer) return [];
  return [{ question: (approval.payload as AgentAskPayload).question, answer }];
}

/** 提问共有几个问题（旧提问算一个） */
export function askCount(approval: AgentApprovalDto): number {
  return askQuestions(approval.payload as AgentAskPayload).length;
}

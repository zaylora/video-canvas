import { adminTargetPath } from "@/utils/admin/assistant/change-result";

/**
 * 助手展示卡片（show_card 工具，card.shown 事件）：卡片的类型、前端再校验的解析，以及 ref / 来源链接的安全处理。
 * Agent 服务已经校验并归一化过一次；这里是第二道关——事件来自数据库和网络，坏数据不能把消息流弄崩，
 * 结构不对的整张卡不渲染（不画半张），ref 和链接按白名单取舍。
 */

export type CardTone = "ok" | "warn" | "bad" | "info";

/** 卡片里的跳转引用。type 可能是没见过的（旧版本、模型写错），解析时保留，渲染时由 resolveRef 取舍 */
export type CardRef = { type: string; key?: string };

export type CardSummaryRow = {
  label: string;
  value: string;
  sub?: string;
  badge?: string;
  tone?: CardTone;
  ref?: CardRef;
};
export type CardCell = { text: string; tone?: CardTone; ref?: CardRef };
export type CardNote = { tone: CardTone; text: string };
/** url 只在是合法 http(s) 网址时才有；没有就只显示标题 */
export type CardSource = { title: string; url?: string };

type CardBase = { title: string; tone: CardTone; notes: CardNote[]; sources: CardSource[] };
export type SummaryCard = CardBase & { kind: "summary"; rows: CardSummaryRow[] };
export type TableCard = CardBase & {
  kind: "table";
  columns: string[];
  rows: { cells: CardCell[] }[];
};
export type AgentCard = SummaryCard | TableCard;

const TONES: readonly CardTone[] = ["ok", "warn", "bad", "info"];
const isObj = (v: unknown): v is Record<string, unknown> =>
  typeof v === "object" && v !== null && !Array.isArray(v);
const str = (v: unknown) => (typeof v === "string" ? v : "");
const tone = (v: unknown): CardTone | undefined =>
  TONES.includes(v as CardTone) ? (v as CardTone) : undefined;

function parseRef(v: unknown): CardRef | undefined {
  if (!isObj(v) || !str(v.type)) return undefined;
  return { type: str(v.type), ...(str(v.key) && { key: str(v.key) }) };
}

/**
 * 来源链接只放行 http(s)。用 URL 解析而不是前缀判断：javascript:、data:、协议相对地址都不过。
 * @param url 模型给的网址
 * @returns 规整后的网址；不合法返回 undefined
 */
export function safeSourceUrl(url: string): string | undefined {
  try {
    const u = new URL(url.trim());
    return u.protocol === "http:" || u.protocol === "https:" ? u.href : undefined;
  } catch {
    return undefined;
  }
}

function parseNotes(v: unknown): CardNote[] {
  if (!Array.isArray(v)) return [];
  return v.flatMap((n) =>
    isObj(n) && str(n.text) ? [{ tone: tone(n.tone) ?? "info", text: str(n.text) }] : [],
  );
}

function parseSources(v: unknown): CardSource[] {
  if (!Array.isArray(v)) return [];
  return v.flatMap((s) => {
    if (!isObj(s)) return [];
    const url = safeSourceUrl(str(s.url));
    const title = str(s.title) || url;
    return title ? [{ title, ...(url && { url }) }] : [];
  });
}

function parseCell(v: unknown): CardCell | null {
  if (typeof v === "string") return { text: v };
  if (!isObj(v) || typeof v.text !== "string") return null;
  const t = tone(v.tone);
  const ref = parseRef(v.ref);
  return { text: v.text, ...(t && { tone: t }), ...(ref && { ref }) };
}

/**
 * 把 card.shown 事件里的卡片解析成前端用的结构。
 * @param raw 事件载荷里的 card
 * @returns 结构不对（类型不认识、没有标题、没有行、表格的列数和单元格数对不上）返回 null，调用方整张卡不渲染
 */
export function parseCard(raw: unknown): AgentCard | null {
  if (!isObj(raw) || !str(raw.title) || !Array.isArray(raw.rows) || raw.rows.length === 0)
    return null;
  const base = {
    title: str(raw.title),
    tone: tone(raw.tone) ?? "info",
    notes: parseNotes(raw.notes),
    sources: parseSources(raw.sources),
  };
  if (raw.kind === "summary") {
    const rows: CardSummaryRow[] = [];
    for (const r of raw.rows) {
      if (!isObj(r) || !str(r.label) || !str(r.value)) return null;
      const t = tone(r.tone);
      const ref = parseRef(r.ref);
      rows.push({
        label: str(r.label),
        value: str(r.value),
        ...(str(r.sub) && { sub: str(r.sub) }),
        ...(str(r.badge) && { badge: str(r.badge) }),
        ...(t && { tone: t }),
        ...(ref && { ref }),
      });
    }
    return { kind: "summary", ...base, rows };
  }
  if (raw.kind === "table") {
    if (!Array.isArray(raw.columns) || raw.columns.length < 2) return null;
    const columns = raw.columns.map(str);
    if (columns.some((c) => !c)) return null;
    const rows: { cells: CardCell[] }[] = [];
    for (const r of raw.rows) {
      if (!isObj(r) || !Array.isArray(r.cells) || r.cells.length !== columns.length) return null;
      const cells = r.cells.map(parseCell);
      if (cells.some((c) => c === null)) return null;
      rows.push({ cells: cells as CardCell[] });
    }
    return { kind: "table", ...base, columns, rows };
  }
  return null;
}

/** 卡片所在的位置：后台助手只能跳后台页面，画布里的助手只能定位画布节点 */
export type RefScope = "admin" | "canvas";

/** ref 解析出的去向：后台页面路径，或画布节点 */
export type RefTarget = { kind: "page"; to: string } | { kind: "node"; id: string };

/**
 * 把模型给的 {type, key} 换成去向。白名单之外的类型、缺少 key、当前位置用不了的类型
 * （后台里出现 node、画布里出现后台页面）都返回 null，调用方只显示文字。
 * @param ref 卡片里的引用
 * @param scope 卡片所在的位置
 */
export function resolveRef(ref: CardRef | undefined, scope: RefScope): RefTarget | null {
  if (!ref) return null;
  if (ref.type === "node")
    return scope === "canvas" && ref.key ? { kind: "node", id: ref.key } : null;
  if (scope !== "admin") return null;
  const to = adminTargetPath(ref.type, ref.key ?? "");
  // 模型、渠道需要 key 才能定位；没有 key 的退成纯文字，不跳到一个没选中的列表
  if (!to || ((ref.type === "model" || ref.type === "channel") && !ref.key)) return null;
  return { kind: "page", to };
}

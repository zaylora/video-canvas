/**
 * show_card 的参数校验与归一化。
 *
 * show_card 是纯展示工具：模型只给结构化数据，这里在 Agent 服务里校验（和 plan_update 一样，不经过 Go），
 * 通过后归一化成 card.shown 事件里的卡片，前端按它渲染。校验不过返回清楚的中文错误，让模型自己改，不渲染半张卡。
 * 前端对 ref 和 URL 还会再校验一次，这里是第一道关。
 */

export type CardTone = "ok" | "warn" | "bad" | "info";
export const CARD_TONES: readonly CardTone[] = ["ok", "warn", "bad", "info"];
/** ref 的白名单；前端把它映射成路由，未知类型只显示文字 */
export const REF_TYPES = [
  "model",
  "channel",
  "plugin",
  "pricing_settings",
  "user",
  "register_settings",
  "smtp_settings",
  "storage",
  "image_processor",
  "node",
] as const;
export type CardRefType = (typeof REF_TYPES)[number];
export interface CardRef {
  type: CardRefType;
  key?: string;
}

export interface SummaryRow {
  label: string;
  value: string;
  sub?: string;
  badge?: string;
  tone?: CardTone;
  ref?: CardRef;
}
export interface TableCell {
  text: string;
  tone?: CardTone;
  ref?: CardRef;
}
export interface CardNote {
  tone: CardTone;
  text: string;
}
export interface CardSource {
  title: string;
  url: string;
}
interface CardBase {
  title: string;
  tone: CardTone;
  notes?: CardNote[];
  sources?: CardSource[];
}
export type Card =
  | (CardBase & { kind: "summary"; rows: SummaryRow[] })
  | (CardBase & { kind: "table"; columns: string[]; rows: Array<{ cells: TableCell[] }> });

export const CARD_LIMITS = {
  /** 一轮最多展示几张卡 */
  perRun: 3,
  title: 40,
  label: 12,
  text: 120,
  badge: 12,
  sourceTitle: 40,
  column: 12,
  key: 200,
  url: 2000,
  summaryRows: 12,
  tableRows: 20,
  columnsMin: 2,
  columnsMax: 5,
  notes: 3,
  sources: 5,
} as const;

export type CardResult = { ok: true; card: Card } | { ok: false; error: string };

class Bad extends Error {}
const len = (s: string) => Array.from(s).length;
const isObj = (v: unknown): v is Record<string, unknown> =>
  typeof v === "object" && v !== null && !Array.isArray(v);

/** 必填文字：去首尾空白后不能为空、不超长 */
function need(v: unknown, path: string, max: number): string {
  const s = typeof v === "string" ? v.trim() : "";
  if (s === "" || len(s) > max) throw new Bad(`${path} 不能为空，且不超过 ${max} 个字`);
  return s;
}

/** 选填文字：没给（或空串）返回 undefined；给了就不能超长，也不能不是字符串 */
function opt(v: unknown, path: string, max: number): string | undefined {
  if (v === undefined || v === null) return undefined;
  if (typeof v !== "string") throw new Bad(`${path} 必须是字符串`);
  const s = v.trim();
  if (s === "") return undefined;
  if (len(s) > max) throw new Bad(`${path} 不超过 ${max} 个字`);
  return s;
}

function tone(v: unknown, path: string): CardTone | undefined {
  if (v === undefined || v === null) return undefined;
  if (typeof v !== "string" || !CARD_TONES.includes(v as CardTone))
    throw new Bad(`${path} 只能是 ${CARD_TONES.join(" / ")}`);
  return v as CardTone;
}

function ref(v: unknown, path: string): CardRef | undefined {
  if (v === undefined || v === null) return undefined;
  if (!isObj(v)) throw new Bad(`${path} 格式不对，应为 {type, key}`);
  if (typeof v.type !== "string" || !REF_TYPES.includes(v.type as CardRefType))
    throw new Bad(`${path}.type 只能是 ${REF_TYPES.join(" / ")}`);
  const type = v.type as CardRefType;
  if (type === "pricing_settings") return { type };
  const key = typeof v.key === "string" ? v.key.trim() : "";
  if (key === "" || len(key) > CARD_LIMITS.key)
    throw new Bad(
      `${path}.key 不能为空，且不超过 ${CARD_LIMITS.key} 个字符（${type} 需要对象标识）`,
    );
  return { type, key };
}

function list(v: unknown, path: string, min: number, max: number): unknown[] {
  if (!Array.isArray(v) || v.length < min || v.length > max)
    throw new Bad(`${path} 需要 ${min} 到 ${max} 项`);
  return v;
}

function notes(v: unknown): CardNote[] | undefined {
  if (v === undefined || v === null) return undefined;
  if (!Array.isArray(v) || v.length > CARD_LIMITS.notes)
    throw new Bad(`notes 最多 ${CARD_LIMITS.notes} 条`);
  const out = v.map((n, i) => {
    if (!isObj(n)) throw new Bad(`notes[${i}] 格式不对，应为 {tone?, text}`);
    return {
      tone: tone(n.tone, `notes[${i}].tone`) ?? "info",
      text: need(n.text, `notes[${i}].text`, CARD_LIMITS.text),
    };
  });
  return out.length > 0 ? out : undefined;
}

/** 来源链接只允许 http(s)；前端渲染前还会再校验一次 */
function sources(v: unknown): CardSource[] | undefined {
  if (v === undefined || v === null) return undefined;
  if (!Array.isArray(v) || v.length > CARD_LIMITS.sources)
    throw new Bad(`sources 最多 ${CARD_LIMITS.sources} 条`);
  const out = v.map((s, i) => {
    if (!isObj(s)) throw new Bad(`sources[${i}] 格式不对，应为 {title, url}`);
    const title = need(s.title, `sources[${i}].title`, CARD_LIMITS.sourceTitle);
    const raw = typeof s.url === "string" ? s.url.trim() : "";
    let url: URL | undefined;
    try {
      url = new URL(raw);
    } catch {
      /* 下面统一报错 */
    }
    if (
      !url ||
      (url.protocol !== "http:" && url.protocol !== "https:") ||
      len(raw) > CARD_LIMITS.url
    ) {
      throw new Bad(
        `sources[${i}].url 必须是 http 或 https 网址（不超过 ${CARD_LIMITS.url} 个字符）`,
      );
    }
    return { title, url: url.href };
  });
  return out.length > 0 ? out : undefined;
}

function summaryRow(r: unknown, i: number): SummaryRow {
  const p = `rows[${i}]`;
  if (!isObj(r)) throw new Bad(`${p} 格式不对，应为 {label, value, sub?, badge?, tone?, ref?}`);
  const row: SummaryRow = {
    label: need(r.label, `${p}.label`, CARD_LIMITS.label),
    value: need(r.value, `${p}.value`, CARD_LIMITS.text),
  };
  const sub = opt(r.sub, `${p}.sub`, CARD_LIMITS.text);
  const badge = opt(r.badge, `${p}.badge`, CARD_LIMITS.badge);
  const t = tone(r.tone, `${p}.tone`);
  const rf = ref(r.ref, `${p}.ref`);
  if (sub) row.sub = sub;
  if (badge) row.badge = badge;
  if (t) row.tone = t;
  if (rf) row.ref = rf;
  return row;
}

function tableCell(c: unknown, path: string): TableCell {
  if (typeof c === "string") {
    const text = c.trim();
    if (len(text) > CARD_LIMITS.text) throw new Bad(`${path} 不超过 ${CARD_LIMITS.text} 个字`);
    return { text };
  }
  if (!isObj(c) || typeof c.text !== "string")
    throw new Bad(`${path} 必须是字符串或 {text, tone?, ref?}`);
  const text = c.text.trim();
  if (len(text) > CARD_LIMITS.text) throw new Bad(`${path}.text 不超过 ${CARD_LIMITS.text} 个字`);
  const cell: TableCell = { text };
  const t = tone(c.tone, `${path}.tone`);
  const rf = ref(c.ref, `${path}.ref`);
  if (t) cell.tone = t;
  if (rf) cell.ref = rf;
  return cell;
}

function validate(args: unknown): Card {
  if (!isObj(args)) throw new Bad("参数格式不对：需要一个对象 {kind, title, rows, …}");
  if (args.kind !== "summary" && args.kind !== "table")
    throw new Bad("kind 只能是 summary 或 table");
  const base = {
    title: need(args.title, "title", CARD_LIMITS.title),
    tone: tone(args.tone, "tone") ?? "info",
  };
  const extra = { notes: notes(args.notes), sources: sources(args.sources) };
  const withExtra = <T extends object>(c: T): T => ({
    ...c,
    ...(extra.notes && { notes: extra.notes }),
    ...(extra.sources && { sources: extra.sources }),
  });

  if (args.kind === "summary") {
    const rows = list(args.rows, "rows", 1, CARD_LIMITS.summaryRows).map(summaryRow);
    return withExtra({ kind: "summary" as const, ...base, rows });
  }
  const columns = list(args.columns, "columns", CARD_LIMITS.columnsMin, CARD_LIMITS.columnsMax).map(
    (c, i) => need(c, `columns[${i}]`, CARD_LIMITS.column),
  );
  const rows = list(args.rows, "rows", 1, CARD_LIMITS.tableRows).map((r, i) => {
    const cells = isObj(r) ? r.cells : undefined;
    if (!Array.isArray(cells) || cells.length !== columns.length)
      throw new Bad(`rows[${i}].cells 必须是数组，且个数等于列数 ${columns.length}`);
    return { cells: cells.map((c, j) => tableCell(c, `rows[${i}].cells[${j}]`)) };
  });
  return withExtra({ kind: "table" as const, ...base, columns, rows });
}

/** 校验并归一化 show_card 的参数 */
export function validateCard(args: unknown): CardResult {
  try {
    return { ok: true, card: validate(args) };
  } catch (e) {
    if (e instanceof Bad) return { ok: false, error: e.message };
    throw e;
  }
}

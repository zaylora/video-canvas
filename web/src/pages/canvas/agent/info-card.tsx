import type { ReactNode } from "react";
import {
  ArrowUpRight,
  CircleAlert,
  CircleCheck,
  CircleX,
  Info,
  TriangleAlert,
  type LucideIcon,
} from "lucide-react";

import { useFocusNode } from "@/components/canvas-editor/use-focus-node";
import { cn } from "@/lib/utils";
import { useAgentHighlight } from "@/store/agent-highlight";
import {
  resolveRef,
  type AgentCard,
  type CardCell,
  type CardTone,
  type RefScope,
  type RefTarget,
} from "@/utils/agent/card";

import { PageLink } from "./change-result-card";
import { MoreButton, useFold } from "./fold-list";

/** 对比表默认露出的行数，其余收进「还有 N 项」 */
const TABLE_FOLD = 6;

/** tone 对应的图标和文字色：沿用现有的状态色（成功绿、警告黄、错误红），info 用弱化前景色 */
const TONE: Record<CardTone, { icon: LucideIcon; text: string; dot: string; label: string }> = {
  ok: { icon: CircleCheck, text: "text-status-success", dot: "bg-status-success", label: "正常" },
  warn: {
    icon: TriangleAlert,
    text: "text-status-warning",
    dot: "bg-status-warning",
    label: "注意",
  },
  bad: { icon: CircleX, text: "text-destructive", dot: "bg-destructive", label: "异常" },
  info: { icon: Info, text: "text-muted-foreground", dot: "bg-muted-foreground/60", label: "信息" },
};

/** 点击节点引用：选中并飞到节点，再闪一下；节点已不在画布上时什么都不做 */
function useNodeJump() {
  const focus = useFocusNode();
  return (id: string) => {
    if (focus(id)) useAgentHighlight.getState().touch([id]);
  };
}

/**
 * 一个可点击的引用。后台页面用站内跳转（PageLink），画布节点定位并高亮；
 * 没有去向（类型不认识、当前位置用不了、缺 key）时只显示文字，不留可点的假象。
 */
function RefText({
  refTarget,
  className,
  children,
}: {
  refTarget: RefTarget | null;
  className?: string;
  children: ReactNode;
}) {
  const jump = useNodeJump();
  if (!refTarget) return <span className={cn("min-w-0 wrap-anywhere", className)}>{children}</span>;
  const cls = cn(
    "hover:bg-chrome-hover focus-visible:ring-node-ring/60 -mx-1 inline-flex w-fit max-w-full items-center gap-1 rounded-md px-1 text-left outline-none hover:underline focus-visible:ring-2",
    className,
  );
  const inner = (
    <>
      <span className="min-w-0 wrap-anywhere">{children}</span>
      <ArrowUpRight aria-hidden className="text-muted-foreground size-3 shrink-0" />
    </>
  );
  if (refTarget.kind === "page")
    return (
      <PageLink to={refTarget.to} className={cls}>
        {inner}
      </PageLink>
    );
  return (
    <button type="button" onClick={() => jump(refTarget.id)} className={cls}>
      {inner}
    </button>
  );
}

/** 单元格里的状态点：颜色之外有隐藏文字，读屏和不分色的人也知道状态 */
function ToneDot({ tone }: { tone: CardTone }) {
  return (
    <>
      <span aria-hidden className={cn("size-1.5 shrink-0 rounded-full", TONE[tone].dot)} />
      <span className="sr-only">{TONE[tone].label}</span>
    </>
  );
}

function Badge({ text, tone }: { text: string; tone?: CardTone }) {
  return (
    <span
      className={cn(
        "bg-chrome-hover ring-chrome-border inline-flex h-5 shrink-0 items-center rounded-full px-2 text-[11px] whitespace-nowrap ring-1",
        tone ? TONE[tone].text : "text-muted-foreground",
      )}
    >
      {text}
    </span>
  );
}

function SummaryBody({
  card,
  scope,
}: {
  card: Extract<AgentCard, { kind: "summary" }>;
  scope: RefScope;
}) {
  return (
    <dl className="grid grid-cols-[auto_minmax(0,1fr)_auto] items-baseline gap-x-3 gap-y-1.5 text-[13px]">
      {card.rows.map((row, i) => (
        <div key={i} className="col-span-3 grid grid-cols-subgrid items-baseline">
          <dt className="text-muted-foreground">{row.label}</dt>
          <dd className="m-0 flex min-w-0 flex-col">
            <RefText refTarget={resolveRef(row.ref, scope)} className="font-medium">
              {row.value}
            </RefText>
            {row.sub && (
              <span className="text-muted-foreground font-mono text-xs wrap-anywhere">
                {row.sub}
              </span>
            )}
          </dd>
          <dd className="m-0 justify-self-end">
            {row.badge && <Badge text={row.badge} tone={row.tone} />}
          </dd>
        </div>
      ))}
    </dl>
  );
}

function Cell({ cell, scope }: { cell: CardCell; scope: RefScope }) {
  return (
    <span className="inline-flex min-w-0 items-center gap-1.5">
      {cell.tone && <ToneDot tone={cell.tone} />}
      <RefText refTarget={resolveRef(cell.ref, scope)}>{cell.text}</RefText>
    </span>
  );
}

function TableBody({
  card,
  scope,
}: {
  card: Extract<AgentCard, { kind: "table" }>;
  scope: RefScope;
}) {
  const fold = useFold(card.rows.length, TABLE_FOLD);
  return (
    <div className="flex flex-col gap-1.5">
      <div className="overflow-x-auto">
        <table className="w-full border-collapse text-left text-[13px]">
          <thead>
            <tr className="text-muted-foreground text-xs">
              {card.columns.map((c, i) => (
                <th key={i} scope="col" className="py-1 pr-3 font-medium whitespace-nowrap">
                  {c}
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            {card.rows.slice(0, fold.visible).map((row, i) => (
              <tr key={i} className="border-chrome-border border-t align-baseline">
                {row.cells.map((cell, j) => (
                  <td key={j} className="py-1.5 pr-3">
                    <Cell cell={cell} scope={scope} />
                  </td>
                ))}
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      <MoreButton count={fold.hidden} onClick={fold.expand} />
    </div>
  );
}

/**
 * 助手用 show_card 展示的卡片：结论卡（标题 + 「标签｜值｜徽标」行）和对比表。
 * 清透风格：半透明底加细描边，状态只用图标和文字色表达；表格超过 6 行时折叠。
 * 引用（ref）按 scope 取舍：后台跳后台页面、画布定位节点，用不了的只显示文字；来源链接只放行 http(s)，新窗口打开。
 * @param card 已解析的卡片（见 parseCard）
 * @param scope 卡片所在的位置
 */
export function InfoCard({ card, scope }: { card: AgentCard; scope: RefScope }) {
  const tone = TONE[card.tone];
  const Icon = tone.icon;
  return (
    <section
      aria-label={card.title}
      className="bg-chrome-hover ring-chrome-border flex flex-col gap-2.5 rounded-2xl p-3 ring-1"
    >
      <h3 className="flex items-start gap-2 text-[13px] leading-5 font-semibold">
        <Icon aria-hidden className={cn("mt-0.5 size-4 shrink-0", tone.text)} />
        <span className="min-w-0 wrap-anywhere">{card.title}</span>
        <span className="sr-only">（{tone.label}）</span>
      </h3>
      {card.kind === "summary" ? (
        <SummaryBody card={card} scope={scope} />
      ) : (
        <TableBody card={card} scope={scope} />
      )}
      {card.notes.length > 0 && (
        <ul aria-label="提示" className="flex flex-col gap-1">
          {card.notes.map((n, i) => {
            const NoteIcon = n.tone === "info" ? CircleAlert : TONE[n.tone].icon;
            return (
              <li
                key={i}
                className={cn("flex items-start gap-1.5 text-xs leading-5", TONE[n.tone].text)}
              >
                <NoteIcon aria-hidden className="mt-0.5 size-3.5 shrink-0" />
                <span className="min-w-0 wrap-anywhere">{n.text}</span>
              </li>
            );
          })}
        </ul>
      )}
      {card.sources.length > 0 && (
        <ul aria-label="来源" className="flex flex-wrap gap-x-3 gap-y-1 text-xs">
          {card.sources.map((s, i) => (
            <li key={i} className="min-w-0">
              {s.url ? (
                <a
                  href={s.url}
                  target="_blank"
                  rel="noreferrer noopener"
                  className="text-muted-foreground hover:text-foreground focus-visible:ring-node-ring/60 inline-flex max-w-full items-center gap-0.5 rounded outline-none hover:underline focus-visible:ring-2"
                >
                  <span className="min-w-0 wrap-anywhere">{s.title}</span>
                  <ArrowUpRight aria-hidden className="size-3 shrink-0" />
                </a>
              ) : (
                <span className="text-muted-foreground wrap-anywhere">{s.title}</span>
              )}
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}

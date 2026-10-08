import { useCallback, useEffect, useRef, useState, type ComponentType } from "react";
import { useSearchParams } from "react-router";
import NumberFlow from "@number-flow/react";
import {
  ChevronLeft,
  ChevronRight,
  Gift,
  Inbox,
  LoaderCircle,
  Receipt,
  RotateCcw,
  Shield,
  Snowflake,
  Undo2,
} from "lucide-react";

import { getMeLedger } from "@/api/me";
import type { LedgerFilter, LedgerPageDto, LedgerPageSize, LedgerType } from "@/api/me/type";
import { Segmented } from "@/components/home/segmented";
import { Button } from "@/components/ui/button";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { DURATION, EASE_OUT_CSS, ms } from "@/lib/motion";
import { cn } from "@/lib/utils";
import { useCreditsStore } from "@/store/credits";
import {
  DEFAULT_LEDGER_PAGE_SIZE,
  formatLedgerTime,
  lastPageOf,
  LEDGER_PAGE_SIZES,
  LEDGER_TYPE_LABEL,
  ledgerAmount,
  ledgerSubtitle,
  pageList,
  parsePageParam,
  parsePageSize,
  type AmountTone,
} from "@/utils/profile/ledger";

/** 筛选项：对应后端 type=all|task|admin */
const FILTERS: { value: LedgerFilter; label: string }[] = [
  { value: "all", label: "全部" },
  { value: "task", label: "生成任务" },
  { value: "admin", label: "管理员调整" },
];

/** 流水类型的图标 */
const TYPE_ICON: Record<LedgerType, ComponentType<{ className?: string }>> = {
  freeze: Snowflake,
  settle: Receipt,
  refund: Undo2,
  admin_adjust: Shield,
  initial: Gift,
};

/** 金额颜色 */
const TONE_CLASS: Record<AmountTone, string> = {
  positive: "text-status-success",
  default: "text-foreground",
  muted: "text-muted-foreground font-normal",
};

/** 翻页时旧内容变淡的过渡：只动 opacity */
const DIM_TRANSITION = { transition: `opacity ${ms(DURATION.base)} ${EASE_OUT_CSS}` };

/** 顶栏高度：翻页后把卡片顶滚到顶栏下面 */
const HEADER_OFFSET = 64;

/** 一张余额卡 */
function BalanceCard({ label, value, hint }: { label: string; value?: number; hint: string }) {
  return (
    <div className="bg-card ring-foreground/10 grid gap-1 rounded-xl p-4 ring-1">
      <span className="text-muted-foreground text-xs">{label}</span>
      <span className="text-2xl font-semibold tracking-tight tabular-nums">
        {value === undefined ? "—" : <NumberFlow value={value} />}
      </span>
      <span className="text-muted-foreground text-xs">{hint}</span>
    </div>
  );
}

/**
 * 页码条：左侧「共 N 条」与失败重试，右侧每页条数、上一页、页码、下一页。
 * 只有 1 页时隐藏翻页部分；加载中禁用全部按钮。
 */
function Pager({
  total,
  page,
  pageSize,
  loading,
  error,
  onPage,
  onPageSize,
  onRetry,
}: {
  total: number;
  page: number;
  pageSize: LedgerPageSize;
  loading: boolean;
  error: boolean;
  onPage: (page: number) => void;
  onPageSize: (size: LedgerPageSize) => void;
  onRetry: () => void;
}) {
  const last = lastPageOf(total, pageSize);
  return (
    <div className="flex flex-wrap items-center gap-x-2 gap-y-2 pt-3 text-xs">
      <span className="text-muted-foreground tabular-nums">共 {total.toLocaleString()} 条</span>
      {error && (
        <span className="text-destructive flex items-center gap-1.5" role="alert">
          加载失败，
          <Button variant="outline" size="xs" onClick={onRetry}>
            <RotateCcw />
            重试
          </Button>
        </span>
      )}
      {loading && (
        <LoaderCircle className="text-muted-foreground size-3.5 animate-spin" aria-label="加载中" />
      )}
      <div className="ml-auto flex flex-wrap items-center gap-1">
        <Select
          value={String(pageSize)}
          onValueChange={(value) => value && onPageSize(parsePageSize(String(value)))}
          disabled={loading}
        >
          <SelectTrigger size="sm" className="w-[5.5rem] text-xs" aria-label="每页条数">
            <SelectValue>{(value: string) => `${value} 条/页`}</SelectValue>
          </SelectTrigger>
          <SelectContent side="top" alignItemWithTrigger={false}>
            {LEDGER_PAGE_SIZES.map((size) => (
              <SelectItem key={size} value={String(size)}>
                {size} 条/页
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        {last > 1 && (
          <nav aria-label="分页" className="flex items-center gap-1">
            <Button
              variant="outline"
              size="icon-sm"
              aria-label="上一页"
              disabled={loading || page <= 1}
              onClick={() => onPage(page - 1)}
            >
              <ChevronLeft />
            </Button>
            {pageList(page, last).map((slot, index) =>
              slot === "…" ? (
                <span key={`gap-${index}`} className="text-muted-foreground px-1" aria-hidden>
                  …
                </span>
              ) : (
                <Button
                  key={slot}
                  variant={slot === page ? "default" : "ghost"}
                  size="sm"
                  className="min-w-8 px-2 tabular-nums"
                  aria-current={slot === page ? "page" : undefined}
                  aria-label={`第 ${slot} 页`}
                  disabled={loading}
                  onClick={() => slot !== page && onPage(slot)}
                >
                  {slot}
                </Button>
              ),
            )}
            <Button
              variant="outline"
              size="icon-sm"
              aria-label="下一页"
              disabled={loading || page >= last}
              onClick={() => onPage(page + 1)}
            >
              <ChevronRight />
            </Button>
          </nav>
        )}
      </div>
    </div>
  );
}

/** 一页流水的列表 */
function LedgerRows({ data }: { data: LedgerPageDto }) {
  return (
    <ul className="divide-border divide-y">
      {data.items.map((item) => {
        const Icon = TYPE_ICON[item.type] ?? Receipt;
        const amount = ledgerAmount(item);
        const subtitle = ledgerSubtitle(item);
        return (
          <li key={item.id} className="flex items-center gap-3 py-2.5">
            <span className="bg-muted text-muted-foreground grid size-8 shrink-0 place-items-center rounded-lg">
              <Icon className="size-4" />
            </span>
            <div className="min-w-0 flex-1">
              <div className="text-sm">{LEDGER_TYPE_LABEL[item.type] ?? item.type}</div>
              {subtitle.text && (
                <div
                  className="text-muted-foreground truncate text-xs"
                  title={subtitle.note ? subtitle.text : undefined}
                >
                  {subtitle.text}
                </div>
              )}
            </div>
            <time
              dateTime={item.createdAt}
              className="text-muted-foreground hidden shrink-0 text-xs tabular-nums sm:block"
            >
              {formatLedgerTime(item.createdAt)}
            </time>
            <span
              className={cn(
                "w-20 shrink-0 text-right text-sm font-semibold tabular-nums",
                TONE_CLASS[amount.tone],
              )}
            >
              {amount.text}
            </span>
          </li>
        );
      })}
    </ul>
  );
}

/**
 * 积分 tab（设计 §6.5）：余额卡 + 积分流水（类型筛选 + 页码分页）。
 * 页码同步到 ?page=；切换筛选或每页条数回第 1 页；翻页时保留当前页并变淡；
 * 失败保留当前页，在分页条旁「加载失败，重试」；页码超出范围（流水被清理）时跳到末页。
 */
export function CreditsTab() {
  const credits = useCreditsStore((state) => state.credits);
  const refreshCredits = useCreditsStore((state) => state.refresh);
  const [params, setParams] = useSearchParams();
  const page = parsePageParam(params.get("page"));
  const [filter, setFilter] = useState<LedgerFilter>("all");
  const [pageSize, setPageSize] = useState<LedgerPageSize>(DEFAULT_LEDGER_PAGE_SIZE);
  const [data, setData] = useState<LedgerPageDto | null>(null);
  /** 重试计数：变了就重新请求同一页 */
  const [attempt, setAttempt] = useState(0);
  /** 当前参数对应的请求标识；与最近一次落定的不同，就是在加载中 */
  const requestKey = `${filter}:${page}:${pageSize}:${attempt}`;
  const [settled, setSettled] = useState<{ key: string; error: boolean }>({
    key: "",
    error: false,
  });
  const loading = settled.key !== requestKey;
  const error = !loading && settled.error;
  const cardRef = useRef<HTMLElement>(null);

  /** 改页码：第 1 页时去掉参数，其余参数（tab）保留 */
  const setPage = useCallback(
    (next: number, replace = false) => {
      setParams(
        (prev) => {
          const out = new URLSearchParams(prev);
          if (next <= 1) out.delete("page");
          else out.set("page", String(next));
          return out;
        },
        { replace },
      );
    },
    [setParams],
  );

  useEffect(() => {
    /** 参数又变了（快速翻页）时丢掉旧响应 */
    let stale = false;
    getMeLedger({ type: filter, page, pageSize })
      .then((result) => {
        if (stale) return;
        const last = lastPageOf(result.total, pageSize);
        if (result.items.length === 0 && page > last) {
          setPage(last, true);
          return;
        }
        setData(result);
        setSettled({ key: requestKey, error: false });
        const card = cardRef.current;
        if (card && card.getBoundingClientRect().top < HEADER_OFFSET) {
          card.scrollIntoView({ block: "start" });
        }
      })
      .catch(() => {
        if (!stale) setSettled({ key: requestKey, error: true });
      });
    return () => {
      stale = true;
    };
  }, [filter, page, pageSize, requestKey, setPage]);

  useEffect(() => {
    void refreshCredits();
  }, [refreshCredits]);

  const retry = () => setAttempt((value) => value + 1);

  const changeFilter = (next: LedgerFilter) => {
    if (next === filter) return;
    setFilter(next);
    setPage(1);
  };

  const changePageSize = (next: LedgerPageSize) => {
    if (next === pageSize) return;
    setPageSize(next);
    setPage(1);
  };

  let body;
  if (!data && loading) {
    body = (
      <div className="grid gap-2.5 py-2" aria-busy>
        {Array.from({ length: 5 }, (_, i) => (
          <Skeleton key={i} className="h-9 w-full" />
        ))}
      </div>
    );
  } else if (!data && error) {
    body = (
      <div className="text-muted-foreground flex items-center gap-2 py-8 text-sm">
        积分流水加载失败
        <Button variant="outline" size="sm" onClick={retry}>
          <RotateCcw />
          重试
        </Button>
      </div>
    );
  } else if (data && data.items.length === 0 && !loading) {
    body = (
      <div className="text-muted-foreground grid place-items-center gap-2 py-10 text-sm">
        <Inbox className="size-6" />
        还没有积分记录
      </div>
    );
  } else if (data) {
    body = (
      <>
        <div
          aria-busy={loading}
          style={DIM_TRANSITION}
          className={cn("overflow-x-auto", loading && "opacity-50")}
        >
          <LedgerRows data={data} />
        </div>
        <Pager
          total={data.total}
          page={page}
          pageSize={pageSize}
          loading={loading}
          error={error}
          onPage={(next) => setPage(next)}
          onPageSize={changePageSize}
          onRetry={retry}
        />
      </>
    );
  }

  return (
    <div className="grid gap-3">
      <div className="grid grid-cols-3 gap-3">
        <BalanceCard label="可用" value={credits?.available} hint="可以立即用于生成" />
        <BalanceCard label="余额" value={credits?.balance} hint="含冻结部分" />
        <BalanceCard label="冻结" value={credits?.frozen} hint="进行中任务占用" />
      </div>
      <section
        ref={cardRef}
        className="bg-card ring-foreground/10 scroll-mt-16 rounded-xl px-4 pt-4 pb-3 ring-1"
      >
        <div className="flex flex-wrap items-center gap-2 pb-2">
          <h2 className="text-sm font-semibold">积分流水</h2>
          <Segmented<LedgerFilter>
            variant="track"
            label="流水类型"
            className="ml-auto"
            value={filter}
            onChange={changeFilter}
            options={FILTERS}
          />
        </div>
        {body}
      </section>
    </div>
  );
}

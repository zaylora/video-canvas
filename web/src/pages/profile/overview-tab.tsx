import { useEffect, useState, type ReactNode } from "react";
import NumberFlow from "@number-flow/react";
import { RotateCcw } from "lucide-react";

import { getMeActivity, getMeStats } from "@/api/me";
import type { ActivityDto, MeStatsDto } from "@/api/me/type";
import { Segmented } from "@/components/home/segmented";
import { ActivityHeatmap } from "@/components/profile/activity-heatmap";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { DURATION, EASE_OUT_CSS, ms } from "@/lib/motion";
import { cn } from "@/lib/utils";
import { useCreditsStore } from "@/store/credits";
import { successRate } from "@/utils/profile/profile-rules";

/** 加载中旧内容变淡的过渡：只动 opacity */
const DIM_TRANSITION = { transition: `opacity ${ms(DURATION.base)} ${EASE_OUT_CSS}` };

/** 年份分段控件里「最近一年」的值 */
const RECENT = "recent";

/**
 * 一张统计卡片
 * @param label 标题
 * @param value 主数字；null 表示没有数据，显示「—」
 * @param hint 副文案
 */
function StatCard({
  label,
  value,
  hint,
}: {
  label: string;
  value: ReactNode | null;
  hint?: ReactNode;
}) {
  return (
    <div className="bg-card ring-foreground/10 grid gap-1 rounded-xl p-4 ring-1">
      <span className="text-muted-foreground text-xs">{label}</span>
      <span className="text-2xl font-semibold tracking-tight tabular-nums">{value ?? "—"}</span>
      <span className="text-muted-foreground min-h-4 text-xs tabular-nums">{hint}</span>
    </div>
  );
}

/** 4 张统计卡片：累计生成、成功率、画布、累计消耗积分（设计 §6.3） */
function StatCards() {
  const [stats, setStats] = useState<MeStatsDto | null>(null);
  /** 重试计数：变了就重新请求 */
  const [attempt, setAttempt] = useState(0);
  /** 最近一次落定的请求：第几次、是否失败；与 attempt 不同就是在加载中 */
  const [settled, setSettled] = useState<{ attempt: number; error: boolean } | null>(null);
  const credits = useCreditsStore((s) => s.credits);
  const refreshCredits = useCreditsStore((s) => s.refresh);
  const state =
    settled?.attempt !== attempt ? "loading" : settled.error ? "error" : ("ready" as const);

  useEffect(() => {
    let stale = false;
    getMeStats()
      .then((next) => {
        if (stale) return;
        setStats(next);
        setSettled({ attempt, error: false });
      })
      .catch(() => {
        if (!stale) setSettled({ attempt, error: true });
      });
    return () => {
      stale = true;
    };
  }, [attempt]);

  useEffect(() => {
    void refreshCredits();
  }, [refreshCredits]);

  if (state === "loading" && !stats) {
    return (
      <div className="grid grid-cols-2 gap-3 lg:grid-cols-4" aria-busy>
        {Array.from({ length: 4 }, (_, i) => (
          <div key={i} className="bg-card ring-foreground/10 grid gap-2.5 rounded-xl p-4 ring-1">
            <Skeleton className="h-3 w-1/2" />
            <Skeleton className="h-7 w-2/3" />
            <Skeleton className="h-3 w-3/4" />
          </div>
        ))}
      </div>
    );
  }

  const failed = state === "error";
  const s = failed ? null : stats;
  const rate = s ? successRate(s) : null;
  return (
    <div className="grid gap-2">
      <div className="grid grid-cols-2 gap-3 lg:grid-cols-4">
        <StatCard
          label="累计生成"
          value={s ? <NumberFlow value={s.total} /> : null}
          hint={s && `近 7 天 ${s.last7d} 次`}
        />
        <StatCard label="成功率" value={rate} hint={s && `成功 ${s.success} · 失败 ${s.failed}`} />
        <StatCard
          label="画布"
          value={s ? <NumberFlow value={s.canvasCount} /> : null}
          hint={s && "未删除的画布数"}
        />
        <StatCard
          label="累计消耗积分"
          value={s ? <NumberFlow value={s.spentCredits} /> : null}
          hint={s && credits && `当前可用 ${credits.available.toLocaleString()}`}
        />
      </div>
      {failed && (
        <div className="text-muted-foreground flex items-center gap-2 text-xs">
          统计加载失败
          <Button variant="outline" size="xs" onClick={() => setAttempt((n) => n + 1)}>
            <RotateCcw />
            重试
          </Button>
        </div>
      )}
    </div>
  );
}

/** 热力图卡片：年份切换（最近一年 + 注册年份到今年），切换时旧图变淡，失败不影响统计卡片 */
function ActivityCard() {
  const [year, setYear] = useState<number | null>(null);
  const [data, setData] = useState<ActivityDto | null>(null);
  const [shownYear, setShownYear] = useState<number | null>(null);
  /** 重试计数：变了就重新请求当前年份 */
  const [attempt, setAttempt] = useState(0);
  const requestKey = `${year ?? RECENT}:${attempt}`;
  /** 最近一次落定的请求；与 requestKey 不同就是在加载中（旧图保留并变淡） */
  const [settled, setSettled] = useState<{ key: string; error: boolean }>({
    key: "",
    error: false,
  });
  const loading = settled.key !== requestKey;
  const error = !loading && settled.error;

  useEffect(() => {
    /** 快速切年份时旧响应不覆盖新选择 */
    let stale = false;
    getMeActivity(year)
      .then((next) => {
        if (stale) return;
        setData(next);
        setShownYear(year);
        setSettled({ key: requestKey, error: false });
      })
      .catch(() => {
        if (!stale) setSettled({ key: requestKey, error: true });
      });
    return () => {
      stale = true;
    };
  }, [year, requestKey]);

  const years = [...(data?.years ?? [])].sort((a, b) => b - a);
  const options = [
    { value: RECENT, label: "最近一年" },
    ...years.map((item) => ({ value: String(item), label: String(item) })),
  ];

  return (
    <section className="bg-card ring-foreground/10 grid gap-3 rounded-xl p-4 ring-1">
      <div className="flex flex-wrap items-center gap-x-3 gap-y-2">
        <h2 className="text-sm font-semibold">生成活跃度</h2>
        {data && (
          <Segmented<string>
            variant="track"
            label="选择年份"
            className="ml-auto"
            value={year === null ? RECENT : String(year)}
            onChange={(value) => setYear(value === RECENT ? null : Number(value))}
            options={options}
          />
        )}
      </div>
      {error ? (
        <div className="text-muted-foreground flex items-center gap-2 py-6 text-sm">
          热力图加载失败
          <Button variant="outline" size="sm" onClick={() => setAttempt((n) => n + 1)}>
            <RotateCcw />
            重试
          </Button>
        </div>
      ) : data ? (
        <div aria-busy={loading} style={DIM_TRANSITION} className={cn(loading && "opacity-50")}>
          <ActivityHeatmap data={data} year={shownYear} />
        </div>
      ) : (
        <Skeleton className="h-[136px] w-full" aria-busy />
      )}
    </section>
  );
}

/** 概览 tab：统计卡片 + 活跃热力图 */
export function OverviewTab() {
  return (
    <div className="grid gap-3">
      <StatCards />
      <ActivityCard />
    </div>
  );
}

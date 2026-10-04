import { BadgeCheck, Clapperboard, Image as ImageIcon, Loader2, Music, Type } from "lucide-react";
import { useCallback, useEffect, useRef, useState, type ReactNode } from "react";

import { listUserLedger, listUserLogins, listUserTasks } from "@/api/admin-users";
import type {
  CursorPage,
  LedgerFilter,
  LoginFilter,
  TaskFilter,
  UserDetail,
  UserLedgerItem,
  UserLoginItem,
  UserTaskItem,
} from "@/api/admin-users/type.d";
import { CopyButton } from "@/components/admin-ui/copy-button";
import {
  DescriptionDetails,
  DescriptionItem,
  DescriptionList,
  DescriptionTerm,
} from "@/components/admin-ui/description-list";
import { Segmented, SegmentedItem } from "@/components/admin-ui/segmented";
import { Tag } from "@/components/admin-ui/tag";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { cn } from "@/lib/utils";
import { formatShortTime } from "@/utils/time";
import {
  AUDIT_ACTION_LABEL,
  auditNote,
  formatAbsolute,
  formatDurationMs,
  formatRelative,
  LEDGER_TYPE_LABEL,
  loginResultView,
  parseUserAgent,
  taskStatusView,
} from "@/utils/admin/user-rules";

/** 每次加载的条数（游标分页） */
const PAGE_LIMIT = 20;

type ListLoad = "loading" | "ready" | "error";

/**
 * 游标分页列表：首屏加载、「加载更多」追加、失败可重试。
 * key 变化（换用户、换筛选、外部刷新）就从头加载，旧请求的结果会被丢弃。
 * @param key 请求身份，变化时重新加载
 * @param load 取一页；cursor 为 null 表示第一页
 */
function useCursorList<T>(key: string, load: (cursor: string | null) => Promise<CursorPage<T>>) {
  const [items, setItems] = useState<T[]>([]);
  const [cursor, setCursor] = useState<string | null>(null);
  const [state, setState] = useState<ListLoad>("loading");
  const [loadingMore, setLoadingMore] = useState(false);
  const [attempt, setAttempt] = useState(0);
  const seq = useRef(0);
  const loadRef = useRef(load);
  useEffect(() => {
    loadRef.current = load;
  });

  useEffect(() => {
    const mine = ++seq.current;
    setState("loading");
    setItems([]);
    setCursor(null);
    loadRef
      .current(null)
      .then((page) => {
        if (mine !== seq.current) return;
        setItems(page.items);
        setCursor(page.next_cursor);
        setState("ready");
      })
      .catch(() => {
        // 全局 toast 已弹；页签里给「加载失败 · 重试」
        if (mine === seq.current) setState("error");
      });
  }, [key, attempt]);

  const more = useCallback(() => {
    if (!cursor || loadingMore) return;
    const mine = seq.current;
    setLoadingMore(true);
    loadRef
      .current(cursor)
      .then((page) => {
        if (mine !== seq.current) return;
        setItems((prev) => [...prev, ...page.items]);
        setCursor(page.next_cursor);
      })
      .catch(() => undefined)
      .finally(() => setLoadingMore(false));
  }, [cursor, loadingMore]);

  return {
    items,
    state,
    hasMore: cursor !== null,
    loadingMore,
    more,
    retry: () => setAttempt((n) => n + 1),
  };
}

/** 页签内的筛选条：分段 + 右侧计数，吸顶 */
function FilterBar<V extends string>({
  slideId,
  label,
  value,
  options,
  onChange,
  count,
}: {
  slideId: string;
  label: string;
  value: V;
  options: { value: V; label: string }[];
  onChange: (value: V) => void;
  count: ReactNode;
}) {
  return (
    <div className="bg-background sticky top-0 z-10 flex items-center justify-between py-3">
      <Segmented aria-label={label}>
        {options.map((option) => (
          <SegmentedItem
            key={option.value}
            slideId={slideId}
            active={value === option.value}
            className="px-2 py-1"
            onClick={() => onChange(option.value)}
          >
            {option.label}
          </SegmentedItem>
        ))}
      </Segmented>
      <span className="text-muted-foreground text-xs tabular-nums">{count}</span>
    </div>
  );
}

/** 列表区域的统一状态：加载骨架 5 行、失败重试、空文案、加载更多 */
function ListBody<T>({
  list,
  empty,
  children,
}: {
  list: ReturnType<typeof useCursorList<T>>;
  empty: string;
  children: ReactNode;
}) {
  if (list.state === "loading") {
    return (
      <div className="flex flex-col gap-3 py-2" aria-busy>
        {Array.from({ length: 5 }, (_, i) => (
          <Skeleton key={i} className="h-8 w-full" />
        ))}
      </div>
    );
  }
  if (list.state === "error") {
    return (
      <div className="text-muted-foreground flex items-center justify-center gap-2 py-12 text-sm">
        加载失败 ·
        <Button variant="link" size="xs" className="px-0" onClick={list.retry}>
          重试
        </Button>
      </div>
    );
  }
  if (list.items.length === 0) {
    return <div className="text-muted-foreground py-12 text-center text-sm">{empty}</div>;
  }
  return (
    <>
      {children}
      <div className="pt-3 text-center">
        {list.hasMore ? (
          <Button variant="ghost" size="xs" disabled={list.loadingMore} onClick={list.more}>
            {list.loadingMore && <Loader2 className="animate-spin" />}
            加载更多
          </Button>
        ) : (
          <span className="text-muted-foreground text-xs">没有更多了</span>
        )}
      </div>
    </>
  );
}

const KIND_ICON: Record<string, typeof Clapperboard> = {
  video: Clapperboard,
  image: ImageIcon,
  audio: Music,
  text: Type,
};

const TASK_FILTERS: { value: TaskFilter; label: string }[] = [
  { value: "all", label: "全部" },
  { value: "success", label: "成功" },
  { value: "failed", label: "失败" },
  { value: "running", label: "进行中" },
];

/** 生成页签：时间 · 类型图标 + 模型 · 状态 · 扣费 · 耗时；失败行可展开看错误，任务 ID 可复制 */
function TasksTab({ userId, refreshKey }: { userId: number; refreshKey: number }) {
  const [filter, setFilter] = useState<TaskFilter>("all");
  const list = useCursorList<UserTaskItem>(`${userId}:${filter}:${refreshKey}`, (cursor) =>
    listUserTasks(userId, { status: filter, cursor, limit: PAGE_LIMIT }),
  );
  return (
    <>
      <FilterBar
        slideId="user-tasks-filter"
        label="生成记录筛选"
        value={filter}
        options={TASK_FILTERS}
        onChange={setFilter}
        count={list.state === "ready" ? `已加载 ${list.items.length} 条` : ""}
      />
      <ListBody list={list} empty={filter === "all" ? "还没有生成记录" : "没有符合条件的记录"}>
        {list.items.map((task) => (
          <TaskRow key={task.id} task={task} />
        ))}
      </ListBody>
    </>
  );
}

function TaskRow({ task }: { task: UserTaskItem }) {
  const view = taskStatusView(task.status);
  const Icon = KIND_ICON[task.kind] ?? Clapperboard;
  const cost = view.running
    ? `冻结 ${task.credits}`
    : task.charged_credits > 0
      ? `-${task.charged_credits}`
      : "0";
  return (
    <div className="grid grid-cols-[5.5rem_1fr_auto_4rem_3rem] items-center gap-x-3 border-b py-2.5 text-[13px] last:border-b-0">
      <span className="text-muted-foreground text-xs tabular-nums">
        {formatShortTime(task.created_at)}
      </span>
      <div className="min-w-0">
        <span className="flex items-center gap-1.5 truncate">
          <Icon className="text-muted-foreground size-3.5 shrink-0" />
          {task.model_name || task.model_key}
        </span>
        {task.error && (
          <details className="mt-0.5 text-xs">
            <summary className="text-destructive cursor-pointer truncate">{task.error}</summary>
            <div className="text-muted-foreground mt-1 flex items-center gap-1 tabular-nums">
              任务 #{task.id}
              <CopyButton text={String(task.id)} label="复制任务 ID" iconOnly />
            </div>
          </details>
        )}
      </div>
      <Tag tone={view.tone}>
        {view.running && (
          <span className="bg-status-running size-1.5 animate-pulse rounded-full motion-reduce:animate-none" />
        )}
        {view.label}
      </Tag>
      <span
        className={cn(
          "text-right font-mono tabular-nums",
          task.charged_credits === 0 && !view.running && "text-muted-foreground",
        )}
      >
        {cost}
      </span>
      <span className="text-muted-foreground text-right text-xs tabular-nums">
        {formatDurationMs(task.duration_ms)}
      </span>
    </div>
  );
}

const LEDGER_FILTERS: { value: LedgerFilter; label: string }[] = [
  { value: "all", label: "全部" },
  { value: "admin", label: "管理员调整" },
  { value: "task", label: "任务" },
];

/** 积分页签：类型 Tag · 变动（正绿负红带正负号）· 说明 · 操作人 · 时间 */
function CreditsTab({ userId, refreshKey }: { userId: number; refreshKey: number }) {
  const [filter, setFilter] = useState<LedgerFilter>("all");
  const list = useCursorList<UserLedgerItem>(`${userId}:${filter}:${refreshKey}`, (cursor) =>
    listUserLedger(userId, { type: filter, cursor, limit: PAGE_LIMIT }),
  );
  return (
    <>
      <FilterBar
        slideId="user-credits-filter"
        label="积分流水筛选"
        value={filter}
        options={LEDGER_FILTERS}
        onChange={setFilter}
        count={list.state === "ready" ? `已加载 ${list.items.length} 条` : ""}
      />
      <ListBody list={list} empty="没有符合条件的流水">
        {list.items.map((row) => (
          <div
            key={row.id}
            className="grid grid-cols-[6.5rem_3.5rem_1fr_auto] items-center gap-x-3 border-b py-2.5 text-[13px] last:border-b-0"
          >
            <span>
              <Tag tone={row.type === "admin_adjust" ? "warning" : "neutral"}>
                {LEDGER_TYPE_LABEL[row.type] ?? row.type}
              </Tag>
            </span>
            <span
              className={cn(
                "text-right font-mono font-medium tabular-nums",
                row.amount > 0 && "text-status-success",
                row.amount < 0 && "text-destructive",
                row.amount === 0 && "text-muted-foreground",
              )}
            >
              {row.amount > 0 ? "+" : ""}
              {row.amount}
            </span>
            <span className="min-w-0 truncate">
              {row.note || (row.task_id ? `任务 #${row.task_id}` : "—")}
              {row.operator_name && (
                <span className="text-muted-foreground ml-1.5 text-xs">· {row.operator_name}</span>
              )}
            </span>
            <span className="text-muted-foreground text-xs tabular-nums">
              {formatShortTime(row.created_at)}
            </span>
          </div>
        ))}
      </ListBody>
    </>
  );
}

const LOGIN_FILTERS: { value: LoginFilter; label: string }[] = [
  { value: "all", label: "全部" },
  { value: "fail", label: "失败" },
];

/** 登录页签：时间 · 结果 · IP · 设备（UA 解析成「Chrome · macOS」） */
function LoginsTab({ userId, refreshKey }: { userId: number; refreshKey: number }) {
  const [filter, setFilter] = useState<LoginFilter>("all");
  const list = useCursorList<UserLoginItem>(`${userId}:${filter}:${refreshKey}`, (cursor) =>
    listUserLogins(userId, { result: filter, cursor, limit: PAGE_LIMIT }),
  );
  return (
    <>
      <FilterBar
        slideId="user-logins-filter"
        label="登录记录筛选"
        value={filter}
        options={LOGIN_FILTERS}
        onChange={setFilter}
        count={list.state === "ready" ? `已加载 ${list.items.length} 条` : ""}
      />
      <ListBody list={list} empty={filter === "all" ? "还没有登录记录" : "没有失败的登录"}>
        {list.items.map((row) => {
          const view = loginResultView(row.kind, row.result);
          return (
            <div
              key={row.id}
              className="grid grid-cols-[5.5rem_5.5rem_1fr_auto] items-center gap-x-3 border-b py-2.5 text-[13px] last:border-b-0"
            >
              <span className="text-muted-foreground text-xs tabular-nums">
                {formatShortTime(row.created_at)}
              </span>
              <span>
                <Tag tone={view.tone}>{view.label}</Tag>
              </span>
              <span className="min-w-0 truncate font-mono text-xs tabular-nums">
                {row.ip || "—"}
              </span>
              <span className="text-muted-foreground text-xs">
                {parseUserAgent(row.user_agent)}
              </span>
            </div>
          );
        })}
      </ListBody>
    </>
  );
}

/** 概览页签：信息格 + 最近 3 条管理员操作；详情还在加载时显示骨架 */
function OverviewTab({
  detail,
  onGotoCredits,
}: {
  detail: UserDetail | null;
  onGotoCredits: () => void;
}) {
  const [lastIp, setLastIp] = useState<string | null>(null);
  const id = detail?.id;

  /** 最近一次成功登录的 IP：详情接口不带，取登录记录里最新一条登录成功的 */
  useEffect(() => {
    if (id === undefined) return;
    let alive = true;
    setLastIp(null);
    listUserLogins(id, { result: "all", limit: 10 })
      .then((page) => {
        if (!alive) return;
        const hit = page.items.find((row) => row.kind === "login" && row.result === "ok");
        setLastIp(hit?.ip ?? null);
      })
      .catch(() => undefined);
    return () => {
      alive = false;
    };
  }, [id]);

  if (!detail) {
    return (
      <div className="grid grid-cols-2 gap-x-6 gap-y-5 pt-4" aria-busy>
        {Array.from({ length: 8 }, (_, i) => (
          <div key={i} className="flex flex-col gap-1.5">
            <Skeleton className="h-3 w-16" />
            <Skeleton className="h-4 w-28" />
          </div>
        ))}
      </div>
    );
  }

  const finished = detail.task_success + detail.task_failed;
  const rate = finished > 0 ? `${Math.round((detail.task_success / finished) * 100)}%` : "—";

  return (
    <div className="pt-4">
      <DescriptionList className="grid-cols-2 md:grid-cols-2 2xl:grid-cols-2">
        <DescriptionItem>
          <DescriptionTerm>注册时间</DescriptionTerm>
          <DescriptionDetails className="tabular-nums">
            {formatAbsolute(detail.created_at)}
          </DescriptionDetails>
        </DescriptionItem>
        <DescriptionItem>
          <DescriptionTerm>邮箱验证</DescriptionTerm>
          <DescriptionDetails>
            {detail.email_verified_at ? (
              <>
                已验证 <BadgeCheck className="text-status-success size-4" />
              </>
            ) : (
              <span className="text-muted-foreground">未验证</span>
            )}
          </DescriptionDetails>
        </DescriptionItem>
        <DescriptionItem>
          <DescriptionTerm>最近登录</DescriptionTerm>
          <DescriptionDetails>
            <span title={formatAbsolute(detail.last_login_at)}>
              {formatRelative(detail.last_login_at)}
            </span>
            {lastIp && (
              <span className="text-muted-foreground font-mono text-xs font-normal tabular-nums">
                {lastIp}
              </span>
            )}
          </DescriptionDetails>
        </DescriptionItem>
        <DescriptionItem>
          <DescriptionTerm>生成总数</DescriptionTerm>
          <DescriptionDetails className="tabular-nums">{detail.task_total}</DescriptionDetails>
        </DescriptionItem>
        <DescriptionItem>
          <DescriptionTerm>成功率</DescriptionTerm>
          <DescriptionDetails className="tabular-nums">{rate}</DescriptionDetails>
        </DescriptionItem>
        <DescriptionItem>
          <DescriptionTerm>累计消耗积分</DescriptionTerm>
          <DescriptionDetails className="tabular-nums">{detail.spent_credits}</DescriptionDetails>
        </DescriptionItem>
        <DescriptionItem>
          <DescriptionTerm>近 7 天生成</DescriptionTerm>
          <DescriptionDetails className="tabular-nums">{detail.tasks_last_7d}</DescriptionDetails>
        </DescriptionItem>
        <DescriptionItem>
          <DescriptionTerm>并发上限</DescriptionTerm>
          <DescriptionDetails className="tabular-nums">
            {detail.effective_max_active_tasks}
            <span className="text-muted-foreground text-xs font-normal">
              {detail.max_active_tasks === null ? "全局默认" : "自定义"}
            </span>
          </DescriptionDetails>
        </DescriptionItem>
      </DescriptionList>

      <div className="mt-6 mb-2 flex items-center justify-between">
        <span className="text-sm font-medium">最近管理员操作</span>
        <Button variant="ghost" size="xs" onClick={onGotoCredits}>
          查看全部
        </Button>
      </div>
      {detail.recent_audits.length > 0 ? (
        <div className="rounded-lg border">
          {detail.recent_audits.slice(0, 3).map((audit, index) => {
            const note = auditNote(audit.detail_json);
            return (
              <div
                key={`${audit.created_at}:${index}`}
                className="flex items-center justify-between gap-3 border-b px-3 py-2 text-xs last:border-b-0"
              >
                <span className="min-w-0 truncate">
                  {audit.actor_name} {AUDIT_ACTION_LABEL[audit.action] ?? audit.action}
                  {note && <span className="text-muted-foreground">：{note}</span>}
                </span>
                <span
                  className="text-muted-foreground shrink-0"
                  title={formatAbsolute(audit.created_at)}
                >
                  {formatRelative(audit.created_at)}
                </span>
              </div>
            );
          })}
        </div>
      ) : (
        <div className="text-muted-foreground rounded-lg border border-dashed px-3 py-4 text-center text-xs">
          还没有管理员操作过这个账号
        </div>
      )}
    </div>
  );
}

export { CreditsTab, LoginsTab, OverviewTab, TasksTab };

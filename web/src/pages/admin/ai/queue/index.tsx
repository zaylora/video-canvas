import { ExternalLink, Loader2, RefreshCw } from "lucide-react";
import { MotionConfig } from "motion/react";
import { useMemo, useState } from "react";
import { toast } from "sonner";

import {
  createRiverTicket,
  pauseQueue,
  resumeQueue,
  saveQueueConcurrency,
} from "@/api/admin/queue";
import type { QueueInfo, QueueName } from "@/api/admin/queue/type";
import { confirm } from "@/components/admin-ui/confirm-dialog";
import { LoadFailed } from "@/components/admin-ui/load-failed";
import {
  PageHeader,
  PageHeaderActions,
  PageHeaderHeading,
  PageHeaderTitle,
} from "@/components/admin-ui/page-header";
import { ReasonTooltip } from "@/components/admin-ui/reason-tooltip";
import { Reveal } from "@/components/admin-ui/reveal";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { cn } from "@/lib/utils";
import { useAdminStore } from "@/store/admin";
import { errorMessage } from "@/utils/admin/errors";
import {
  appliedText,
  dropDraft,
  isQueueIdle,
  PAUSE_CONFIRM_TEXT,
  queueWriteDenyReason,
  riverUiDenyReason,
  saveFailedText,
  setDraft,
  shownConcurrency,
  validateConcurrency,
  type ConcurrencyDrafts,
} from "@/utils/admin/queue";
import { canManageInfra } from "@/utils/admin/role";

import { ReadOnlyNotice } from "../../shared";
import { useAliveRef } from "../../use-admin";
import { ChannelOccupancy } from "./channel-occupancy";
import { InstanceTable } from "./instance-table";
import { QueueCard } from "./queue-card";
import { ThroughputCard } from "./throughput-card";
import { useQueueData } from "./use-queue-data";

/** 页面骨架：三张队列卡 + 两块表格的位置 */
function QueueSkeleton() {
  return (
    <div className="grid gap-3" aria-busy="true" aria-label="加载中">
      <div className="grid gap-3 lg:grid-cols-3">
        <Skeleton className="h-52" />
        <Skeleton className="h-52" />
        <Skeleton className="h-52" />
      </div>
      <Skeleton className="h-40" />
      <Skeleton className="h-32" />
    </div>
  );
}

/**
 * 后台「任务队列」页（/admin/ai/queue）：看生成任务在各个队列里的积压、渠道名额占用、实例是否健康，
 * 并调整队列并发、暂停 / 恢复队列。设计稿见 docs/design/任务队列River。
 * - 读接口 admin 及以上；改并发、暂停 / 恢复、打开 River UI 仅 super_admin，其他角色这些控件禁用并说明原因；
 * - 页面可见时每 5 秒静默刷新，隐藏时暂停；滑杆草稿与服务端值分开保存，刷新不会覆盖正在拖动的滑杆；
 * - 界面只显示队列的中文标签，不出现内部队列名；
 * - 单用户并发、排队倍数、排队超时不在这一页（在「注册设置」「用户管理」里），页面上也不放提示。
 * 请求错误的全局提示由拦截器弹，这里只处理状态与就地提示。
 */
export default function QueuePage() {
  const role = useAdminStore((state) => state.role);
  const canWrite = canManageInfra(role);
  const writeDeny = queueWriteDenyReason(canWrite);
  const riverDeny = riverUiDenyReason(canWrite);
  const aliveRef = useAliveRef();
  const { overview, throughput, patchQueue, reloadAll } = useQueueData();

  const [drafts, setDrafts] = useState<ConcurrencyDrafts>({});
  const [saving, setSaving] = useState<Partial<Record<QueueName, boolean>>>({});
  const [toggling, setToggling] = useState<Partial<Record<QueueName, boolean>>>({});
  const [errors, setErrors] = useState<Partial<Record<QueueName, string | null>>>({});
  const [applied, setApplied] = useState<Partial<Record<QueueName, string | null>>>({});
  const [riverBusy, setRiverBusy] = useState(false);
  const [refreshing, setRefreshing] = useState(false);

  const data = overview.data;
  const idle = useMemo(() => (data ? isQueueIdle(data) : false), [data]);

  /** 保存某个队列的并发；失败时回到服务端值，并在卡片里写明原因 */
  const save = async (queue: QueueInfo) => {
    const value = shownConcurrency(drafts[queue.name], queue.concurrency);
    const invalid = validateConcurrency(value, queue.hard_cap);
    setApplied((prev) => ({ ...prev, [queue.name]: null }));
    if (invalid) {
      setErrors((prev) => ({ ...prev, [queue.name]: saveFailedText(invalid, queue.concurrency) }));
      setDrafts((prev) => dropDraft(prev, queue.name));
      return;
    }
    setErrors((prev) => ({ ...prev, [queue.name]: null }));
    setSaving((prev) => ({ ...prev, [queue.name]: true }));
    try {
      const result = await saveQueueConcurrency(queue.name, value);
      if (!aliveRef.current) return;
      patchQueue(queue.name, { concurrency: result.concurrency });
      setDrafts((prev) => dropDraft(prev, queue.name));
      setApplied((prev) => ({
        ...prev,
        [queue.name]: appliedText(result.applied_instances, result.total_instances),
      }));
      void overview.refreshSilently();
    } catch (cause) {
      if (!aliveRef.current) return;
      setDrafts((prev) => dropDraft(prev, queue.name));
      setErrors((prev) => ({
        ...prev,
        [queue.name]: saveFailedText(errorMessage(cause, "请求失败"), queue.concurrency),
      }));
    } finally {
      if (aliveRef.current) setSaving((prev) => ({ ...prev, [queue.name]: false }));
    }
  };

  /** 暂停前先确认（排队时间继续累计）；恢复直接执行 */
  const togglePause = async (queue: QueueInfo) => {
    if (!queue.paused) {
      await confirm({
        title: `暂停「${queue.label}」？`,
        description: PAUSE_CONFIRM_TEXT,
        confirmLabel: "暂停",
        destructive: true,
        onConfirm: async () => {
          const result = await pauseQueue(queue.name);
          patchQueue(queue.name, { paused: result.paused });
          toast.warning(`「${queue.label}」已暂停（对所有实例生效）`);
          void overview.refreshSilently();
        },
      });
      return;
    }
    setToggling((prev) => ({ ...prev, [queue.name]: true }));
    try {
      const result = await resumeQueue(queue.name);
      if (!aliveRef.current) return;
      patchQueue(queue.name, { paused: result.paused });
      toast.success(`「${queue.label}」已恢复`);
      void overview.refreshSilently();
    } catch {
      // 全局 toast 已弹，按钮状态由刷新校准
    } finally {
      if (aliveRef.current) setToggling((prev) => ({ ...prev, [queue.name]: false }));
    }
  };

  /** 换取 ticket 后在新标签页打开 River UI（同源相对地址） */
  const openRiver = async () => {
    setRiverBusy(true);
    try {
      const { url } = await createRiverTicket();
      const opened = window.open(url, "_blank");
      if (opened) opened.opener = null;
      else toast.error("浏览器拦截了新标签页，请允许弹窗后重试");
    } catch {
      // 全局 toast 已弹
    } finally {
      if (aliveRef.current) setRiverBusy(false);
    }
  };

  /** 手动刷新两份数据 */
  const refresh = async () => {
    setRefreshing(true);
    try {
      await reloadAll();
    } finally {
      if (aliveRef.current) setRefreshing(false);
    }
  };

  return (
    <MotionConfig reducedMotion="user">
      <div className="h-full overflow-y-auto">
        <main className="px-4 py-6 lg:px-6">
          <PageHeader>
            <PageHeaderHeading>
              <PageHeaderTitle>任务队列</PageHeaderTitle>
              <p
                className={cn(
                  "text-muted-foreground mt-1 flex items-center gap-1.5 text-xs",
                  overview.stale && "text-amber-600 dark:text-amber-400",
                )}
              >
                <span
                  aria-hidden
                  className={cn(
                    "size-1.5 rounded-full",
                    overview.stale ? "bg-amber-500" : "bg-status-success",
                  )}
                />
                {overview.stale ? "刷新失败，显示的是旧数据" : "页面可见时每 5 秒自动刷新"}
              </p>
            </PageHeaderHeading>
            <PageHeaderActions>
              <Tooltip>
                <TooltipTrigger
                  render={
                    <Button
                      variant="outline"
                      size="icon"
                      aria-label="刷新"
                      disabled={refreshing}
                      onClick={() => void refresh()}
                    />
                  }
                >
                  <RefreshCw className={cn(refreshing && "animate-spin")} />
                </TooltipTrigger>
                <TooltipContent>刷新</TooltipContent>
              </Tooltip>
              <ReasonTooltip reason={riverDeny}>
                <Button
                  variant="outline"
                  disabled={riverDeny !== null || riverBusy}
                  onClick={() => void openRiver()}
                >
                  {riverBusy ? <Loader2 className="animate-spin" /> : <ExternalLink />}
                  打开 River UI
                </Button>
              </ReasonTooltip>
            </PageHeaderActions>
          </PageHeader>

          {!canWrite && (
            <ReadOnlyNotice className="mb-4" what="修改队列并发、暂停或恢复队列、打开 River UI" />
          )}

          {overview.status === "loading" && <QueueSkeleton />}
          {overview.status === "error" && !data && (
            <LoadFailed onRetry={() => void overview.reload()} />
          )}
          {data && (
            <div className="grid gap-3">
              <Reveal>
                <section aria-label="队列" className="grid gap-3 lg:grid-cols-3">
                  {data.queues.map((queue) => (
                    <QueueCard
                      key={queue.name}
                      queue={queue}
                      draft={drafts[queue.name]}
                      saving={!!saving[queue.name]}
                      toggling={!!toggling[queue.name]}
                      denyReason={writeDeny}
                      error={errors[queue.name] ?? null}
                      applied={applied[queue.name] ?? null}
                      onDraft={(value) => {
                        setErrors((prev) => ({ ...prev, [queue.name]: null }));
                        setApplied((prev) => ({ ...prev, [queue.name]: null }));
                        setDrafts((prev) => setDraft(prev, queue.name, value, queue.concurrency));
                      }}
                      onSave={() => void save(queue)}
                      onReset={() => setDrafts((prev) => dropDraft(prev, queue.name))}
                      onTogglePause={() => void togglePause(queue)}
                    />
                  ))}
                </section>
              </Reveal>

              <Reveal index={1}>
                <ChannelOccupancy channels={data.channels} idle={idle} />
              </Reveal>

              <Reveal index={2}>
                <ThroughputCard
                  points={throughput.data}
                  status={throughput.status}
                  stale={throughput.stale}
                  onRetry={() => void throughput.reload()}
                />
              </Reveal>

              <Reveal index={3}>
                <InstanceTable
                  instances={data.instances}
                  queues={data.queues}
                  now={overview.fetchedAt}
                />
              </Reveal>
            </div>
          )}
        </main>
      </div>
    </MotionConfig>
  );
}

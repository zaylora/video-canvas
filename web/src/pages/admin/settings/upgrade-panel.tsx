import { useCallback, useEffect, useRef, useState } from "react";
import { Check, X } from "lucide-react";
import { motion, useReducedMotion } from "motion/react";
import { toast } from "sonner";

import { getUpgradeInfo } from "@/api/admin/system";
import type { UpgradeInfo } from "@/api/admin/system/type.d";
import { Notice } from "@/components/admin-ui/notice";
import { Button } from "@/components/ui/button";
import { Card, CardAction, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { DURATION, EASE_OUT, SPRING } from "@/lib/motion";
import { cn } from "@/lib/utils";
import { useSystemUpdateStore } from "@/store/system-update";
import { formatDateTime } from "@/utils/admin/agent-skill";
import { UPGRADE_STEPS, formatBytes, isRunFinished, runProgress } from "@/utils/admin/upgrade";

/** 升级中轮询升级状态的间隔（毫秒） */
const POLL_MS = 2000;
/** 升级成功后多久自动刷新页面（秒）：新版本的前端要重新加载 */
const RELOAD_AFTER_S = 2;

/** 这一次发起的升级或回退 */
export interface UpgradeRun {
  /** 升级还是回退，只影响标题 */
  kind: "upgrade" | "rollback";
  /** 目标版本 */
  target: string;
  /** 发起时间（毫秒时间戳），用来区分上一次运行留下的终态 */
  startedAt: number;
}

/**
 * 跟踪一次升级：每 2 秒静默请求升级状态，直到这次运行到达终态。
 * 升级中 backend 会被重建，请求失败是正常的，此时标记 reconnecting，不弹错误提示。
 * 页面刷新时如果 updater 还在跑，从它报告的目标版本接着跟踪。
 * @param info 页面已加载的升级面板数据
 * @param setInfo 轮询拿到新数据后更新页面
 * @returns 当前运行、是否在重连、开始跟踪与关闭结果的方法
 */
export function useUpgradeRun(info: UpgradeInfo | null, setInfo: (next: UpgradeInfo) => void) {
  const [run, setRun] = useState<UpgradeRun | null>(null);
  const [reconnecting, setReconnecting] = useState(false);
  const [finished, setFinished] = useState(false);
  const setUpgrading = useSystemUpdateStore((state) => state.setUpgrading);
  const resumed = useRef(false);

  /** 进页面时 updater 正在跑：接着跟踪（只做一次） */
  useEffect(() => {
    if (resumed.current || !info?.running || !info.last) return;
    resumed.current = true;
    setRun({ kind: "upgrade", target: info.last.target, startedAt: 0 });
  }, [info]);

  useEffect(() => {
    setUpgrading(!!run && !finished);
    return () => setUpgrading(false);
  }, [run, finished, setUpgrading]);

  useEffect(() => {
    if (!run || finished) return;
    let stopped = false;
    const tick = async () => {
      try {
        const next = await getUpgradeInfo(true);
        if (stopped) return;
        setReconnecting(false);
        setInfo(next);
        if (isRunFinished(next, run.target, run.startedAt)) {
          setFinished(true);
          if (next.last?.state === "succeeded") {
            toast.success(`已${run.kind === "rollback" ? "回退" : "升级"}到 v${run.target}`);
            window.setTimeout(() => window.location.reload(), RELOAD_AFTER_S * 1000);
          }
        }
      } catch {
        if (!stopped) setReconnecting(true);
      }
    };
    const timer = window.setInterval(() => void tick(), POLL_MS);
    void tick();
    return () => {
      stopped = true;
      window.clearInterval(timer);
    };
  }, [run, finished, setInfo]);

  const start = useCallback((next: UpgradeRun) => {
    setFinished(false);
    setReconnecting(false);
    setRun(next);
  }, []);

  /** 关掉失败的结果卡片 */
  const dismiss = useCallback(() => {
    setRun(null);
    setFinished(false);
  }, []);

  return { run, reconnecting, finished, start, dismiss };
}

/**
 * 升级进度卡：五步 + 进度条，backend 重启期间显示重连提示，结束后显示结局。
 * @param run 这一次运行
 * @param info 最新的升级面板数据
 * @param reconnecting 最近一次轮询失败（backend 正在重启）
 * @param finished 这次运行已到终态
 * @param current 发起时的版本，用在「已退回 vX」里
 * @param onDismiss 关掉失败的结果
 */
export function UpgradeProgressCard({
  run,
  info,
  reconnecting,
  finished,
  current,
  onDismiss,
}: {
  run: UpgradeRun;
  info: UpgradeInfo | null;
  reconnecting: boolean;
  finished: boolean;
  current: string;
  onDismiss: () => void;
}) {
  const reduceMotion = useReducedMotion();
  const last = info?.last;
  /** 还没轮询到这次运行的记录时，停在第一步 */
  const ours =
    last && last.target === run.target && Date.parse(last.updated_at) >= run.startedAt - 2000;
  const progress = ours ? runProgress(last) : null;
  const step = progress?.step ?? 0;
  const outcome = finished ? progress?.outcome : "running";
  const percent = outcome === "succeeded" ? 100 : Math.round((step / UPGRADE_STEPS.length) * 100);
  const verb = run.kind === "rollback" ? "回退" : "升级";

  return (
    <motion.div
      initial={reduceMotion ? { opacity: 0 } : { opacity: 0, y: 4 }}
      animate={{ opacity: 1, y: 0 }}
      transition={{ duration: DURATION.base, ease: EASE_OUT }}
    >
      <Card aria-live="polite">
        <CardHeader>
          <CardTitle>
            {verb}到 <span className="font-mono tabular-nums">v{run.target}</span>
          </CardTitle>
          {finished && outcome !== "succeeded" && (
            <CardAction>
              <Button variant="ghost" size="sm" onClick={onDismiss}>
                关闭
              </Button>
            </CardAction>
          )}
        </CardHeader>
        <CardContent className="flex flex-col gap-3">
          <div className="bg-muted h-1 overflow-hidden rounded-full">
            <motion.div
              className="bg-status-running h-full origin-left"
              animate={{ scaleX: percent / 100 }}
              initial={false}
              transition={SPRING}
            />
          </div>
          <ol className="flex flex-col">
            {UPGRADE_STEPS.map((name, i) => {
              const failed = progress?.failedAt === i && (finished || progress.rollingBack);
              const done = i < step || (i === step && outcome === "succeeded");
              const active = i === step && !done && !failed && !finished;
              return (
                <li
                  key={name}
                  className={cn(
                    "flex items-center gap-2.5 py-1.5 text-sm",
                    !done && !active && !failed && "text-muted-foreground",
                  )}
                >
                  <StepIcon done={done} active={active} failed={failed} />
                  <span>{name}</span>
                  {i === 2 && done && last?.backup && (
                    <span className="text-muted-foreground ml-auto truncate font-mono text-xs">
                      {last.backup}
                    </span>
                  )}
                </li>
              );
            })}
          </ol>
          <ProgressNotice
            outcome={outcome}
            reconnecting={reconnecting || !!progress?.restarting}
            rollingBack={!!progress?.rollingBack}
            message={last?.message ?? ""}
            target={run.target}
            current={current}
            verb={verb}
          />
          {finished && outcome !== "succeeded" && info?.log_tail && (
            <details className="text-xs">
              <summary className="text-muted-foreground hover:text-foreground cursor-pointer">
                查看运行输出
              </summary>
              <pre className="bg-muted mt-2 max-h-64 overflow-auto rounded-lg p-3 font-mono leading-relaxed">
                {info.log_tail}
              </pre>
            </details>
          )}
        </CardContent>
      </Card>
    </motion.div>
  );
}

/**
 * 步骤前的状态图标：完成打勾、进行中转圈、失败打叉、未开始空心圈
 * @param done 已完成
 * @param active 进行中
 * @param failed 停在这一步失败
 */
function StepIcon({ done, active, failed }: { done: boolean; active: boolean; failed: boolean }) {
  if (failed) {
    return (
      <span className="bg-destructive grid size-4.5 shrink-0 place-items-center rounded-full text-white">
        <X className="size-3" strokeWidth={3} />
      </span>
    );
  }
  if (done) {
    return (
      <span className="bg-status-success grid size-4.5 shrink-0 place-items-center rounded-full text-white">
        <Check className="size-3" strokeWidth={3} />
      </span>
    );
  }
  if (active) {
    return (
      <span
        aria-label="进行中"
        className="border-status-running size-4.5 shrink-0 animate-spin rounded-full border-2 border-t-transparent motion-reduce:animate-none"
      />
    );
  }
  return <span className="border-border size-4.5 shrink-0 rounded-full border-[1.5px]" />;
}

/**
 * 进度卡底部的提示：进行中提示重连，结束后按结局给出说明
 * @param outcome 结局；还在跑为 running
 * @param reconnecting backend 正在重启
 * @param rollingBack 正在退回旧版本
 * @param message update.sh 给出的说明（失败时含处理建议）
 * @param target 目标版本
 * @param current 发起时的版本
 * @param verb 升级 / 回退
 */
function ProgressNotice({
  outcome,
  reconnecting,
  rollingBack,
  message,
  target,
  current,
  verb,
}: {
  outcome: string | undefined;
  reconnecting: boolean;
  rollingBack: boolean;
  message: string;
  target: string;
  current: string;
  verb: string;
}) {
  if (rollingBack) {
    return (
      <Notice tone="warning" title="新版本启动失败">
        正在退回 v{current}…
      </Notice>
    );
  }
  switch (outcome) {
    case "succeeded":
      return (
        <Notice tone="success" title={`${verb}完成`}>
          已运行 v{target}，{RELOAD_AFTER_S} 秒后自动刷新页面。
        </Notice>
      );
    case "failed":
      return (
        <Notice tone="warning" title={`${verb}失败，服务未改动`}>
          {message}
        </Notice>
      );
    case "failed_rolled_back":
      return (
        <Notice tone="warning" title={`${verb}失败，已退回 v${current}`}>
          {message}。服务已恢复正常，可以展开下方的运行输出查看原因。
        </Notice>
      );
    case "failed_needs_attention":
      return (
        <Notice tone="danger" title="需要人工处理">
          {message}
        </Notice>
      );
    default:
      return reconnecting ? (
        <Notice tone="info" title="服务重启中">
          后台会短暂无法访问，页面正在自动重连，不要关闭。
        </Notice>
      ) : null;
  }
}

/**
 * 升级记录与备份：上次一键升级的结果、数据库备份列表、回退入口。只读，不提供下载和恢复。
 * @param info 升级面板数据
 * @param current 当前版本
 * @param rollbackBlock 不能回退的原因（如不是 super_admin）；可以回退为 null
 * @param onRollback 点「回退到 vX」
 */
export function UpgradeHistoryCard({
  info,
  current,
  rollbackBlock,
  onRollback,
}: {
  info: UpgradeInfo;
  current: string;
  rollbackBlock: string | null;
  onRollback: () => void;
}) {
  const last = info.last;
  return (
    <Card>
      <CardHeader>
        <CardTitle>升级记录与备份</CardTitle>
        {info.rollback_target && (
          <CardAction>
            <Button
              variant="outline"
              size="sm"
              className="text-destructive"
              disabled={!!rollbackBlock}
              title={rollbackBlock ?? undefined}
              onClick={onRollback}
            >
              回退到 v{info.rollback_target}
            </Button>
          </CardAction>
        )}
      </CardHeader>
      <CardContent className="flex flex-col gap-3 text-sm">
        <p className="text-muted-foreground">
          {last
            ? `上次一键升级：v${last.previous} → v${last.target}，${formatDateTime(last.updated_at)}，${STATE_LABEL[last.state] ?? last.state}`
            : `还没有通过后台升级过，当前 v${current}`}
        </p>
        {info.backups.length > 0 ? (
          <div className="overflow-x-auto">
            <table className="w-full text-xs">
              <thead className="text-muted-foreground">
                <tr className="border-b">
                  <th className="py-2 text-left font-medium">数据库备份（保留最近 3 份）</th>
                  <th className="py-2 text-right font-medium">大小</th>
                  <th className="py-2 text-right font-medium">时间</th>
                </tr>
              </thead>
              <tbody>
                {info.backups.map((b) => (
                  <tr key={b.name} className="border-b last:border-0">
                    <td className="py-2 font-mono">backups/{b.name}</td>
                    <td className="py-2 text-right font-mono tabular-nums">
                      {formatBytes(b.size)}
                    </td>
                    <td className="py-2 text-right font-mono tabular-nums">
                      {formatDateTime(b.mod_time)}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        ) : (
          <p className="text-muted-foreground">还没有备份，每次升级前会自动备份数据库。</p>
        )}
        <p className="text-muted-foreground text-xs">
          备份在服务器的部署目录里；恢复会丢掉备份之后写入的数据，请按部署文档在服务器上手动执行。
        </p>
      </CardContent>
    </Card>
  );
}

/** 上次运行结局的文字 */
const STATE_LABEL: Record<string, string> = {
  running: "进行中",
  succeeded: "成功",
  failed: "失败（服务未改动）",
  failed_rolled_back: "失败，已退回",
  failed_needs_attention: "失败，需要人工处理",
};

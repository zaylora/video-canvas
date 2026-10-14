import { useEffect, useState } from "react";
import { ExternalLink, RefreshCw } from "lucide-react";
import { motion } from "motion/react";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";

import {
  getMigrations,
  getSystemVersion,
  getUpgradeInfo,
  startRollback,
  startUpgrade,
} from "@/api/admin/system";
import type {
  MigrationFile,
  SystemVersion,
  UpdateCheck,
  UpgradeInfo,
} from "@/api/admin/system/type.d";
import { confirm } from "@/components/admin-ui/confirm-dialog";
import {
  CopyBlock,
  CopyBlockCode,
  CopyBlockHeader,
  CopyBlockTitle,
} from "@/components/admin-ui/copy-block";
import { CopyButton } from "@/components/admin-ui/copy-button";
import { Notice } from "@/components/admin-ui/notice";
import {
  PageHeader,
  PageHeaderActions,
  PageHeaderHeading,
  PageHeaderTitle,
} from "@/components/admin-ui/page-header";
import { Tag } from "@/components/admin-ui/tag";
import { Button } from "@/components/ui/button";
import { Card, CardAction, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { SPRING } from "@/lib/motion";
import { cn } from "@/lib/utils";
import { useAdminStore } from "@/store/admin";
import { useSystemUpdateStore } from "@/store/system-update";
import { formatDateTime } from "@/utils/admin/agent-skill";
import { canManageInfra } from "@/utils/admin/role";
import { manualUpgradeCommands, upgradeBlockReason } from "@/utils/admin/upgrade";
import { formatRelative } from "@/utils/admin/user-rules";

import { MD_COMPONENTS } from "../agent-skills/file-preview";
import { SettingsError } from "./settings-state";
import { UpgradeHistoryCard, UpgradeProgressCard, useUpgradeRun } from "./upgrade-panel";

/**
 * 「系统设置 → 版本与升级」（设计稿 docs/design/自动更新设计 4.2、5.9 节）：
 * 当前版本、新版本与更新日志、升级（一键或手动）、数据库迁移、升级记录与备份。
 */
export default function VersionSettingsPage() {
  const role = useAdminStore((state) => state.role);
  const isSuper = canManageInfra(role);
  const check = useSystemUpdateStore((state) => state.check);
  const checking = useSystemUpdateStore((state) => state.checking);
  const loadCheck = useSystemUpdateStore((state) => state.load);

  const [version, setVersion] = useState<SystemVersion | null>(null);
  const [migrations, setMigrations] = useState<MigrationFile[] | null>(null);
  const [info, setInfo] = useState<UpgradeInfo | null>(null);
  const [failed, setFailed] = useState(false);

  /** 加载失败后点重试：加 1 触发重新加载 */
  const [attempt, setAttempt] = useState(0);

  useEffect(() => {
    void loadCheck();
  }, [loadCheck]);

  useEffect(() => {
    let alive = true;
    Promise.all([getSystemVersion(), getMigrations(), getUpgradeInfo()])
      .then(([v, m, u]) => {
        if (!alive) return;
        setVersion(v);
        setMigrations(m);
        setInfo(u);
      })
      .catch(() => {
        if (alive) setFailed(true);
      });
    return () => {
      alive = false;
    };
  }, [attempt]);

  const { run, reconnecting, finished, start, dismiss } = useUpgradeRun(info, setInfo);
  const current = check?.current ?? version?.version ?? "";
  const hasUpdate = !!check?.has_update && !!check.latest;
  const block = upgradeBlockReason(info, isSuper);
  const busy = !!run && !finished;

  const upgrade = async () => {
    if (!check?.latest) return;
    const target = check.latest;
    /** 确认前再取一次：进行中的任务数要尽量新 */
    const fresh = await getUpgradeInfo().catch(() => info);
    if (fresh) setInfo(fresh);
    await confirm({
      title: `升级到 v${target}`,
      confirmLabel: "开始升级",
      description: (
        <ul className="list-disc space-y-1 pl-5">
          <li>先备份数据库，再拉取镜像并重建服务</li>
          <li>服务会中断约 1 分钟，期间后台和画布都无法访问</li>
          <li>失败会自动退回 v{current}</li>
        </ul>
      ),
      children: <ActiveTasksNotice count={fresh?.active_tasks ?? 0} />,
      blockReason: upgradeBlockReason(fresh, isSuper),
      onConfirm: async () => {
        await startUpgrade(target);
        start({ kind: "upgrade", target, startedAt: Date.now() });
      },
    });
  };

  const rollback = async () => {
    if (!info?.rollback_target) return;
    const target = info.rollback_target;
    await confirm({
      title: `回退到 v${target}`,
      confirmLabel: "开始回退",
      destructive: true,
      description: (
        <ul className="list-disc space-y-1 pl-5">
          <li>先备份数据库，再换回 v{target} 的镜像并重建服务</li>
          <li>服务会中断约 1 分钟</li>
        </ul>
      ),
      children:
        info.current_migrations.length > 0 ? (
          <Notice tone="warning" title="数据库变更不会撤销">
            v{current} 执行过 {info.current_migrations.length} 个迁移：
            <span className="font-mono">{info.current_migrations.join("、")}</span>
            。如果其中有删除或改名，v{target} 可能无法启动，届时需要用备份恢复。
          </Notice>
        ) : (
          <ActiveTasksNotice count={info.active_tasks} />
        ),
      blockReason: upgradeBlockReason(info, isSuper),
      onConfirm: async () => {
        await startRollback(target);
        start({ kind: "rollback", target, startedAt: Date.now() });
      },
    });
  };

  return (
    <div className="flex flex-col gap-4">
      <PageHeader>
        <PageHeaderHeading>
          <PageHeaderTitle>版本与升级</PageHeaderTitle>
        </PageHeaderHeading>
        <PageHeaderActions>
          <Button
            variant="outline"
            size="sm"
            disabled={checking || busy}
            onClick={() => void loadCheck(true)}
          >
            <RefreshCw className={cn(checking && "animate-spin motion-reduce:animate-none")} />
            {checking ? "检查中…" : "重新检查"}
          </Button>
        </PageHeaderActions>
      </PageHeader>

      {failed && !version ? (
        <SettingsError
          onRetry={() => {
            setFailed(false);
            setAttempt((n) => n + 1);
          }}
        />
      ) : (
        <>
          <VersionCard
            version={version}
            check={check}
            info={info}
            showUpgrade={hasUpdate && !busy && !!info?.available}
            block={block}
            onUpgrade={() => void upgrade()}
          />
          {run && (
            <UpgradeProgressCard
              run={run}
              info={info}
              reconnecting={reconnecting}
              finished={finished}
              current={current}
              onDismiss={dismiss}
            />
          )}
          {hasUpdate && check && <ChangelogCard check={check} />}
          {hasUpdate && check?.latest && info && (!info.available || info.uses_latest) && (
            <ManualStepsCard
              current={current}
              target={check.latest}
              usesLatest={info.uses_latest}
            />
          )}
          <MigrationsCard files={migrations} />
          {info?.available && (
            <UpgradeHistoryCard
              info={info}
              current={current}
              rollbackBlock={busy ? "正在升级" : block}
              onRollback={() => void rollback()}
            />
          )}
        </>
      )}
    </div>
  );
}

/**
 * 升级会中断进行中的生成任务：有任务时提示数量，没有时什么都不显示
 * @param count 全站进行中的生成任务数
 */
function ActiveTasksNotice({ count }: { count: number }) {
  if (count <= 0) return null;
  return (
    <Notice tone="warning" title={`${count} 个生成任务进行中`}>
      会被中断，服务恢复后自动对账，不会重复扣积分。
    </Notice>
  );
}

/**
 * 版本状态卡：左边当前版本与构建信息，右边检查结果与升级按钮
 * @param version 当前版本；加载中为 null
 * @param check 新版本检查结果；加载中为 null
 * @param info 升级面板数据
 * @param showUpgrade 是否显示升级按钮（有新版本、updater 可用、没在升级）
 * @param block 不能升级的原因，按钮禁用并显示；可以升级为 null
 * @param onUpgrade 点升级
 */
function VersionCard({
  version,
  check,
  info,
  showUpgrade,
  block,
  onUpgrade,
}: {
  version: SystemVersion | null;
  check: UpdateCheck | null;
  info: UpgradeInfo | null;
  showUpgrade: boolean;
  block: string | null;
  onUpgrade: () => void;
}) {
  return (
    <Card>
      <CardContent className="flex flex-col gap-4">
        <div className="flex flex-col justify-between gap-4 sm:flex-row">
          <div className="min-w-0">
            <div className="text-muted-foreground text-xs">当前版本</div>
            {version ? (
              <>
                <div className="font-mono text-3xl font-semibold tabular-nums">
                  {version.version === "dev" ? "dev" : `v${version.version}`}
                </div>
                <div className="text-muted-foreground mt-1 font-mono text-xs break-all">
                  {version.git_commit.slice(0, 7)} · 构建于 {formatDateTime(version.build_time)}
                  {version.latest_migration && ` · 数据库已迁移到 ${version.latest_migration}`}
                </div>
              </>
            ) : (
              <Skeleton className="mt-1 h-9 w-32" />
            )}
          </div>
          <div className="flex flex-col items-start gap-1.5 text-xs sm:items-end sm:text-right">
            <CheckState check={check} />
            {showUpgrade && check?.latest && (
              <>
                <Button
                  size="sm"
                  className="mt-1"
                  disabled={!!block}
                  title={block ?? undefined}
                  onClick={onUpgrade}
                >
                  升级到 v{check.latest}
                </Button>
                {block && <span className="text-muted-foreground">{block}</span>}
              </>
            )}
            {info?.uses_latest && check?.has_update && (
              <span className="text-muted-foreground">
                部署使用 latest，不能一键升级，见下方说明
              </span>
            )}
          </div>
        </div>
        {check?.warning && (
          <Notice tone="warning" title="检查更新失败">
            {check.warning}
            {check.latest ? "。下面显示的是上次的结果" : ""}。5 分钟后可再试，或检查服务器能否访问
            api.github.com。
          </Notice>
        )}
      </CardContent>
    </Card>
  );
}

/**
 * 检查结果的标签与时间
 * @param check 新版本检查结果；加载中为 null
 */
function CheckState({ check }: { check: UpdateCheck | null }) {
  if (!check) return <Skeleton className="h-6 w-28" />;
  if (!check.enabled) {
    return (
      <>
        <Tag>检查更新已关闭</Tag>
        <span className="text-muted-foreground">
          配置项 <span className="font-mono">update.check_enabled</span>
        </span>
      </>
    );
  }
  if (check.dev_build) return <Tag>开发构建，不检查更新</Tag>;
  if (!check.latest) return <Tag>未能检查</Tag>;
  const checked = check.checked_at ? `上次检查 ${formatRelative(check.checked_at)}` : "";
  if (!check.has_update) {
    return (
      <>
        <Tag tone="success">已是最新版本</Tag>
        {checked && <span className="text-muted-foreground">{checked}</span>}
      </>
    );
  }
  return (
    <>
      <Tag tone="info" mono>
        v{check.latest} 可升级
      </Tag>
      {check.published_at && (
        <span className="text-muted-foreground">发布于 {formatDateTime(check.published_at)}</span>
      )}
      {checked && <span className="text-muted-foreground">{checked}</span>}
    </>
  );
}

/**
 * 更新日志：GitHub Release 正文；超过 320px 时收起，可展开
 * @param check 有新版本的检查结果
 */
function ChangelogCard({ check }: { check: UpdateCheck }) {
  const [open, setOpen] = useState(false);
  return (
    <Card>
      <CardHeader>
        <CardTitle>
          更新日志 <span className="font-mono">v{check.latest}</span>
        </CardTitle>
        {check.release_url && (
          <CardAction>
            <a
              href={check.release_url}
              target="_blank"
              rel="noreferrer noopener"
              className="text-muted-foreground hover:text-foreground inline-flex items-center gap-1 text-xs"
            >
              在 GitHub 查看 <ExternalLink className="size-3" />
            </a>
          </CardAction>
        )}
      </CardHeader>
      <CardContent>
        <motion.div
          layout
          transition={SPRING}
          className={cn(
            "relative overflow-hidden text-sm leading-relaxed",
            !open &&
              "max-h-80 after:pointer-events-none after:absolute after:inset-x-0 after:bottom-0 after:h-16 after:bg-gradient-to-t after:from-card",
          )}
        >
          {check.release_notes ? (
            <ReactMarkdown remarkPlugins={[remarkGfm]} components={MD_COMPONENTS}>
              {check.release_notes}
            </ReactMarkdown>
          ) : (
            <p className="text-muted-foreground">这个版本没有填写更新日志。</p>
          )}
        </motion.div>
        <Button
          variant="ghost"
          size="sm"
          className="text-muted-foreground mt-1 -ml-2"
          onClick={() => setOpen((v) => !v)}
        >
          {open ? "收起" : "展开全部"}
        </Button>
      </CardContent>
    </Card>
  );
}

/**
 * 手动升级步骤：没启用一键升级、或部署用 latest 时显示
 * @param current 当前版本
 * @param target 目标版本
 * @param usesLatest 部署用的是 latest
 */
function ManualStepsCard({
  current,
  target,
  usesLatest,
}: {
  current: string;
  target: string;
  usesLatest: boolean;
}) {
  return (
    <Card>
      <CardHeader>
        <CardTitle>升级步骤</CardTitle>
      </CardHeader>
      <CardContent className="flex flex-col gap-3">
        <Notice tone="warning" title="先备份">
          数据库迁移只向前，没有自动回滚。新版本启动时会自动迁移；出了问题需要用备份恢复。
        </Notice>
        <ol className="flex flex-col gap-3">
          {manualUpgradeCommands(current, target).map((step, i) => (
            <li key={step.title} className="grid grid-cols-[1.5rem_1fr] gap-2.5">
              <span className="bg-muted grid size-6 place-items-center rounded-full font-mono text-xs">
                {i + 1}
              </span>
              <CopyBlock>
                <CopyBlockHeader>
                  <CopyBlockTitle>{step.title}</CopyBlockTitle>
                  <CopyButton text={step.command} />
                </CopyBlockHeader>
                <CopyBlockCode>{step.command}</CopyBlockCode>
              </CopyBlock>
            </li>
          ))}
        </ol>
        {usesLatest ? (
          <Notice tone="warning" title="无法一键升级">
            部署目录 .env 里是 <span className="font-mono">IMAGE_TAG=latest</span>
            ：无法确认版本，失败也无法回退。按上面第 2 步改成具体版本号后即可使用一键升级。
          </Notice>
        ) : (
          <Notice tone="info" title="想在这里一键升级？">
            在部署目录的 .env 加一行 <span className="font-mono">UPDATER_TOKEN=随机字符串</span>
            ，执行 <span className="font-mono">docker compose up -d</span> 后再执行{" "}
            <span className="font-mono">docker compose --profile updater up -d</span>
            。updater 会挂载 Docker，等同服务器 root 权限，按需启用。
          </Notice>
        )}
        <p className="text-muted-foreground text-xs">
          也可以在部署目录执行 <span className="font-mono">./update.sh --tag {target}</span>
          ，它会自动完成以上三步并在失败时回退。
        </p>
      </CardContent>
    </Card>
  );
}

/** 迁移文件状态 → 标签 */
function migrationTag(f: MigrationFile) {
  if (f.unknown) return <Tag tone="warning">库里有、程序里没有</Tag>;
  if (f.modified) return <Tag tone="danger">已执行，但文件被改过</Tag>;
  if (f.applied) return <Tag tone="success">已执行</Tag>;
  return <Tag>待执行</Tag>;
}

/**
 * 数据库迁移：一行摘要 + 可展开的明细；有异常（被改过、库比程序新）时默认展开
 * @param files 迁移文件状态；加载中为 null
 */
function MigrationsCard({ files }: { files: MigrationFile[] | null }) {
  const abnormal = !!files?.some((f) => f.modified || f.unknown);
  const [open, setOpen] = useState(false);
  const shown = open || abnormal;
  const applied = files?.filter((f) => f.applied && !f.unknown) ?? [];
  const pending = files?.filter((f) => !f.applied).length ?? 0;
  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center gap-2">
          数据库迁移
          {files?.some((f) => f.unknown) && <Tag tone="warning">库比程序新</Tag>}
          {files?.some((f) => f.modified) && <Tag tone="danger">文件被改过</Tag>}
        </CardTitle>
        {files && files.length > 0 && !abnormal && (
          <CardAction>
            <Button
              variant="ghost"
              size="sm"
              className="text-muted-foreground"
              onClick={() => setOpen((v) => !v)}
            >
              {open ? "收起明细" : "展开明细"}
            </Button>
          </CardAction>
        )}
      </CardHeader>
      <CardContent className="text-sm">
        {!files ? (
          <Skeleton className="h-5 w-64" />
        ) : (
          <p className="text-muted-foreground">
            {applied.length > 0 ? (
              <>
                已执行到 <span className="font-mono">{applied[applied.length - 1].filename}</span>{" "}
                ·{" "}
              </>
            ) : null}
            共 {files.length} 个{pending > 0 ? `，${pending} 个待执行（升级后启动时自动执行）` : ""}
          </p>
        )}
        {files && shown && (
          <div className="mt-3 overflow-x-auto">
            <table className="w-full text-xs">
              <thead className="text-muted-foreground">
                <tr className="border-b">
                  <th className="py-2 text-left font-medium">文件</th>
                  <th className="py-2 text-left font-medium">状态</th>
                  <th className="py-2 text-left font-medium">执行时间</th>
                  <th className="py-2 text-left font-medium">执行版本</th>
                </tr>
              </thead>
              <tbody>
                {files.map((f) => (
                  <tr key={f.filename} className="border-b last:border-0">
                    <td className="py-2 font-mono">{f.filename}</td>
                    <td className="py-2">{migrationTag(f)}</td>
                    <td className="py-2 font-mono tabular-nums">
                      {f.applied_at ? formatDateTime(f.applied_at) : "—"}
                    </td>
                    <td className="py-2 font-mono">{f.app_version || "—"}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </CardContent>
    </Card>
  );
}

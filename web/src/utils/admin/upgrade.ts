import type { UpgradeInfo, UpgradeRunStatus, UpgradeState } from "@/api/admin/system/type.d";

/** 手动升级的一步：说明 + 可复制的命令 */
export interface ManualStep {
  /** 这一步做什么 */
  title: string;
  /** 在服务器部署目录执行的命令（或 .env 里的一行） */
  command: string;
}

/**
 * 没启用一键升级时给管理员的手动步骤：备份 → 改 IMAGE_TAG → 拉取并重建
 * @param current 当前版本，用在备份文件名里
 * @param target 目标版本
 * @returns 三步，每步一条可复制的命令
 */
export function manualUpgradeCommands(current: string, target: string): ManualStep[] {
  return [
    {
      title: "备份数据库",
      command: `docker compose exec -T postgres pg_dump -U postgres -Fc video_canvas > backup-${current}-$(date +%Y%m%d%H%M).dump`,
    },
    { title: `把部署目录 .env 里的版本改为 ${target}`, command: `IMAGE_TAG=${target}` },
    { title: "拉取镜像并重建", command: "docker compose pull && docker compose up -d" },
  ];
}

/** 界面上的升级步骤，与 update.sh 的阶段一一对应 */
export const UPGRADE_STEPS = [
  "准备",
  "拉取镜像",
  "备份数据库",
  "重建服务（新版本启动时自动迁移数据库）",
  "等待新版本就绪",
] as const;

/** update.sh 的阶段 → 界面第几步；rollback 发生在重建之后，停在最后一步 */
const PHASE_STEP: Record<string, number> = {
  prepare: 0,
  compose: 0,
  pull: 1,
  backup: 2,
  recreate: 3,
  health: 4,
  rollback: 4,
  finish: 4,
};

/** 一次运行在界面上的样子 */
export interface RunProgress {
  /** 正在进行（或停在）第几步 */
  step: number;
  /** 结局；还在跑为 running */
  outcome: UpgradeState;
  /** 失败停在第几步；没失败为 null */
  failedAt: number | null;
  /** 新版本没起来，正在退回旧版本 */
  rollingBack: boolean;
  /** 处在 backend 被重建的阶段：请求会失败，界面显示重连提示 */
  restarting: boolean;
}

/**
 * 把 update.sh 的进度翻译成界面状态
 * @param last updater 报告的最近一次运行
 * @returns 第几步、结局、是否在回退、是否处在 backend 重启阶段
 */
export function runProgress(last: UpgradeRunStatus): RunProgress {
  const step = PHASE_STEP[last.phase] ?? 0;
  const outcome = last.state;
  const rollingBack = last.phase === "rollback" && outcome === "running";
  const failed = outcome !== "running" && outcome !== "succeeded";
  return {
    step,
    outcome,
    failedAt: failed || rollingBack ? step : null,
    rollingBack,
    restarting: outcome === "running" && step >= 3,
  };
}

/**
 * updater 报告的最近一次记录是不是「这一次」发起的。
 * 只比较服务器自己写的 updated_at 与发起前那条记录是否相同，不拿浏览器时钟比，
 * 避免浏览器和服务器时间有偏差时永远认不出本次运行。
 * @param last updater 报告的最近一次运行
 * @param target 这次的目标版本
 * @param baseline 发起前最近一次记录的 updated_at；当时没有记录为 null
 * @returns 是这次的为 true
 */
export function isOurRun(
  last: UpgradeRunStatus | null | undefined,
  target: string,
  baseline: string | null,
): last is UpgradeRunStatus {
  return !!last && last.target === target && last.updated_at !== baseline;
}

/**
 * 判断「这一次」发起的升级是否已经结束：updater 空闲，且最近一次记录是本次的并已到终态
 * @param info 升级面板数据
 * @param target 这次的目标版本
 * @param baseline 发起前最近一次记录的 updated_at；当时没有记录为 null
 * @returns 已结束为 true
 */
export function isRunFinished(info: UpgradeInfo, target: string, baseline: string | null): boolean {
  const last = info.last;
  if (info.running || !isOurRun(last, target, baseline)) return false;
  return last.state !== "running";
}

/**
 * 不能一键升级的原因；可以升级时返回 null
 * @param info 升级面板数据；还没加载到为 null
 * @param isSuperAdmin 当前是不是 super_admin
 * @returns 原因文案或 null
 */
export function upgradeBlockReason(info: UpgradeInfo | null, isSuperAdmin: boolean): string | null {
  if (!isSuperAdmin) return "仅超级管理员可升级";
  if (!info) return "正在读取升级状态";
  if (!info.available) return info.unavailable_reason || "一键升级未启用";
  if (info.uses_latest) return "部署使用 latest 镜像，无法确认版本也无法回退，不能一键升级";
  if (info.running) return "已有升级在进行中";
  return null;
}

/**
 * 字节数转成人看的大小
 * @param bytes 字节数
 * @returns 如 512 B、161.8 KB、5.0 MB
 */
export function formatBytes(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  const units = ["KB", "MB", "GB"];
  let value = bytes / 1024;
  let unit = 0;
  while (value >= 1024 && unit < units.length - 1) {
    value /= 1024;
    unit += 1;
  }
  return `${value.toFixed(1)} ${units[unit]}`;
}

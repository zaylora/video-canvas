import service from "@/utils/requests/service";
import type { MigrationFile, SystemVersion, UpdateCheck, UpgradeInfo } from "./type";

/**
 * 读取当前程序版本与数据库迁移进度（admin 可读）
 * @param silent 为 true 时失败不弹全局提示：升级中轮询新版本是否已启动时用
 * @returns 版本、commit、构建时间、最后一个已执行的迁移
 */
export const getSystemVersion = (silent = false) =>
  service.get<SystemVersion>("/admin/system/version", undefined, { silent });

/**
 * 检查有没有新版本（admin 可读）。后端缓存 1 小时，检查失败也返回结果，原因在 warning
 * @param force 跳过缓存重新检查（后端 30 秒内复用上次结果）
 * @returns 当前版本、最新版本、更新日志
 */
export const checkSystemUpdate = (force = false) =>
  service.get<UpdateCheck>("/admin/system/updates", force ? { force: 1 } : undefined);

/**
 * 读取每个数据库迁移文件的执行状态（admin 可读）
 * @returns 按文件名排序，库里有、程序里没有的排在最后
 */
export const getMigrations = async () =>
  (await service.get<MigrationFile[] | null>("/admin/system/migrations", undefined)) ?? [];

/**
 * 读取一键升级面板的数据（admin 可读）
 * @param silent 为 true 时失败不弹全局提示：升级中 backend 重启，轮询失败是正常的
 * @returns updater 是否可用、最近一次运行状态、备份、进行中的任务数
 */
export const getUpgradeInfo = async (silent = false, timeout?: number) => {
  const info = await service.get<UpgradeInfo>("/admin/system/upgrade", undefined, {
    silent,
    timeout,
  });
  return {
    ...info,
    backups: info.backups ?? [],
    current_migrations: info.current_migrations ?? [],
  };
};

/**
 * 开始一键升级（super_admin）：只能升到检查到的最新版本；立即返回，进度轮询 getUpgradeInfo
 * @param target 目标版本，如 0.4.0
 */
export const startUpgrade = (target: string) =>
  service.post<{ target: string }>("/admin/system/upgrade", { target });

/**
 * 开始回退（super_admin）：只能退到最近一次一键升级之前的版本；数据库迁移不会撤销
 * @param target 目标版本，即 UpgradeInfo.rollback_target
 */
export const startRollback = (target: string) =>
  service.post<{ target: string }>("/admin/system/rollback", { target });

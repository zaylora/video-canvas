/** 当前程序版本（GET /admin/system/version） */
export interface SystemVersion {
  /** 版本号，如 0.4.0；开发构建为 dev */
  version: string;
  /** 构建时的 git commit */
  git_commit: string;
  /** 构建时间 */
  build_time: string;
  /** Go 版本 */
  go_version: string;
  /** 库里最后一个已执行的迁移文件；还没有迁移记录时为空串 */
  latest_migration: string;
}

/** 检查新版本的结果（GET /admin/system/updates）：失败不报错，原因在 warning */
export interface UpdateCheck {
  /** 是否开启了检查更新（update.check_enabled） */
  enabled: boolean;
  /** 当前是开发构建，不检查 */
  dev_build: boolean;
  /** 当前版本，不带 v */
  current: string;
  /** 最新正式版本，不带 v；没检查到时缺省 */
  latest?: string;
  /** 是否有比当前新的版本 */
  has_update: boolean;
  /** 更新日志（Markdown） */
  release_notes?: string;
  /** GitHub Release 页面 */
  release_url?: string;
  /** 发布时间（ISO） */
  published_at?: string;
  /** 结果实际从 GitHub 取回的时间（ISO） */
  checked_at?: string;
  /** 检查失败的原因；有旧结果时仍返回旧结果 */
  warning?: string;
}

/** 一个数据库迁移文件的执行状态（GET /admin/system/migrations） */
export interface MigrationFile {
  /** 文件名 */
  filename: string;
  /** 是否已执行 */
  applied: boolean;
  /** 执行时间（ISO），未执行时缺省 */
  applied_at?: string;
  /** 执行它的程序版本 */
  app_version?: string;
  /** 执行耗时（毫秒） */
  duration_ms: number;
  /** 已执行，但文件内容和执行时不一样 */
  modified: boolean;
  /** 库里有记录，程序里没有（库比程序新） */
  unknown: boolean;
}

/** update.sh 的运行状态 */
export type UpgradeState =
  | "running"
  | "succeeded"
  /** 没改动运行中的服务（拉取失败、备份失败等） */
  | "failed"
  /** 新版本没起来，已退回旧版本且服务正常 */
  | "failed_rolled_back"
  /** 退回后旧版本也没起来，需要人工处理 */
  | "failed_needs_attention";

/** update.sh 写的进度 */
export interface UpgradeRunStatus {
  /** 目标版本 */
  target: string;
  /** 升级前的版本 */
  previous: string;
  /** 当前阶段：prepare / compose / pull / backup / recreate / health / rollback / finish */
  phase: string;
  /** 运行状态 */
  state: UpgradeState;
  /** 给管理员看的说明；失败时是原因和处理建议 */
  message: string;
  /** 这次升级前的数据库备份路径（服务器上），没有备份时为空串 */
  backup: string;
  /** 最后更新时间（ISO，UTC） */
  updated_at: string;
}

/** 部署目录 backups/ 下的一份数据库备份 */
export interface UpgradeBackup {
  /** 文件名 */
  name: string;
  /** 字节数 */
  size: number;
  /** 修改时间（ISO） */
  mod_time: string;
}

/** 一键升级面板的数据（GET /admin/system/upgrade） */
export interface UpgradeInfo {
  /** updater 已启用且连得上 */
  available: boolean;
  /** 不可用的原因；可用时为空串 */
  unavailable_reason: string;
  /** 部署目录 .env 里的 IMAGE_TAG */
  image_tag: string;
  /** 部署用的是 latest，不能一键升级 */
  uses_latest: boolean;
  /** update.sh 正在执行 */
  running: boolean;
  /** 最近一次运行的状态；从没运行过为 null */
  last: UpgradeRunStatus | null;
  /** 数据库备份，新的在前 */
  backups: UpgradeBackup[];
  /** 最近一次运行输出的最后若干行 */
  log_tail: string;
  /** 全站进行中的生成任务数，升级会中断它们 */
  active_tasks: number;
  /** 可以回退到的版本；不能回退时为空串 */
  rollback_target: string;
  /** 当前版本执行过的数据库迁移文件 */
  current_migrations: string[];
}

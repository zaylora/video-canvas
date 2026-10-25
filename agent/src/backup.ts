/**
 * 在线备份 Agent 的 SQLite（含同一个文件里 Pi Durable 的表）：服务正在运行也能备份，不用停机。
 * 用 VACUUM INTO：读到的是一致的快照，结果是单个自包含的文件（不依赖 -wal）。备份完用 integrity_check 验一遍；
 * 失败不留半截文件。升级前由 update.sh 调用（经 backup-cli.ts），恢复就是停掉 agent、把备份文件换成数据库文件。
 */
import { existsSync, mkdirSync, rmSync, statSync } from "node:fs";
import { dirname } from "node:path";
import { DatabaseSync } from "node:sqlite";

export interface BackupResult {
  path: string;
  bytes: number;
  /** 备份里已应用的迁移条数（升级前后对比用） */
  migrations: number;
  integrity: string;
}

export function backupDatabase(dbPath: string, dest: string): BackupResult {
  if (!existsSync(dbPath)) throw new Error(`数据库文件不存在：${dbPath}（不会凭空创建）`);
  if (existsSync(dest)) throw new Error(`备份文件已存在：${dest}`);
  mkdirSync(dirname(dest), { recursive: true });
  try {
    const db = new DatabaseSync(dbPath);
    try {
      db.exec("PRAGMA busy_timeout = 5000"); // 服务正在写：等一会儿，而不是立刻 SQLITE_BUSY
      db.prepare("VACUUM INTO ?").run(dest);
    } finally {
      db.close();
    }
    const snap = new DatabaseSync(dest, { readOnly: true });
    try {
      const integrity = String((snap.prepare("PRAGMA integrity_check").get() as { integrity_check: string }).integrity_check);
      if (integrity !== "ok") throw new Error(`备份没有通过完整性检查：${integrity}`);
      const migrations = Number((snap.prepare("SELECT COUNT(*) AS n FROM agent_schema_migrations").get() as { n: number }).n);
      return { path: dest, bytes: statSync(dest).size, migrations, integrity };
    } finally {
      snap.close();
    }
  } catch (e) {
    rmSync(dest, { force: true });
    throw e;
  }
}

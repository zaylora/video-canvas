#!/usr/bin/env node
/**
 * 备份命令：AGENT_DB_PATH=/data/agent.sqlite node dist/backup-cli.js <备份文件路径>
 * 成功：stdout 一行 JSON（路径、大小、迁移条数），退出码 0；失败：stderr 说明原因，退出码 1；参数不对退出码 2。
 */
import { backupDatabase } from "./backup.ts";

const dest = process.argv[2];
const dbPath = process.env.AGENT_DB_PATH;
if (!dest || process.argv.length > 3) {
  process.stderr.write("用法：AGENT_DB_PATH=<数据库文件> node backup-cli.js <备份文件路径>\n");
  process.exit(2);
}
if (!dbPath) {
  process.stderr.write("缺少环境变量 AGENT_DB_PATH\n");
  process.exit(2);
}
try {
  process.stdout.write(JSON.stringify(backupDatabase(dbPath, dest)) + "\n");
} catch (e) {
  process.stderr.write(`备份失败：${(e as Error).message}\n`);
  process.exit(1);
}

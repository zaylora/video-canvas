import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { existsSync, mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { DatabaseSync } from "node:sqlite";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { test } from "node:test";
import { fileURLToPath } from "node:url";
import { backupDatabase } from "../src/backup.ts";
import { openStore } from "../src/store/store.ts";

function withDir(fn: (dir: string) => void | Promise<void>) {
  return async () => {
    const dir = mkdtempSync(join(tmpdir(), "agent-backup-"));
    try {
      await fn(dir);
    } finally {
      rmSync(dir, { recursive: true, force: true });
    }
  };
}

const CLI = fileURLToPath(new URL("../src/backup-cli.ts", import.meta.url));
const node = (args: string[], env: Record<string, string> = {}) =>
  spawnSync("node", ["--disable-warning=ExperimentalWarning", CLI, ...args], { env: { PATH: process.env.PATH ?? "", ...env }, encoding: "utf8" });

test("在线备份：服务还开着库（有未合并的 WAL）时拿到一致的快照，含同一个文件里 Pi 的表；备份能直接当库用", withDir((dir) => {
  const dbPath = join(dir, "agent.sqlite");
  const store = openStore(dbPath, { workerId: 1 });
  try {
    // Pi Durable 的表和应用的表在同一个文件里（前缀 pi_）
    const raw = new DatabaseSync(dbPath);
    raw.exec("CREATE TABLE pi_demo (k TEXT PRIMARY KEY, v TEXT); INSERT INTO pi_demo VALUES ('a', '对话历史');");
    raw.close();
    const sess = store.createSession({ userId: "1", canvasId: "7", title: "备份前", mode: "all", modelKey: "claude" });
    store.appendEvent(sess.id, "0", "message.user", { text: "你好" });

    const dest = join(dir, "backups", "snap.sqlite");
    const res = backupDatabase(dbPath, dest);
    assert.equal(res.integrity, "ok");
    assert.ok(res.bytes > 0 && res.migrations >= 3);
    store.createSession({ userId: "1", canvasId: "7", title: "备份后才有", mode: "all", modelKey: "claude" }); // 备份之后的写入不在快照里

    const restored = openStore(dest, { workerId: 1 });
    try {
      assert.deepEqual(restored.listSessions("1", "7").map((s) => s.title), ["备份前"]);
      assert.equal(restored.listEvents(sess.id, 0).length, 1);
    } finally {
      restored.close();
    }
    const db = new DatabaseSync(dest, { readOnly: true });
    assert.equal((db.prepare("SELECT v FROM pi_demo WHERE k = 'a'").get() as { v: string }).v, "对话历史");
    db.close();
  } finally {
    store.close();
  }
}));

test("不覆盖已有的备份；库不存在时报错而且不会凭空建出一个空库；失败不留半截文件", withDir((dir) => {
  const dbPath = join(dir, "agent.sqlite");
  openStore(dbPath).close();
  const dest = join(dir, "snap.sqlite");
  backupDatabase(dbPath, dest);
  assert.throws(() => backupDatabase(dbPath, dest), /已存在/);

  const missing = join(dir, "nope.sqlite");
  assert.throws(() => backupDatabase(missing, join(dir, "x.sqlite")), /不存在/);
  assert.ok(!existsSync(missing), "不能把「备份」变成建库");
  assert.ok(!existsSync(join(dir, "x.sqlite")));

  const notDb = join(dir, "broken.sqlite");
  writeFileSync(notDb, "这不是 SQLite 文件".repeat(100));
  assert.throws(() => backupDatabase(notDb, join(dir, "y.sqlite")));
  assert.ok(!existsSync(join(dir, "y.sqlite")), "失败后不留半截备份");
}));

test("命令行：AGENT_DB_PATH + 目标路径；成功时 stdout 是一行 JSON、退出码 0；参数不对、库不存在、目标已存在都是非 0 并说明原因", withDir((dir) => {
  const dbPath = join(dir, "agent.sqlite");
  openStore(dbPath).close();
  const dest = join(dir, "out", "snap.sqlite");
  const ok = node([dest], { AGENT_DB_PATH: dbPath });
  assert.equal(ok.status, 0, ok.stderr);
  const out = JSON.parse(ok.stdout.trim());
  assert.deepEqual([out.path, out.integrity], [dest, "ok"]);
  assert.ok(existsSync(dest));

  const again = node([dest], { AGENT_DB_PATH: dbPath });
  assert.equal(again.status, 1);
  assert.match(again.stderr, /已存在/);
  const noArg = node([], { AGENT_DB_PATH: dbPath });
  assert.equal(noArg.status, 2);
  assert.match(noArg.stderr, /用法/);
  const noEnv = node([join(dir, "z.sqlite")]);
  assert.equal(noEnv.status, 2);
  assert.match(noEnv.stderr, /AGENT_DB_PATH/);
  const noDb = node([join(dir, "z.sqlite")], { AGENT_DB_PATH: join(dir, "missing.sqlite") });
  assert.equal(noDb.status, 1);
  assert.match(noDb.stderr, /不存在/);
}));

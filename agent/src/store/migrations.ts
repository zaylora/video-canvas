import { createHash } from "node:crypto";
import type { DatabaseSync } from "node:sqlite";

/**
 * Agent 服务自己的 SQLite 迁移。和 Pi Durable 共用一个文件：Pi 的表按它自己的机制升级（表名前缀不同，互不干扰），
 * 这里的表都叫 agent_*，版本记在 agent_schema_migrations。
 *
 * 规则：
 *  - 只增不改：已经发布的迁移不能修改（记了校验和，改了拒绝启动），要改结构就加一条新迁移；
 *  - 库的版本比代码新（回退了镜像）拒绝启动，不假装能用——升级前要先备份 agent_data 卷；
 *  - 每条迁移在一个事务里，失败整条回滚，版本不前进。
 */
export interface Migration {
  version: number;
  name: string;
  sql: string;
}

// 状态值和 Go 的 model 常量保持一致：活跃运行占用画布，由部分唯一索引保证同一画布同时只有一个
const ACTIVE_RUN_STATUSES = ["queued", "running", "waiting_approval", "waiting_input"].map((s) => `'${s}'`).join(",");

export const MIGRATIONS: Migration[] = [
  {
    version: 1,
    name: "init",
    sql: `
CREATE TABLE agent_sessions (
  id          INTEGER PRIMARY KEY,
  user_id     INTEGER NOT NULL,
  canvas_id   INTEGER NOT NULL,
  title       TEXT    NOT NULL DEFAULT '',
  mode        TEXT    NOT NULL DEFAULT 'all',
  model_key   TEXT    NOT NULL DEFAULT '',
  last_seq    INTEGER NOT NULL DEFAULT 0,
  created_at  INTEGER NOT NULL,
  updated_at  INTEGER NOT NULL,
  deleted_at  INTEGER
);
CREATE INDEX idx_agent_sessions_canvas ON agent_sessions (user_id, canvas_id, updated_at DESC);

CREATE TABLE agent_runs (
  id             INTEGER PRIMARY KEY,
  session_id     INTEGER NOT NULL REFERENCES agent_sessions (id),
  canvas_id      INTEGER NOT NULL,
  user_id        INTEGER NOT NULL,
  status         TEXT    NOT NULL,
  mode           TEXT    NOT NULL DEFAULT 'all',
  budget_credits INTEGER NOT NULL DEFAULT 0,
  spent_credits  INTEGER NOT NULL DEFAULT 0,
  context_tokens INTEGER NOT NULL DEFAULT 0,
  steps          INTEGER NOT NULL DEFAULT 0,
  max_steps      INTEGER NOT NULL DEFAULT 40,
  error_code     TEXT    NOT NULL DEFAULT '',
  error_message  TEXT    NOT NULL DEFAULT '',
  prompt_version INTEGER NOT NULL DEFAULT 0,
  tools_hash     TEXT    NOT NULL DEFAULT '',
  build_sha      TEXT    NOT NULL DEFAULT '',
  harness_key    TEXT    NOT NULL DEFAULT '',
  lease_until    INTEGER NOT NULL DEFAULT 0,
  created_at     INTEGER NOT NULL,
  updated_at     INTEGER NOT NULL,
  ended_at       INTEGER
);
CREATE INDEX idx_agent_runs_session ON agent_runs (session_id);
CREATE INDEX idx_agent_runs_user ON agent_runs (user_id);
CREATE INDEX idx_agent_runs_harness ON agent_runs (harness_key, created_at);
CREATE UNIQUE INDEX uk_agent_runs_active ON agent_runs (canvas_id) WHERE status IN (${ACTIVE_RUN_STATUSES});

CREATE TABLE agent_events (
  id         INTEGER PRIMARY KEY,
  session_id INTEGER NOT NULL REFERENCES agent_sessions (id),
  run_id     INTEGER NOT NULL DEFAULT 0,
  seq        INTEGER NOT NULL,
  type       TEXT    NOT NULL,
  payload    TEXT    NOT NULL DEFAULT '{}',
  created_at INTEGER NOT NULL
);
CREATE UNIQUE INDEX uk_agent_events_seq ON agent_events (session_id, seq);
CREATE INDEX idx_agent_events_created ON agent_events (created_at);

CREATE TABLE agent_approvals (
  id            INTEGER PRIMARY KEY,
  run_id        INTEGER NOT NULL REFERENCES agent_runs (id),
  session_id    INTEGER NOT NULL,
  canvas_id     INTEGER NOT NULL,
  user_id       INTEGER NOT NULL,
  kind          TEXT    NOT NULL,
  tool_call_id  TEXT    NOT NULL DEFAULT '',
  payload       TEXT    NOT NULL DEFAULT '{}',
  quote_credits INTEGER NOT NULL DEFAULT 0,
  status        TEXT    NOT NULL,
  decision      TEXT    NOT NULL DEFAULT '{}',
  result        TEXT    NOT NULL DEFAULT '{}',
  decided_at    INTEGER,
  expires_at    INTEGER NOT NULL,
  created_at    INTEGER NOT NULL,
  updated_at    INTEGER NOT NULL
);
CREATE INDEX idx_agent_approvals_run ON agent_approvals (run_id, status);
CREATE INDEX idx_agent_approvals_user ON agent_approvals (user_id);
`,
  },
  {
    version: 2,
    name: "session-conversation",
    // 会话在 Pi Durable 里的会话 id：对话历史归 Pi，这里只记对应关系
    sql: `ALTER TABLE agent_sessions ADD COLUMN conversation_id TEXT NOT NULL DEFAULT '';`,
  },
];

const checksum = (sql: string): string => createHash("sha256").update(sql).digest("hex");

/** 把迁移按顺序应用到 db；已经应用过的跳过。now 用来记应用时间（毫秒）。 */
export function migrate(db: DatabaseSync, migrations: readonly Migration[], now: () => number): void {
  migrations.forEach((m, i) => {
    if (m.version !== i + 1) throw new Error(`迁移的版本号必须从 1 开始连续，第 ${i + 1} 条是 ${m.version}`);
  });
  db.exec(`CREATE TABLE IF NOT EXISTS agent_schema_migrations (
    version    INTEGER PRIMARY KEY,
    name       TEXT    NOT NULL,
    checksum   TEXT    NOT NULL,
    applied_at INTEGER NOT NULL
  )`);
  const applied = db.prepare("SELECT version, name, checksum FROM agent_schema_migrations ORDER BY version").all() as Array<{ version: number; name: string; checksum: string }>;
  const newest = applied.at(-1)?.version ?? 0;
  if (newest > migrations.length) {
    throw new Error(`数据库的版本 ${newest} 比当前代码（最新是 ${migrations.length}）新：可能回退了镜像。` +
      "请换回新版镜像，或从升级前的备份恢复 agent_data 卷");
  }
  for (const a of applied) {
    const m = migrations[a.version - 1];
    if (a.checksum !== checksum(m.sql)) throw new Error(`迁移 ${a.version}（${m.name}）已经应用过，但内容被修改过。已发布的迁移不能改，请新增一条`);
  }
  for (const m of migrations.slice(applied.length)) {
    db.exec("BEGIN IMMEDIATE");
    try {
      db.exec(m.sql);
      db.prepare("INSERT INTO agent_schema_migrations (version, name, checksum, applied_at) VALUES (?, ?, ?, ?)").run(m.version, m.name, checksum(m.sql), now());
      db.exec("COMMIT");
    } catch (e) {
      db.exec("ROLLBACK");
      throw new Error(`迁移 ${m.version}（${m.name}）失败：${(e as Error).message}`, { cause: e });
    }
  }
}

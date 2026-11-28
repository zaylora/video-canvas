import assert from "node:assert/strict";
import test from "node:test";

import {
  hasPty,
  ok,
  runCli,
  runInPty,
  startSite,
  tempConfig,
  type Handler,
  type Recorded,
} from "./helpers/site.ts";

type Ev = { seq: number; type: string; data?: unknown; run_id?: string };

const SESSIONS = [
  { id: "s1", canvas_id: "c1", title: "命令行", mode: "m", last_seq: 10 },
  { id: "s2", canvas_id: "c1", title: "另一条", mode: "m", last_seq: 42 },
];

/** 小镜的假站点：events 按「第几次拉取」返回预设的一批事件。 */
function agentSite(batches: Array<Ev[] | number>, extra?: Handler) {
  let pull = 0;
  const handler: Handler = (req: Recorded, count) => {
    const custom = extra?.(req, count);
    if (custom) return custom;
    if (req.method === "GET" && req.path === "/api/v1/canvas/c1/agent/sessions")
      return ok(SESSIONS);
    if (req.method === "POST" && req.path === "/api/v1/canvas/c1/agent/sessions") {
      return ok({ id: "s-new", canvas_id: "c1", title: "命令行", mode: "m", last_seq: 0 });
    }
    if (req.method === "POST" && /\/agent\/sessions\/[^/]+\/runs$/.test(req.path)) {
      return ok({ id: "r1", session_id: req.path.split("/")[5], status: "queued" }, 202);
    }
    if (req.method === "GET" && /\/agent\/sessions\/[^/]+\/events$/.test(req.path)) {
      const batch = batches[Math.min(pull++, batches.length - 1)];
      if (typeof batch === "number") return { status: batch, body: { code: 1, msg: "busy" } };
      const sid = req.path.split("/")[5];
      return ok(batch.map((e) => ({ session_id: sid, run_id: "r1", ...e })));
    }
    if (req.method === "POST" && req.path === "/api/v1/agent/approvals/a1/decision") {
      return ok({ id: "a1", status: "approved" });
    }
    return undefined;
  };
  return startSite(handler);
}

const envFor = (base: string): Record<string, string> => ({
  XDG_CONFIG_HOME: tempConfig({ base, token: "t" }),
});

const watch = (extra: string[] = []): string[] => [
  "agent",
  "run",
  "c1",
  "--message",
  "改一下",
  "--watch",
  "--interval",
  "1",
  "--timeout",
  "30",
  "--json",
  ...extra,
];

const status = (seq: number, value: string): Ev => ({
  seq,
  type: "run.status",
  data: { status: value },
});

test("小镜绑定当前协作代数，查看者不能创建会话或启动运行", async (context) => {
  let role = "editor";
  const site = await agentSite([], ({ path }) => {
    if (path.endsWith("/collaboration"))
      return ok({ epoch: 3, role, canvas: { id: "c1", revision: 7 } });
  });
  context.after(() => site.close());
  const args = ["agent", "run", "c1", "--message", "改一下", "--budget", "10", "--json"];
  const env = envFor(site.base);
  const result = await runCli(args, env);
  assert.equal(result.code, 0, result.stdout);
  assert.equal(
    site.requests.find(({ path }) => path.endsWith("/runs"))?.body.collaboration_epoch,
    3,
  );
  role = "viewer";
  const denied = await runCli(args, env);
  assert.equal(denied.code, 1, denied.stdout);
  assert.equal(JSON.parse(denied.stdout).error.reason, "permission_denied");
  assert.equal(site.requests.filter(({ method }) => method === "POST").length, 1);
});

test("小镜读取会话历史并通过现有接口追加指令、续跑和撤销", async (context) => {
  const site = await agentSite([], ({ path }) => {
    if (path.endsWith("/history")) return ok({ events: [], has_more: false });
    if (path.endsWith("/interject")) return ok(null);
    if (path.endsWith("/resume")) return ok({ id: "r1", status: "queued" });
    if (path.endsWith("/undo")) return ok({ revision: 8, reverted: 1, skipped: [] });
  });
  context.after(() => site.close());
  const env = envFor(site.base);
  const sessions = await runCli(["agent", "sessions", "c1", "--json"], env);
  assert.equal(sessions.code, 0, sessions.stdout);
  assert.deepEqual(JSON.parse(sessions.stdout).data, SESSIONS);
  for (const command of [
    ["history", "s1", "--before", "20", "--limit", "30"],
    ["run", "c1", "--message", "参考这个镜头", "--attach", "9007199254740993"],
    ["interject", "r1", "--message", "改成夜景", "--attach", "32"],
    ["resume", "r1", "--add-budget", "20"],
  ]) {
    const result = await runCli(["agent", ...command, "--json"], env);
    assert.equal(result.code, 0, result.stdout);
  }
  const history = site.requests.find(({ path }) => path.endsWith("/history"));
  assert.equal(history?.query.get("before"), "20");
  assert.equal(history?.query.get("limit"), "30");
  assert.deepEqual(
    site.requests.filter(({ method }) => method === "POST").map(({ body }) => body),
    [
      { message: "参考这个镜头", attachments: [{ asset_id: "9007199254740993" }] },
      { message: "改成夜景", attachments: [{ asset_id: "32" }] },
      { add_budget: 20 },
    ],
  );
  if (hasPty) {
    const undo = await runInPty(["agent", "undo", "r1"], env, "yes\n");
    assert.equal(undo.code, 0, undo.stdout + undo.stderr);
    assert.equal(site.requests.at(-1)?.path, "/api/v1/agent/runs/r1/undo");
  }
});

test("agent run --session：会话不存在就报错，不新建会话也不提交运行", async () => {
  const site = await agentSite([[]]);
  try {
    const res = await runCli(
      ["agent", "run", "c1", "--message", "x", "--session", "nope"],
      envFor(site.base),
    );
    assert.equal(res.code, 2);
    assert.match(res.stderr, /nope/);
    assert.equal(site.requests.filter((r) => r.method === "POST").length, 0);
  } finally {
    await site.close();
  }
});

test("agent run --session：从指定会话当前的位置往后看，不重放历史", async () => {
  const site = await agentSite([[status(43, "succeeded")]]);
  try {
    const res = await runCli(watch(["--session", "s2"]), envFor(site.base));
    assert.equal(res.code, 0, res.stderr);
    const events = site.requests.find((r) => r.path.endsWith("/sessions/s2/events"));
    assert.equal(events?.query.get("after"), "42");
  } finally {
    await site.close();
  }
});

test("agent run --watch：approval.decided 不算在等审批，要跟到运行结束", async () => {
  const site = await agentSite([
    [{ seq: 11, type: "approval.decided", data: { status: "approved", kind: "generate" } }],
    [status(12, "succeeded")],
  ]);
  try {
    const res = await runCli(watch(), envFor(site.base));
    assert.equal(res.code, 0, res.stderr);
    const body = JSON.parse(res.stdout.trim()) as { data: { waiting: boolean; events: Ev[] } };
    assert.equal(body.data.waiting, false);
    assert.equal(body.data.events.length, 2, "不能在 decided 这一条就收尾");
  } finally {
    await site.close();
  }
});

test("agent run --watch：回了话之后还有工具调用就继续等，不提前收尾", async () => {
  const site = await agentSite([
    [{ seq: 11, type: "message.done", data: { text: "我先去查一下" } }],
    [{ seq: 12, type: "tool.start", data: { id: "t1", name: "canvas_read" } }],
    [{ seq: 13, type: "tool.end", data: { id: "t1" } }, status(14, "succeeded")],
  ]);
  try {
    const res = await runCli(watch(), envFor(site.base));
    assert.equal(res.code, 0, res.stderr);
    const body = JSON.parse(res.stdout.trim()) as { data: { events: Ev[] } };
    assert.deepEqual(
      body.data.events.map((e) => e.type),
      ["message.done", "tool.start", "tool.end", "run.status"],
    );
  } finally {
    await site.close();
  }
});

test("agent run --watch：需要审批时停下，审批命令里带上 --session", async () => {
  const created: Ev = {
    seq: 11,
    type: "approval.created",
    data: {
      id: "a1",
      payload: { reason: "要花积分", items: [{ label: "镜头1", count: 2, price: 5 }] },
    },
  };
  const site = await agentSite([[created]]);
  try {
    const env = envFor(site.base);
    const res = await runCli(watch(), env);
    assert.equal(res.code, 0, res.stderr);
    assert.equal(
      (JSON.parse(res.stdout.trim()) as { data: { waiting: boolean } }).data.waiting,
      true,
    );
    assert.match(res.stderr, /agent approve a1 --session s2/);
  } finally {
    await site.close();
  }
});

test("agent run --watch：拉事件遇到 5xx 会退避重试而不是退出", async () => {
  const site = await agentSite([503, [status(11, "succeeded")]]);
  try {
    const res = await runCli(watch(), envFor(site.base));
    assert.equal(res.code, 0, res.stderr);
    assert.match(res.stderr, /重试/);
  } finally {
    await site.close();
  }
});

const approvalEvents: Ev[] = [
  {
    seq: 11,
    type: "approval.created",
    data: {
      id: "a1",
      payload: {
        reason: "要花积分",
        items: [
          { label: "生成镜头1", model_name: "视频模型", count: 2, price: 5 },
          { label: "生成镜头2", model_name: "视频模型", count: 1, price: 8 },
        ],
      },
    },
  },
];

test("agent approve --session：批准前把将批准的条目内容摊开（只列选中的）", async () => {
  const site = await agentSite([approvalEvents]);
  try {
    const res = await runCli(
      ["agent", "approve", "a1", "--session", "s1", "--items", "0", "--json"],
      envFor(site.base),
    );
    assert.equal(res.code, 0, res.stderr);
    assert.match(res.stderr, /生成镜头1/);
    assert.doesNotMatch(res.stderr, /生成镜头2/);
    const post = site.requests.find((r) => r.method === "POST" && r.path.endsWith("/decision"));
    assert.deepEqual(post?.body.items, [{ index: 0, approve: true }]);
  } finally {
    await site.close();
  }
});

test("agent approve：没给 --session 时说明看不到条目内容，并提示怎么补", async () => {
  const site = await agentSite([approvalEvents]);
  try {
    const res = await runCli(["agent", "approve", "a1", "--json"], envFor(site.base));
    assert.equal(res.code, 0, res.stderr);
    assert.match(res.stderr, /--session/);
  } finally {
    await site.close();
  }
});

test(
  "agent approve：在终端里先展示再确认，输入 yes 才提交，别的输入取消",
  { skip: !hasPty },
  async () => {
    const site = await agentSite([approvalEvents]);
    try {
      const env = envFor(site.base);
      const argv = ["agent", "approve", "a1", "--session", "s1"];
      const no = await runInPty(argv, env, "no");
      assert.equal(no.code, 4);
      assert.match(no.stdout, /生成镜头1/);
      assert.equal(site.requests.filter((r) => r.path.endsWith("/decision")).length, 0);
      const yes = await runInPty(argv, env, "yes");
      assert.equal(yes.code, 0, yes.stdout);
      assert.equal(site.requests.filter((r) => r.path.endsWith("/decision")).length, 1);
    } finally {
      await site.close();
    }
  },
);

test("agent approve --reject：驳回不需要确认，也不拉条目", async () => {
  const site = await agentSite([approvalEvents]);
  try {
    const res = await runCli(["agent", "approve", "a1", "--reject", "--json"], envFor(site.base));
    assert.equal(res.code, 0, res.stderr);
    const post = site.requests.find((r) => r.path.endsWith("/decision"));
    assert.equal(post?.body.decision, "reject");
  } finally {
    await site.close();
  }
});

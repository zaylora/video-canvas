import assert from "node:assert/strict";
import { writeFileSync } from "node:fs";
import { join } from "node:path";
import test from "node:test";

import { hasPty, ok, runCli, runInPty, startSite, tempConfig } from "./helpers/site.ts";

const graph = { nodes: [{ id: "shot", type: "canvas", data: { label: "分镜" } }], edges: [] };
const canvas = { id: "c1", title: "协作画布", revision: 7, payload_json: graph };
const snapshot = { role: "editor", epoch: 3, canGenerate: false, canvas };

test("快照和回放跨过 JavaScript 安全整数边界时保持连续版本", async (context) => {
  const raw = (JSON as unknown as { rawJSON(text: string): object }).rawJSON;
  const epoch = "9007199254740993";
  const after = "9007199254740991";
  const revisions = ["9007199254740992", "9007199254740993"];
  const site = await startSite(({ path, query }) => {
    if (path.endsWith("/collaboration"))
      return ok({
        ...snapshot,
        epoch: raw(epoch),
        canvas: { ...canvas, revision: raw(revisions[1]!) },
      });
    if (path.endsWith("/collaboration/events")) {
      assert.equal(query.get("epoch"), epoch);
      assert.equal(query.get("after"), after);
      return ok({
        epoch: raw(epoch),
        revision: raw(revisions[1]!),
        cursor: raw(revisions[1]!),
        events: revisions.map((revision) => ({ epoch: raw(epoch), revision: raw(revision) })),
        role: "editor",
        canGenerate: false,
      });
    }
  });
  context.after(() => site.close());
  const env = { XDG_CONFIG_HOME: tempConfig({ base: site.base, token: "t" }) };
  const watched = await runCli(
    ["canvas", "watch", "c1", "--since", after, "--epoch", epoch, "--once", "--jsonl"],
    env,
  );
  assert.equal(watched.code, 0, watched.stderr);
  const events = watched.stdout
    .trim()
    .split("\n")
    .map((line) => JSON.parse(line));
  assert.deepEqual(
    events
      .filter(({ type }) => type === "change")
      .map(({ epoch, revision }) => ({ epoch, revision })),
    revisions.map((revision) => ({ epoch, revision })),
  );
});

test("协作提交在请求和回执中保留大版本的精确值", async (context) => {
  const raw = (JSON as unknown as { rawJSON(text: string): object }).rawJSON;
  const epoch = "9007199254740993";
  const revision = "9007199254740995";
  const confirmed = "9007199254740996";
  const site = await startSite(({ path, body, raw: request }) => {
    if (path.endsWith("/me")) return ok({ id: 1 });
    if (path.endsWith("/collaboration"))
      return ok({ ...snapshot, epoch: raw(epoch), canvas: { ...canvas, revision: raw(revision) } });
    if (path.endsWith("/collaboration/ops")) {
      assert.ok(request.includes(`"epoch":${epoch},`));
      assert.ok(request.includes(`"baseRevision":${revision},`));
      return ok({
        opId: body.opId,
        epoch: raw(epoch),
        revision: raw(confirmed),
        changes: body.changes,
      });
    }
  });
  context.after(() => site.close());
  const config = tempConfig({ base: site.base, token: "t" });
  const env = { XDG_CONFIG_HOME: config };
  const file = join(config, "changes.json");
  const plan = join(config, "plan.json");
  writeFileSync(
    file,
    JSON.stringify({
      changes: [{ kind: "node", id: "shot", fields: { "data.label": "确认分镜" } }],
    }),
  );
  const prepared = await runCli(
    ["canvas", "plan", "c1", "--input", file, "-o", plan, "--json"],
    env,
  );
  assert.equal(prepared.code, 0, prepared.stderr);
  const applied = await runCli(["canvas", "apply", "c1", "--plan", plan, "--json"], env);
  assert.equal(applied.code, 0, applied.stdout);
  assert.equal(JSON.parse(applied.stdout).data.revision, confirmed);
});

test("监听独立恢复评论游标，按原顺序输出且不推进图谱版本", async (context) => {
  const comment = { id: "note2", nodeId: "shot", authorUsername: "导演", body: "继续这一镜" };
  const site = await startSite(({ path, query }) => {
    if (path.endsWith("/collaboration"))
      return ok({ ...snapshot, capabilities: { actions: ["comments-replay"] } });
    if (path.endsWith("/collaboration/events"))
      return ok({
        epoch: 3,
        revision: 7,
        cursor: 7,
        events: [],
        role: "editor",
        canGenerate: false,
      });
    if (path.endsWith("/comments/events")) {
      if (query.get("after") === "note1")
        return ok({ cursor: "note2", items: [comment], more: true });
      if (query.get("after") === "note2") return ok({ cursor: "note3", items: [], more: false });
    }
  });
  context.after(() => site.close());
  const result = await runCli(
    [
      "canvas",
      "watch",
      "c1",
      "--since",
      "7",
      "--epoch",
      "3",
      "--comments-since",
      "note1",
      "--once",
      "--jsonl",
    ],
    { XDG_CONFIG_HOME: tempConfig({ base: site.base, token: "t" }) },
  );
  assert.equal(result.code, 0, result.stderr);
  const events = result.stdout
    .trim()
    .split("\n")
    .map((line) => JSON.parse(line));
  assert.deepEqual(
    events.map(({ type, revision }) => [type, revision]),
    [
      ["comment", "7"],
      ["comment-cursor", "7"],
      ["comment-cursor", "7"],
    ],
  );
  assert.deepEqual(events[0].data, comment);
  assert.deepEqual(events[1].data, { cursor: "note2" });
  assert.deepEqual(events[2].data, { cursor: "note3" });
});

test("共享列表和受邀编辑者的已确认图谱", async (context) => {
  const site = await startSite((request) => {
    if (request.path === "/api/v1/canvas/shared") {
      return ok({ list: [{ ...canvas, role: "editor" }], page: 2, page_size: 10, total: 11 });
    }
    if (request.path.endsWith("/collaboration")) return ok(snapshot);
    if (request.path === "/api/v1/canvas/c1") {
      return { status: 404, body: { code: 30001, msg: "画布不存在" } };
    }
  });
  context.after(() => site.close());
  const env = { XDG_CONFIG_HOME: tempConfig({ base: site.base, token: "t" }) };
  const listed = await runCli(
    ["canvas", "list", "--scope", "shared", "--page", "2", "--json"],
    env,
  );
  assert.equal(listed.code, 0, listed.stderr);
  assert.equal(JSON.parse(listed.stdout).data.list[0].role, "editor");
  assert.equal(site.requests[0]?.query.get("page"), "2");
  const got = await runCli(["canvas", "get", "c1", "--json"], env);
  assert.equal(got.code, 0, got.stderr);
  assert.deepEqual(JSON.parse(got.stdout).data.payload_json, graph);
  const status = await runCli(["canvas", "status", "c1", "--json"], env);
  assert.equal(status.code, 0, status.stderr);
  const result = JSON.parse(status.stdout).data;
  assert.equal(result.role, "editor");
  assert.equal(result.epoch, "3");
  assert.equal(result.revision, "7");
  assert.equal(result.canGenerate, false);
});

test("状态公布协议能力，计划遵守站点批次上限和支持的版本", async (context) => {
  const capabilities = {
    protocolVersion: 1,
    maxRecordsPerBatch: 1,
    maxBatchBytes: 4096,
    actions: ["commit", "replay"],
  };
  const site = await startSite(({ path }) => {
    if (path.endsWith("/collaboration")) return ok({ ...snapshot, capabilities });
    if (path.endsWith("/me")) return ok({ id: 1 });
  });
  context.after(() => site.close());
  const config = tempConfig({ base: site.base, token: "t" });
  const env = { XDG_CONFIG_HOME: config };
  const status = await runCli(["canvas", "status", "c1", "--json"], env);
  assert.deepEqual(JSON.parse(status.stdout).data.capabilities, capabilities);
  const input = join(config, "changes.json");
  const changes = ["first", "second"].map((id) => ({
    kind: "node",
    id,
    fields: { "data.label": id },
  }));
  writeFileSync(input, JSON.stringify({ changes }));
  const args = ["canvas", "plan", "c1", "--input", input, "--json"];
  const oversize = await runCli(args, env);
  assert.equal(oversize.code, 2, oversize.stdout);
  assert.match(JSON.parse(oversize.stdout).error.message, /批次.*1/);
  capabilities.protocolVersion = 2;
  const unsupported = await runCli(args, env);
  assert.equal(JSON.parse(unsupported.stdout).error.reason, "unsupported");
  assert.ok(!site.requests.some(({ method }) => method === "POST"));
});

test("普通共享查看者通过只读入口获取画布", async (context) => {
  const site = await startSite((request) => {
    if (request.path.endsWith("/reader")) return ok(canvas);
    if (request.path.endsWith("/collaboration")) {
      return { status: 409, body: { code: 30018, msg: "协作会话已变更" } };
    }
    return { status: 404, body: { code: 30001, msg: "画布不存在" } };
  });
  context.after(() => site.close());
  const env = { XDG_CONFIG_HOME: tempConfig({ base: site.base, token: "t" }) };
  const result = await runCli(["canvas", "get", "c1", "--json"], env);
  assert.equal(result.code, 0, result.stderr);
  assert.equal(JSON.parse(result.stdout).data.collaboration_role, "viewer");
});

test("watch 按连续版本输出增量，保留被裁剪的空版本", async (context) => {
  const site = await startSite((request) => {
    if (request.path.endsWith("/collaboration")) return ok(snapshot);
    if (request.path.endsWith("/events")) {
      assert.equal(request.query.get("epoch"), "3");
      assert.equal(request.query.get("after"), "5");
      return ok({
        ...snapshot,
        revision: 7,
        cursor: 7,
        events: [
          { epoch: 3, revision: 6 },
          { epoch: 3, revision: 7, changes: [] },
        ],
      });
    }
  });
  context.after(() => site.close());
  const env = { XDG_CONFIG_HOME: tempConfig({ base: site.base, token: "t" }) };
  const result = await runCli(
    ["canvas", "watch", "c1", "--since", "5", "--epoch", "3", "--once", "--jsonl"],
    env,
  );
  assert.equal(result.code, 0, result.stderr);
  const events = result.stdout
    .trim()
    .split("\n")
    .map((line) => JSON.parse(line));
  assert.deepEqual(
    events.map(({ revision }) => revision),
    ["6", "7"],
  );
  assert.ok(events.every(({ type }) => type === "change"));
});

test("监听游标不能跨协作会话回放，且历史缺口重取快照", async (context) => {
  const site = await startSite(({ path }) => {
    if (path.endsWith("/collaboration")) return ok(snapshot);
    if (path.endsWith("/events")) return ok({ snapshotRequired: true, epoch: 3 });
  });
  context.after(() => site.close());
  const env = { XDG_CONFIG_HOME: tempConfig({ base: site.base, token: "t" }) };
  const args = ["canvas", "watch", "c1", "--since", "5", "--once", "--jsonl", "--epoch"];
  const expired = await runCli([...args, "2"], env);
  assert.equal(expired.code, 1, expired.stdout);
  assert.ok(site.requests.every(({ path }) => !path.endsWith("/events")));
  const gap = await runCli([...args, "3"], env);
  assert.equal(gap.code, 0, gap.stderr);
  const event = JSON.parse(gap.stdout);
  assert.equal(event.type, "snapshot");
  assert.equal(event.revision, "7");
});

test("节点编辑用确认过的协作版本提交，并报告字段冲突", { skip: !hasPty }, async (context) => {
  const editable = { ...graph.nodes[0], data: { kind: "script", label: "分镜" } };
  const site = await startSite((request) => {
    if (request.path === "/api/v1/me") return ok({ id: 1 });
    if (request.path.endsWith("/collaboration")) {
      return ok({
        ...snapshot,
        canvas: { ...canvas, payload_json: { ...graph, nodes: [editable] } },
      });
    }
    if (request.path.endsWith("/collaboration/ops")) {
      return ok({
        epoch: 3,
        revision: 8,
        opId: request.body.opId,
        conflicts: [{ id: "shot", field: "data.text" }],
      });
    }
  });
  context.after(() => site.close());
  const env = { XDG_CONFIG_HOME: tempConfig({ base: site.base, token: "t" }) };
  const result = await runInPty(
    ["canvas", "node", "set", "c1", "shot", "--text", "新正文"],
    env,
    "yes\n",
  );
  assert.equal(result.code, 1);
  assert.match(result.stderr + result.stdout, /shot\.data\.text/);
  const commits = site.requests.filter(({ method }) => method === "POST");
  assert.equal(commits.length, 1);
  assert.equal(commits[0]?.path, "/api/v1/canvas/c1/collaboration/ops");
  assert.equal(commits[0]?.body.epoch, 3);
  assert.equal(commits[0]?.body.baseRevision, 7);
  assert.deepEqual(commits[0]?.body.changes, [
    { kind: "node", id: "shot", fields: { "data.text": "新正文" } },
  ]);
});

test("查看者和协作画布的整图保存均无法进入旧写接口", async (context) => {
  const site = await startSite((request) => {
    if (request.path.endsWith("/collaboration")) return ok({ ...snapshot, role: "viewer" });
  });
  context.after(() => site.close());
  const config = tempConfig({ base: site.base, token: "t" });
  const env = { XDG_CONFIG_HOME: config };
  const payload = join(config, "graph.json");
  writeFileSync(payload, JSON.stringify(graph));
  const result = await runCli(
    ["canvas", "save", "c1", "--payload", payload, "--revision", "7", "--json"],
    env,
  );
  assert.equal(result.code, 1, result.stdout);
  assert.equal(JSON.parse(result.stdout).error.reason, "permission_denied");
  assert.ok(site.requests.every(({ method }) => method === "GET"));
});

test("删除节点的终端确认锁定预览版本", { skip: !hasPty }, async (context) => {
  const site = await startSite(({ path, body }) => {
    if (path.endsWith("/collaboration")) return ok(snapshot);
    if (path.endsWith("/me")) return ok({ id: 1 });
    if (path.endsWith("/collaboration/ops")) {
      const conflicts = body.requireBaseRevision ? [{ id: "revision", revision: 8 }] : [];
      return ok({ opId: body.opId, epoch: 3, revision: 8, conflicts });
    }
  });
  context.after(() => site.close());
  const env = { XDG_CONFIG_HOME: tempConfig({ base: site.base, token: "t" }) };
  const result = await runInPty(["canvas", "node", "rm", "c1", "shot"], env, "yes\n");
  assert.equal(result.code, 1, result.stdout);
  const post = site.requests.find(({ method }) => method === "POST");
  assert.equal(post?.body.baseRevision, 7);
  assert.equal(post?.body.requireBaseRevision, true);
});

import assert from "node:assert/strict";
import { rmSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import test from "node:test";

import { ok, runCli, startSite, tempConfig } from "./helpers/site.ts";

test("计划内删除镜头也必须交互确认", async (context) => {
  const site = await startSite(({ path }) => {
    if (path.endsWith("/me")) return ok({ id: 1 });
    if (path.endsWith("/collaboration"))
      return ok({ epoch: 3, role: "editor", canvas: { id: "c1", revision: 7 } });
  });
  context.after(() => site.close());
  const config = tempConfig({ base: site.base, token: "t" });
  context.after(() => rmSync(config, { recursive: true, force: true }));
  const env = { XDG_CONFIG_HOME: config };
  const input = join(config, "input.json");
  const plan = join(config, "plan.json");
  const changes = [
    { kind: "node", id: "board", action: "storyboard-shot", value: { action: "rm", id: "shot" } },
  ];
  writeFileSync(input, JSON.stringify({ changes }));
  const planned = await runCli(
    ["canvas", "plan", "c1", "--input", input, "-o", plan, "--json"],
    env,
  );
  assert.equal(planned.code, 0, planned.stdout);
  const applied = await runCli(["canvas", "apply", "c1", "--plan", plan, "--json"], env);
  assert.equal(applied.code, 2, applied.stdout);
  assert.match(JSON.parse(applied.stdout).error.message, /必须先由人确认/);
  assert.equal(site.requests.filter(({ method }) => method === "POST").length, 0);
});

test("计划保留观察版本，未知结果用原载荷和操作编号恢复", async (context) => {
  let revision = 7;
  let actor = 1;
  let submits = 0;
  let recorded = false;
  const site = await startSite((request) => {
    if (request.path === "/api/v1/me") return ok({ id: actor });
    if (request.path.endsWith("/collaboration")) {
      return ok({ epoch: 3, role: "editor", canvas: { id: "c1", revision } });
    }
    if (request.method === "GET" && request.path.includes("/collaboration/ops/")) {
      assert.equal(request.query.get("epoch"), "3");
      return ok({
        epoch: 3,
        opId: request.path.split("/").at(-1),
        ...(recorded ? { result: "applied", revision: 9 } : { result: "missing" }),
      });
    }
    if (request.path.endsWith("/collaboration/ops")) {
      submits++;
      if (submits === 1) return { status: 502, body: { code: -1, msg: "暂时不可用" } };
      recorded = true;
      return ok({ epoch: 3, opId: request.body.opId, revision: 9 });
    }
  });
  context.after(() => site.close());
  const config = tempConfig({ base: site.base, token: "t" });
  const env = { XDG_CONFIG_HOME: config };
  const input = join(config, "input.json");
  const plan = join(config, "plan.json");
  const changes = [{ kind: "node", id: "shot", fields: { "data.prompt": "草稿" } }];
  writeFileSync(input, JSON.stringify({ changes }));
  const planned = await runCli(
    ["canvas", "plan", "c1", "--input", input, "-o", plan, "--json"],
    env,
  );
  assert.equal(planned.code, 0, planned.stdout);
  const opId = JSON.parse(planned.stdout).data.opId;
  revision = 8;
  const applied = await runCli(["canvas", "apply", "c1", "--plan", plan, "--json"], env);
  assert.equal(applied.code, 1);
  assert.equal(JSON.parse(applied.stdout).error.reason, "result_unknown");
  actor = 2;
  const wrongActor = await runCli(["canvas", "resume", "c1", "--run", opId, "--json"], env);
  assert.equal(wrongActor.code, 1);
  assert.equal(submits, 1);
  actor = 1;
  const resumed = await runCli(["canvas", "resume", "c1", "--run", opId, "--json"], env);
  assert.equal(resumed.code, 0, resumed.stdout);
  const queried = await runCli(["canvas", "op", "status", "c1", opId, "--json"], env);
  assert.equal(queried.code, 0, queried.stdout);
  assert.equal(JSON.parse(queried.stdout).data.revision, "9");
  const confirmed = await runCli(["canvas", "resume", "c1", "--run", opId, "--json"], env);
  assert.equal(confirmed.code, 0, confirmed.stdout);
  assert.equal(JSON.parse(confirmed.stdout).data.result, "applied");
  const requests = site.requests.filter((request) => request.path.endsWith("/collaboration/ops"));
  assert.equal(requests.length, 2);
  assert.deepEqual(requests[0]?.body, { opId, epoch: 3, baseRevision: 7, changes });
  assert.deepEqual(requests[1]?.body, requests[0]?.body);
});

test("多批计划隔离冲突和未知结果，恢复后仅补交未确认的依赖", async (context) => {
  let revision = 7;
  const receipts = new Map<string, { result: "conflict" | "applied"; revision: number }>();
  const site = await startSite(({ path, method, body }) => {
    if (path.endsWith("/me")) return ok({ id: 1 });
    if (path.endsWith("/collaboration")) {
      return ok({ epoch: 3, role: "editor", canvas: { id: "c1", revision } });
    }
    if (method === "GET" && path.includes("/collaboration/ops/")) {
      const opId = path.split("/").at(-1) ?? "";
      return ok({ epoch: 3, opId, ...(receipts.get(opId) ?? { result: "missing" }) });
    }
    if (path.endsWith("/collaboration/ops")) {
      const opId = String(body.opId);
      const [change] = body.changes as Array<{ id: string }>;
      if (change?.id === "conflict") {
        receipts.set(opId, { result: "conflict", revision });
        return ok({ epoch: 3, opId, revision, conflicts: [{ id: "conflict", revision }] });
      }
      receipts.set(opId, { result: "applied", revision: ++revision });
      if (change?.id === "lost") return { status: 502, body: { code: -1, msg: "响应丢失" } };
      return ok({ epoch: 3, opId, revision });
    }
  });
  context.after(() => site.close());
  const config = tempConfig({ base: site.base, token: "t" });
  context.after(() => rmSync(config, { force: true, recursive: true }));
  const env = { XDG_CONFIG_HOME: config };
  const input = join(config, "input.json");
  const plan = join(config, "plan.json");
  function batch(id: string, after: string[] = []) {
    return { id, after, changes: [{ kind: "node", id, fields: { "data.prompt": "稿件" } }] };
  }
  writeFileSync(
    input,
    JSON.stringify({
      batches: [
        batch("lost"),
        batch("dependent", ["lost"]),
        batch("conflict"),
        batch("blocked", ["conflict"]),
        batch("independent"),
      ],
    }),
  );
  const planned = await runCli(
    ["canvas", "plan", "c1", "--input", input, "-o", plan, "--json"],
    env,
  );
  assert.equal(planned.code, 0, planned.stdout);
  const { runId } = JSON.parse(planned.stdout).data;
  const applied = await runCli(["canvas", "apply", "c1", "--plan", plan, "--json"], env);
  assert.equal(applied.code, 1, applied.stdout);
  const result = JSON.parse(applied.stdout).error;
  assert.equal(result.reason, "partial");
  assert.equal(result.details.runId, runId);
  assert.deepEqual(
    result.details.batches.map(({ status }: { status: string }) => status),
    ["result_unknown", "blocked", "conflict", "blocked", "applied"],
  );
  const resumed = await runCli(["canvas", "resume", "c1", "--run", runId, "--json"], env);
  assert.equal(resumed.code, 1, resumed.stdout);
  assert.deepEqual(
    JSON.parse(resumed.stdout).error.details.batches.map(
      ({ status }: { status: string }) => status,
    ),
    ["applied", "applied", "conflict", "blocked", "applied"],
  );
  const posts = site.requests.filter(
    ({ method, path }) => method === "POST" && path.endsWith("/ops"),
  );
  assert.deepEqual(
    posts.map(({ body }) => (body.changes as Array<{ id: string }>)[0]?.id),
    ["lost", "conflict", "independent", "dependent"],
  );
  assert.ok(posts.every(({ body }) => body.baseRevision === 7));
});

test("多批计划在产生副作用前拒绝未声明或循环的依赖", async (context) => {
  const config = tempConfig();
  context.after(() => rmSync(config, { force: true, recursive: true }));
  const input = join(config, "input.json");
  const changes = [{ kind: "node", id: "shot", fields: { "data.prompt": "稿件" } }];
  writeFileSync(input, JSON.stringify({ batches: [{ id: "self", after: ["self"], changes }] }));
  const result = await runCli(["canvas", "plan", "c1", "--input", input, "--json"], {
    XDG_CONFIG_HOME: config,
  });
  assert.equal(result.code, 2, result.stdout);
  assert.match(JSON.parse(result.stdout).error.message, /依赖/);
});

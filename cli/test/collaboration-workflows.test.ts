import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import test from "node:test";

import { hasPty, ok, runCli, runInPty, startSite, tempConfig } from "./helpers/site.ts";

const jobId = "fedcba9876543210fedcba9876543210";

test("分镜命令保留最初观察版本，由服务端判定字段或顺序冲突", async (context) => {
  const revision = 8;
  const shots = [
    { id: "entrance", scene: "外景" },
    { id: "dialogue", scene: "室内" },
  ];
  const site = await startSite(({ path, body }) => {
    if (path.endsWith("/collaboration"))
      return ok({
        epoch: 3,
        role: "editor",
        canvas: {
          id: "c1",
          revision,
          payload_json: {
            nodes: [{ id: "board", data: { kind: "storyboard", storyboard: { shots } } }],
          },
        },
      });
    if (path.endsWith("/me")) return ok({ id: 1 });
    if (path.endsWith("/collaboration/ops"))
      return ok({ epoch: 3, opId: body.opId, revision: revision + 1 });
  });
  context.after(() => site.close());
  const root = tempConfig({ base: site.base, token: "t" });
  context.after(() => fs.rmSync(root, { recursive: true, force: true }));
  const env = { XDG_CONFIG_HOME: root };
  const prefix = ["canvas", "storyboard", "shot"];
  const listed = await runCli([...prefix, "list", "c1", "board", "--json"], env);
  assert.equal(listed.code, 0, listed.stdout);
  assert.deepEqual(JSON.parse(listed.stdout).data, { epoch: "3", revision: "8", shots });
  const input = path.join(root, "shot.json");
  fs.writeFileSync(input, JSON.stringify({ scene: "夜晚" }));
  const modified = await runCli(
    [
      ...prefix,
      "set",
      "c1",
      "board",
      "entrance",
      "--input",
      input,
      "--epoch",
      "3",
      "--revision",
      "7",
      "--json",
    ],
    env,
  );
  assert.equal(modified.code, 0, modified.stdout);
  const move = [
    ...prefix,
    "move",
    "c1",
    "board",
    "entrance",
    "--index",
    "2",
    "--epoch",
    "3",
    "--json",
  ];
  const moved = await runCli([...move, "--revision", "8"], env);
  assert.equal(moved.code, 0, moved.stdout);
  const commits = site.requests.filter(({ method }) => method === "POST");
  assert.deepEqual(
    commits.map(({ body }) => ({ revision: body.baseRevision, changes: body.changes })),
    [
      {
        revision: 7,
        changes: [
          {
            kind: "node",
            id: "board",
            action: "storyboard-shot",
            value: { action: "set", id: "entrance", fields: { scene: "夜晚" } },
          },
        ],
      },
      {
        revision: 8,
        changes: [
          {
            kind: "node",
            id: "board",
            action: "storyboard-shot",
            value: { action: "move", id: "entrance", index: 2 },
          },
        ],
      },
    ],
  );
});

function snapshot(epoch = 3, revision = 7) {
  return ok({
    epoch,
    role: "editor",
    canGenerate: true,
    storyboardBudgetLimit: true,
    canvas: {
      id: "c1",
      revision,
      payload_json: { nodes: [{ id: "board", data: { kind: "storyboard" } }] },
    },
  });
}

test("导演台应用部分对象属性，保留用户所见版本", async (context) => {
  const site = await startSite(({ path, body }) => {
    if (path.endsWith("/me")) return ok({ id: 1 });
    if (path.endsWith("/collaboration"))
      return ok({
        epoch: 3,
        role: "editor",
        canvas: {
          id: "c1",
          revision: 8,
          payload_json: {
            nodes: [
              { id: "stage", data: { kind: "director", scene: { props: [{ id: "wall", x: 0 }] } } },
            ],
          },
        },
      });
    if (path.endsWith("/collaboration/ops")) return ok({ epoch: 3, opId: body.opId, revision: 9 });
  });
  context.after(() => site.close());
  const root = tempConfig({ base: site.base, token: "t" });
  context.after(() => fs.rmSync(root, { recursive: true, force: true }));
  const env = { XDG_CONFIG_HOME: root };
  const input = path.join(root, "object.json");
  const edit = { collection: "props", id: "wall", action: "set", fields: { x: 4 } };
  fs.writeFileSync(input, JSON.stringify(edit));
  const response = await runCli(
    [
      "canvas",
      "director",
      "scene",
      "apply",
      "c1",
      "stage",
      "--epoch",
      "3",
      "--revision",
      "7",
      "--input",
      input,
      "--json",
    ],
    env,
  );
  assert.equal(response.code, 0, response.stdout);
  const body = site.requests.find(({ method }) => method === "POST")?.body;
  assert.equal(body?.baseRevision, 7);
  assert.deepEqual(body?.changes, [
    { kind: "node", id: "stage", action: "director-object", value: edit },
  ]);
});

test("拆分镜重试复用确认版本，任务状态可在新进程读取", async (context) => {
  let actor = 1;
  let submissions = 0;
  const site = await startSite(({ path, body }) => {
    if (path.endsWith("/collaboration")) return snapshot(3, 7 + submissions);
    if (path.endsWith("/me")) return ok({ id: actor });
    if (path.endsWith("/collaboration/ops")) return ok({ epoch: 3, opId: body.opId, revision: 8 });
    if (path.endsWith("/storyboard/split")) {
      submissions++;
      if (submissions === 1) return { status: 502, body: { code: -1, msg: "响应丢失" } };
    }
    if (path.endsWith("/storyboard/split") || path.endsWith(`/storyboard/split/${jobId}`)) {
      return ok({ id: jobId, status: "succeeded", credits: 1 });
    }
  });
  context.after(() => site.close());
  const root = tempConfig({ base: site.base, token: "t" });
  context.after(() => fs.rmSync(root, { recursive: true, force: true }));
  const script = path.join(root, "script.txt");
  fs.writeFileSync(script, " 剧本 \n");
  const env = { XDG_CONFIG_HOME: root };
  const args = [
    "canvas",
    "storyboard",
    "split",
    "c1",
    "board",
    "--script-file",
    script,
    "--mode",
    "append",
    "--max-credits",
    "12",
    "--idempotency-key",
    "split-1",
    "--json",
  ];
  const lost = await runCli(args, env);
  assert.equal(JSON.parse(lost.stdout).error.reason, "result_unknown");
  actor++;
  const other = await runCli(args, env);
  assert.equal(JSON.parse(other.stdout).error.reason, "permission_denied");
  actor--;
  const retried = await runCli(args, env);
  assert.equal(retried.code, 0, retried.stdout);
  assert.equal(JSON.parse(retried.stdout).data.id, jobId);
  const posts = site.requests.filter(({ path }) => path.endsWith("/storyboard/split"));
  assert.equal(posts.length, 2);
  assert.deepEqual(posts[0]?.body, {
    node_id: "board",
    script: "剧本",
    mode: "append",
    request_key: "split-1",
    collaboration: { epoch: 3, revision: 8, max_credits: 12 },
  });
  assert.deepEqual(posts[1]?.body, posts[0]?.body);
  assert.equal(site.requests.filter(({ path }) => path.endsWith("/collaboration/ops")).length, 1);
  const waited = await runCli(["canvas", "storyboard", "wait", "c1", jobId, "--json"], env);
  assert.equal(waited.code, 0, waited.stdout);
  assert.equal(JSON.parse(waited.stdout).data.status, "succeeded");
});

test(
  "覆盖拆分镜确认绑定预览代数，提交要求服务端原子核对预览版本",
  { skip: !hasPty },
  async (context) => {
    let snapshots = 0;
    let epoch = 3;
    const site = await startSite(({ path, body }) => {
      if (path.endsWith("/collaboration")) return snapshot(++snapshots === 1 ? 3 : epoch);
      if (path.endsWith("/me")) return ok({ id: 1 });
      if (path.endsWith("/collaboration/ops")) return ok({ epoch, opId: body.opId, revision: 8 });
      if (path.endsWith("/storyboard/split")) return ok({ id: jobId, status: "pending" });
    });
    context.after(() => site.close());
    const root = tempConfig({ base: site.base, token: "t" });
    context.after(() => fs.rmSync(root, { recursive: true, force: true }));
    const script = path.join(root, "script.txt");
    fs.writeFileSync(script, "剧本");
    const env = { XDG_CONFIG_HOME: root };
    const args = [
      "canvas",
      "storyboard",
      "split",
      "c1",
      "board",
      "--script-file",
      script,
      "--mode",
      "replace",
      "--max-credits",
      "12",
    ];
    const rejected = await runCli([...args, "--json"], env);
    assert.equal(rejected.code, 2);
    assert.equal(site.requests.length, 0);
    epoch = 4;
    const changed = await runInPty(args, env, "yes\n");
    assert.equal(changed.code, 1, changed.stdout);
    assert.ok(!site.requests.some(({ method }) => method === "POST"));
    epoch = 3;
    const confirmed = await runInPty(args, env, "yes\n");
    assert.equal(confirmed.code, 0, confirmed.stdout);
    const edit = site.requests.find(({ path }) => path.endsWith("/collaboration/ops"));
    assert.equal(edit?.body.requireBaseRevision, true);
    assert.equal(edit?.body.baseRevision, 7);
  },
);

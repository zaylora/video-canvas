import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import test from "node:test";

import { ok, runCli, startSite, tempConfig } from "./helpers/site.ts";

test("协作生成在重试时复用原输入确认和付费绑定", async (context) => {
  let revision = 7;
  let cumulativeBudget = false;
  let submissions = 0;
  const site = await startSite(({ path, body }) => {
    if (path.endsWith("/collaboration"))
      return ok({
        epoch: 3,
        role: "editor",
        canGenerate: true,
        generationBudgetLimit: true,
        cumulativeBudget,
        canvas: { id: "c1", revision },
      });
    if (path.endsWith("/me")) return ok({ id: 1 });
    if (path.endsWith("/collaboration/ops")) return ok({ epoch: 3, opId: body.opId, revision: 8 });
    if (path.endsWith("/generation-tasks")) {
      submissions++;
      if (submissions === 1) return { status: 502, body: { code: -1, msg: "响应丢失" } };
      return ok({ items: [{ task: { id: 42, status: "pending" } }] });
    }
  });
  context.after(() => site.close());
  const env = { XDG_CONFIG_HOME: tempConfig({ base: site.base, token: "t" }) };
  const args = [
    "generate",
    "image",
    "--model",
    "m",
    "--canvas",
    "c1",
    "--node",
    "shot",
    "--max-credits",
    "10",
    "--budget-key",
    "episode-a",
    "--budget-credits",
    "40",
    "--idempotency-key",
    "gen-1",
    "--json",
  ];
  const unsupported = await runCli([...args, "--prompt", "初稿"], env);
  assert.equal(unsupported.code, 1, unsupported.stdout);
  assert.equal(JSON.parse(unsupported.stdout).error.reason, "unsupported");
  assert.ok(!site.requests.some(({ method }) => method === "POST"));
  cumulativeBudget = true;
  const first = await runCli([...args, "--prompt", "初稿"], env);
  assert.equal(first.code, 1, first.stdout);
  assert.equal(JSON.parse(first.stdout).error.reason, "result_unknown");
  revision = 9;
  const changed = await runCli([...args, "--prompt", "新稿"], env);
  assert.equal(changed.code, 1, changed.stdout);
  assert.equal(JSON.parse(changed.stdout).error.reason, "op_id_reused");
  const second = await runCli([...args, "--prompt", "初稿"], env);
  assert.equal(second.code, 0, second.stdout);
  const confirmed = site.requests.filter(({ path }) => path.endsWith("/collaboration/ops"));
  assert.equal(confirmed.length, 1);
  assert.equal(confirmed[0]?.body.baseRevision, 7);
  assert.deepEqual(confirmed[0]?.body.changes, [
    {
      kind: "node",
      id: "shot",
      fields: { "data.generationInput": { model: "m", input: { prompt: "初稿" } } },
    },
  ]);
  const submitted = site.requests.filter(({ path }) => path.endsWith("/generation-tasks"));
  assert.equal(submitted.length, 2);
  assert.deepEqual(submitted[0]?.body.collaboration, {
    epoch: 3,
    revision: 8,
    max_credits: 10,
    budget: { key: "episode-a", limit: 40 },
  });
  assert.deepEqual(submitted[1]?.body, submitted[0]?.body);
  assert.ok(submitted.every(({ headers }) => headers["idempotency-key"] === "gen-1"));
});

test("角色音色在角色节点确认输入，实际音频任务使用音色绑定", async (context) => {
  let role = "viewer";
  const site = await startSite(({ path, body }) => {
    if (path.endsWith("/collaboration"))
      return ok({
        epoch: 3,
        role,
        canGenerate: true,
        generationBudgetLimit: true,
        canvas: {
          id: "c1",
          revision: 7,
          payload_json: { nodes: [{ id: "actor", data: { kind: "character" } }] },
        },
      });
    if (path.endsWith("/me")) return ok({ id: 1 });
    if (path.endsWith("/collaboration/ops")) return ok({ epoch: 3, opId: body.opId, revision: 8 });
    if (path.endsWith("/generation-tasks"))
      return ok({ items: [{ task: { id: 42, status: "pending" } }] });
  });
  context.after(() => site.close());
  const env = { XDG_CONFIG_HOME: tempConfig({ base: site.base, token: "t" }) };
  const args = [
    "canvas",
    "character",
    "generate",
    "c1",
    "actor",
    "--kind",
    "voice",
    "--model",
    "voice-model",
    "--prompt",
    "试音",
    "--max-credits",
    "9",
    "--json",
  ];
  const forbidden = await runCli(args, env);
  assert.equal(JSON.parse(forbidden.stdout).error.reason, "permission_denied");
  assert.ok(!site.requests.some(({ method }) => method === "POST"));
  role = "editor";
  const generated = await runCli(args, env);
  assert.equal(generated.code, 0, generated.stdout);
  const confirm = site.requests.find(({ path }) => path.endsWith("/collaboration/ops"));
  assert.deepEqual(confirm?.body.changes, [
    {
      kind: "node",
      id: "actor",
      fields: { "data.generationInput": { model: "voice-model", input: { prompt: "试音" } } },
    },
  ]);
  const submit = site.requests.find(({ path }) => path.endsWith("/generation-tasks"));
  assert.equal(submit?.body.node_id, "voice:actor");
  assert.equal(submit?.body.kind, "audio");
  assert.deepEqual(submit?.body.collaboration, { epoch: 3, revision: 8, max_credits: 9 });
});

test("角色组合任务在音色响应丢失后按固定预算和图片绑定恢复", async (context) => {
  let revision = 7;
  let imageReady = false;
  let voiceSubmits = 0;
  const site = await startSite(({ path, body }) => {
    if (path.endsWith("/me")) return ok({ id: 1 });
    if (path.endsWith("/collaboration"))
      return ok({
        epoch: 3,
        role: "editor",
        canGenerate: true,
        generationBudgetLimit: true,
        canvas: {
          id: "c1",
          revision,
          payload_json: {
            nodes: [
              {
                id: "actor",
                data: {
                  kind: "character",
                  ...(imageReady && {
                    assetId: "42",
                    characterImageTaskId: "12",
                    characterImageAssetId: "42",
                  }),
                },
              },
            ],
          },
        },
      });
    if (path.endsWith("/collaboration/ops"))
      return ok({ epoch: 3, opId: body.opId, revision: ++revision });
    if (path.endsWith("/generation-tasks")) {
      if (body.kind === "image") return ok({ items: [{ task: { id: 12, status: "pending" } }] });
      if (++voiceSubmits === 1) return { status: 502, body: { code: -1, msg: "响应丢失" } };
      return ok({ items: [{ task: { id: 13, status: "pending" } }] });
    }
    if (path.endsWith("/generation-tasks/12")) {
      imageReady = true;
      return ok({ id: 12, status: "succeeded" });
    }
  });
  context.after(() => site.close());
  const root = tempConfig({ base: site.base, token: "t" });
  context.after(() => fs.rmSync(root, { recursive: true, force: true }));
  const imageInput = path.join(root, "image.json");
  const voiceInput = path.join(root, "voice.json");
  fs.writeFileSync(imageInput, JSON.stringify({ prompt: "角色正面形象" }));
  fs.writeFileSync(voiceInput, JSON.stringify({ prompt: "试音" }));
  const args = [
    "canvas",
    "character",
    "generate",
    "c1",
    "actor",
    "--kind",
    "image-and-voice",
    "--image-model",
    "image-model",
    "--voice-model",
    "voice-model",
    "--image-input",
    imageInput,
    "--voice-input",
    voiceInput,
    "--voice-from-image",
    "--max-credits",
    "18",
    "--image-max-credits",
    "11",
    "--idempotency-key",
    "workflow-a",
    "--json",
  ];
  const env = { XDG_CONFIG_HOME: root };
  const first = await runCli(args, env);
  assert.equal(JSON.parse(first.stdout).error.reason, "result_unknown");
  assert.equal(JSON.parse(first.stdout).error.details.idempotencyKey, "workflow-a");
  const replayed = await runCli(args, env);
  assert.equal(replayed.code, 0, replayed.stdout);
  assert.equal(JSON.parse(replayed.stdout).data.voice.id, 13);
  const posts = site.requests.filter(({ path }) => path.endsWith("/generation-tasks"));
  assert.equal(posts.filter(({ body }) => body.kind === "image").length, 1);
  const voicePosts = posts.filter(({ body }) => body.kind === "audio");
  assert.equal(voicePosts.length, 2);
  assert.deepEqual(voicePosts[0]?.body, voicePosts[1]?.body);
  assert.deepEqual(voicePosts[0]?.body.input, { prompt: "试音", images: [42] });
  assert.equal(voicePosts[0]?.body.collaboration.max_credits, 7);
  assert.equal(site.requests.filter(({ path }) => path.endsWith("/collaboration/ops")).length, 2);
});

import assert from "node:assert/strict";
import { writeFileSync } from "node:fs";
import { join } from "node:path";
import test from "node:test";

import { ok, runCli, startSite, tempConfig } from "./helpers/site.ts";

test("scoped credentials only leave the process for their own canvas on their own site", async (context) => {
  const key = `vca_${"1".repeat(64)}`;
  const route = "/api/v1/canvas-automation/c1/collaboration";
  const site = await startSite(({ path }) => {
    if (path === route) {
      return ok({ epoch: 2, role: "editor", canvas: { id: "c1", revision: 4 } });
    }
  });
  context.after(() => site.close());
  const root = tempConfig({ base: site.base, token: "normal-session" });
  const file = join(root, "automation.json");
  writeFileSync(file, JSON.stringify({ base: site.base, canvasId: "c1", token: key }), {
    mode: 0o600,
  });
  const env = { XDG_CONFIG_HOME: root, VC_AUTOMATION_CREDENTIALS: file };
  const result = await runCli(["canvas", "status", "c1", "--json"], env);
  assert.equal(result.code, 0, result.stdout + result.stderr);
  assert.equal(site.requests[0]?.headers.authorization, `Bearer ${key}`);
  for (const args of [
    ["canvas", "status", "c2"],
    ["canvas", "status", "c1", "--base", "http://127.0.0.1:1"],
    ["whoami"],
  ]) {
    const denied = await runCli([...args, "--json"], env);
    assert.notEqual(denied.code, 0, args.join(" "));
    assert.ok(!(denied.stdout + denied.stderr).includes(key));
  }
  assert.equal(site.requests.length, 1);
  assert.ok(!(result.stdout + result.stderr).includes(key));
});

test("paid automation submits and waits through canvas-scoped routes", async (context) => {
  const key = `vca_${"2".repeat(64)}`;
  const route = "/api/v1/canvas-automation/c1/collaboration";
  const task = { id: 11, node_id: "n1", status: "succeeded", credits: 1 };
  const site = await startSite(({ path, method, body }) => {
    if (path === route)
      return ok({
        epoch: 2,
        role: "editor",
        canGenerate: true,
        generationBudgetLimit: true,
        automationActor: "7",
        canvas: { id: "c1", revision: 4 },
      });
    if (path === `${route}/ops` && method === "POST")
      return ok({ epoch: 2, revision: 5, opId: (body as { opId: string }).opId });
    if (path === `${route}/generation-tasks` && method === "POST")
      return ok({ items: [{ node_id: "n1", task }] });
    if (path === `${route}/generation-tasks/11`) return ok(task);
  });
  context.after(() => site.close());
  const root = tempConfig({ base: site.base, token: "normal-session" });
  const file = join(root, "automation.json");
  writeFileSync(file, JSON.stringify({ base: site.base, canvasId: "c1", token: key }), {
    mode: 0o600,
  });
  const env = { XDG_CONFIG_HOME: root, VC_AUTOMATION_CREDENTIALS: file };
  const result = await runCli(
    [
      "generate",
      "audio",
      "--canvas",
      "c1",
      "--node",
      "n1",
      "--model",
      "audio-1",
      "--prompt",
      "hello",
      "--max-credits",
      "2",
      "--idempotency-key",
      "paid-1",
      "--wait",
      "--json",
    ],
    env,
  );
  assert.equal(result.code, 0, result.stdout + result.stderr);
  assert.ok(site.requests.some((entry) => entry.path === `${route}/generation-tasks/11`));
  const submitted = site.requests.find((entry) => entry.path === `${route}/generation-tasks`);
  assert.equal(submitted?.headers["idempotency-key"], "paid-1");
  assert.ok(site.requests.every((entry) => entry.headers.authorization === `Bearer ${key}`));
});

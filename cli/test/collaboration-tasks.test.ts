import assert from "node:assert/strict";
import fs from "node:fs";
import test from "node:test";

import { ok, runCli, startSite, tempConfig } from "./helpers/site.ts";

test("编辑者可在 CLI 读取与监听专项任务摘要，查看者不能请求摘要", async (context) => {
  let role = "editor";
  const items = [
    {
      id: "ff",
      nodeId: "board",
      kind: "storyboard-split",
      status: "running",
      username: "编导",
      updatedAt: "2026-11-28T12:00:00Z",
    },
  ];
  const site = await startSite(({ path, query }) => {
    if (path.endsWith("/collaboration"))
      return ok({
        epoch: 3,
        role,
        canGenerate: false,
        canCopyToLibrary: false,
        canvas: { id: "c1", revision: 7 },
        capabilities: { actions: role === "editor" ? ["task-summaries"] : [] },
      });
    if (path.endsWith("/events")) return ok({ epoch: 3, revision: 7, cursor: 7, role, events: [] });
    if (path.endsWith("/tasks")) {
      assert.equal(query.get("epoch"), "3");
      return ok({ items });
    }
  });
  context.after(() => site.close());
  const root = tempConfig({ base: site.base, token: "t" });
  context.after(() => fs.rmSync(root, { recursive: true, force: true }));
  const env = { XDG_CONFIG_HOME: root };
  const list = await runCli(["canvas", "task", "list", "c1", "--json"], env);
  assert.equal(list.code, 0, list.stdout);
  assert.deepEqual(JSON.parse(list.stdout).data.items, items);
  const watch = await runCli(["canvas", "watch", "c1", "--tasks", "--once", "--jsonl"], env);
  assert.equal(watch.code, 0, `${watch.stdout} ${watch.stderr}`);
  const records = watch.stdout
    .trim()
    .split("\n")
    .map((line) => JSON.parse(line));
  assert.deepEqual(records.find((record) => record.type === "task-summaries")?.data.items, items);
  role = "viewer";
  const viewer = await runCli(["canvas", "task", "list", "c1", "--json"], env);
  assert.notEqual(viewer.code, 0);
  assert.equal(site.requests.filter(({ path }) => path.endsWith("/tasks")).length, 2);
});

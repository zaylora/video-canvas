import assert from "node:assert/strict";
import test from "node:test";

import { hasPty, ok, runCli, runInPty, startSite, tempConfig } from "./helpers/site.ts";

test("协作评论、提及和版本活动沿用当前会话", async (context) => {
  const site = await startSite(({ path }) => {
    if (path.endsWith("/collaboration"))
      return ok({ role: "editor", epoch: 3, canvas: { id: "c1", revision: 7 } });
    if (path.endsWith("/comments")) return ok({ authorUsername: "导演" });
    if (path.endsWith("/history") || path.endsWith("/mentions")) return ok({ items: [] });
  });
  context.after(() => site.close());
  const env = { XDG_CONFIG_HOME: tempConfig({ base: site.base, token: "t" }) };
  const comment = await runCli(
    [
      "canvas",
      "comment",
      "add",
      "c1",
      "--node",
      "shot",
      "--text",
      "核对镜头",
      "--mention",
      "a1",
      "--op-id",
      "note-1",
      "--json",
    ],
    env,
  );
  assert.equal(comment.code, 0, comment.stdout);
  assert.equal(JSON.parse(comment.stdout).data.authorUsername, "导演");
  assert.deepEqual(site.requests.find(({ method }) => method === "POST")?.body, {
    epoch: 3,
    nodeId: "shot",
    body: "核对镜头",
    opId: "note-1",
    mentions: ["a1"],
  });
  for (const command of [
    ["history", "list", "c1"],
    ["mention", "list", "c1"],
  ]) {
    const result = await runCli(["canvas", ...command, "--json"], env);
    assert.equal(result.code, 0, result.stdout);
    assert.equal(site.requests.at(-1)?.query.get("epoch"), "3");
  }
});

test("撤销提交明确目标，拒绝意外撤销后来发生的操作", { skip: !hasPty }, async (context) => {
  const site = await startSite(({ path, body }) => {
    if (path.endsWith("/collaboration"))
      return ok({ role: "editor", epoch: 3, canvas: { id: "c1", revision: 7 } });
    if (path.endsWith("/undo"))
      return ok({
        epoch: 3,
        opId: body.opId,
        revision: 8,
        conflicts: [{ field: "expectedRevision", revision: 7 }],
      });
  });
  context.after(() => site.close());
  const env = { XDG_CONFIG_HOME: tempConfig({ base: site.base, token: "t" }) };
  const result = await runInPty(
    ["canvas", "undo", "c1", "--target", "6", "--op-id", "undo-1"],
    env,
    "yes\n",
  );
  assert.equal(result.code, 1);
  assert.match(result.stdout + result.stderr, /最新可操作版本 7/);
  assert.deepEqual(site.requests.at(-1)?.body, {
    opId: "undo-1",
    epoch: 3,
    expectedRevision: 6,
    redo: false,
  });
});

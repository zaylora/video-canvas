import assert from "node:assert/strict";
import test from "node:test";

import { ok, runCli, startSite, tempConfig } from "./helpers/site.ts";

test("canvas compose preserves selected ordering and can resume or cancel a job", async () => {
  const route = "/api/v1/canvas/c1/collaboration";
  const site = await startSite(({ method, path }) => {
    if (path === route) {
      return ok({
        epoch: 2,
        role: "editor",
        canvas: { id: "c1", revision: 4 },
        capabilities: { actions: ["compose"] },
      });
    }
    if (path === `${route}/compose` && method === "POST") {
      return ok({ id: "job1", status: "pending", segment_count: 2 });
    }
    if (path === `${route}/compose/job1`) {
      return ok({ id: "job1", status: method === "DELETE" ? "canceled" : "succeeded" });
    }
  });
  try {
    const env = { XDG_CONFIG_HOME: tempConfig({ base: site.base, token: "token" }) };
    const given = ["c1", "--epoch", "2"];
    const submitted = await runCli(
      [
        "canvas",
        "compose",
        ...given,
        "--revision",
        "3",
        "--op-id",
        "render",
        "--nodes",
        "v2,v1",
        "--wait",
        "--json",
      ],
      env,
    );
    assert.equal(submitted.code, 0, submitted.stderr);
    assert.deepEqual(site.requests[1]?.body, {
      epoch: 2,
      revision: 3,
      opId: "render",
      nodes: ["v2", "v1"],
    });
    for (const [sub, method] of [
      ["compose-status", "GET"],
      ["compose-cancel", "DELETE"],
    ]) {
      const result = await runCli(["canvas", sub!, ...given, "--job", "job1", "--json"], env);
      assert.equal(result.code, 0, result.stderr);
      assert.equal(site.requests.at(-1)?.method, method);
      assert.equal(site.requests.at(-1)?.query.get("epoch"), "2");
    }
  } finally {
    await site.close();
  }
});

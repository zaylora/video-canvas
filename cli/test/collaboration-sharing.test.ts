import assert from "node:assert/strict";
import test from "node:test";

import { hasPty, ok, runCli, runInPty, startSite, tempConfig } from "./helpers/site.ts";

test("重命名以已确认版本提交且不发送归档参数", async (context) => {
  const site = await startSite(({ path, method }) => {
    if (method === "GET" && path === "/api/v1/canvas/c1")
      return ok({ id: "c1", title: "镜头", revision: 7, collaboration_active: true });
    if (method === "POST" && path.endsWith("/management"))
      return ok({ id: "c1", title: "正式分镜", revision: 8, archived: false });
  });
  context.after(() => site.close());
  const env = { XDG_CONFIG_HOME: tempConfig({ base: site.base, token: "t" }) };
  const args = ["canvas", "rename", "c1", "--title", " 正式分镜 ", "--json"];
  const rejected = await runCli([...args, "--revision", "6"], env);
  assert.notEqual(rejected.code, 0);
  assert.ok(site.requests.every(({ method }) => method === "GET"));
  const renamed = await runCli(args, env);
  assert.equal(renamed.code, 0, renamed.stdout + renamed.stderr);
  assert.equal(JSON.parse(renamed.stdout).data.title, "正式分镜");
  assert.deepEqual(site.requests.at(-1)?.body, { revision: 7, title: "正式分镜" });
});

test("归档确认带上观察版本，取消无副作用", { skip: !hasPty }, async (context) => {
  const preview = "a".repeat(64);
  const site = await startSite(({ path, method }) => {
    if (method === "GET" && path === "/api/v1/canvas/c1")
      return ok({ id: "c1", title: "镜头", revision: 7, collaboration_active: true });
    if (method === "GET" && path.endsWith("/management"))
      return ok({ title: "镜头", revision: 7, preview, editors: 1, viewers: 2 });
    if (method === "POST" && path.endsWith("/management")) return ok({ archived: true });
  });
  context.after(() => site.close());
  const env = { XDG_CONFIG_HOME: tempConfig({ base: site.base, token: "t" }) };
  const args = ["canvas", "archive", "c1"];
  const refused = await runInPty(args, env, "no\n");
  assert.equal(refused.code, 4, refused.stdout + refused.stderr);
  assert.ok(site.requests.every(({ method }) => method === "GET"));
  const archived = await runInPty(args, env, "yes\n");
  assert.equal(archived.code, 0, archived.stdout + archived.stderr);
  assert.match(archived.stdout + archived.stderr, /1 位编辑者、2 位查看者/);
  assert.deepEqual(site.requests.at(-1)?.body, { revision: 7, archive: true, preview });
});

test("退出共享只在确认后撤销自己的访问", { skip: !hasPty }, async (context) => {
  const site = await startSite(({ path, method }) => {
    if (path.endsWith("/collaboration"))
      return ok({ role: "editor", epoch: 3, canvas: { id: "c1", title: "镜头", revision: 7 } });
    if (method === "DELETE" && path.endsWith("/shares/me")) return ok({});
  });
  context.after(() => site.close());
  const env = { XDG_CONFIG_HOME: tempConfig({ base: site.base, token: "t" }) };
  const args = ["canvas", "share", "leave", "c1"];
  const refused = await runInPty(args, env, "no\n");
  assert.equal(refused.code, 4);
  assert.match(refused.stdout + refused.stderr, /重新邀请/);
  assert.ok(site.requests.every(({ method }) => method === "GET"));
  const result = await runInPty(args, env, "yes\n");
  assert.equal(result.code, 0, result.stdout + result.stderr);
  assert.equal(site.requests.at(-1)?.path, "/api/v1/canvas/c1/shares/me");
  assert.equal(site.requests.at(-1)?.method, "DELETE");
});

test("关闭协作按服务端预览确认并发送同一版本", { skip: !hasPty }, async (context) => {
  const preview = "b".repeat(64);
  const site = await startSite(({ path, method, query }) => {
    if (method === "GET" && path === "/api/v1/canvas/c1")
      return ok({ id: "c1", title: "镜头", revision: 7, collaboration_active: true });
    if (method === "GET" && path.endsWith("/mode") && query.get("active") === "false")
      return ok({ title: "镜头", revision: 9, preview, editors: 2, viewers: 1, active: false });
    if (method === "PUT" && path.endsWith("/mode")) return ok({ active: false });
  });
  context.after(() => site.close());
  const env = { XDG_CONFIG_HOME: tempConfig({ base: site.base, token: "t" }) };
  const args = ["canvas", "collaboration", "disable", "c1"];
  const refused = await runInPty(args, env, "no\n");
  assert.equal(refused.code, 4, refused.stdout + refused.stderr);
  assert.ok(site.requests.every(({ method }) => method === "GET"));
  const result = await runInPty(args, env, "yes\n");
  assert.equal(result.code, 0, result.stdout + result.stderr);
  assert.match(result.stdout + result.stderr, /2 位编辑者.*1 位查看者/);
  assert.deepEqual(site.requests.at(-1)?.body, { active: false, revision: 9, preview });
});

test("交付固定确认版本，转移后可查询最小收据", { skip: !hasPty }, async (context) => {
  const site = await startSite(({ path, method }) => {
    if (path === "/api/v1/canvas/c1") return ok({ id: "c1", title: "镜头", revision: 7 });
    if (method === "POST" && path.endsWith("/shares")) return ok({});
    if (path.endsWith("/deliveries/send-1")) return ok({ status: "succeeded" });
  });
  context.after(() => site.close());
  const env = { XDG_CONFIG_HOME: tempConfig({ base: site.base, token: "t" }) };
  const result = await runInPty(
    ["canvas", "share", "transfer", "c1", "--email", "tester@example.test", "--op-id", "send-1"],
    env,
    "yes\n",
  );
  assert.equal(result.code, 0, result.stdout + result.stderr);
  assert.deepEqual(site.requests.at(-1)?.body, {
    email: "tester@example.test",
    mode: "transfer",
    request_key: "send-1",
    revision: 7,
  });
  const receipt = await runCli(["canvas", "share", "receipt", "c1", "send-1", "--json"], env);
  assert.equal(receipt.code, 0, receipt.stdout);
  assert.equal(JSON.parse(receipt.stdout).data.status, "succeeded");
});

import assert from "node:assert/strict";
import { mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { createServer } from "node:http";
import { DatabaseSync } from "node:sqlite";
import { test } from "node:test";
import { openNodeSqliteStorage } from "@earendil-works/pi-durable/storage/sqlite/node";
import { AgentEngine } from "../src/engine/engine.ts";
import { createPlatform } from "../src/platform-client.ts";
import { replayOf } from "../src/durable-tools.ts";
import { openStore } from "../src/store/store.ts";
import { fakeGo } from "./fake-go.ts";
import { codec, TOKEN } from "./engine-helpers.ts";
import { withHttp } from "./http-helpers.ts";

test("二期工具均可安全重放", () => {
  for (const name of [
    "admin_query",
    "admin_get_resource",
    "admin_list_plugins",
    "admin_get_plugin",
    "admin_plugin_check",
    "admin_request_secret",
    "admin_test_run",
  ])
    assert.equal(replayOf(name), "safe");
});

test("真实试跑先审批，决定立即返回，终态经事件和历史交付", async () => {
  let finish!: () => void;
  const terminal = new Promise<void>((r) => {
    finish = r;
  });
  let called = 0;
  await withHttp(
    {
      turns: [
        {
          tools: [
            {
              id: "trial",
              name: "admin_test_run",
              args: { model_key: "video", input: { prompt: "测试" }, reason: "测试平台" },
            },
          ],
        },
        { text: "试跑成功" },
      ],
      setup: {
        tools: [
          {
            name: "admin_test_run",
            label: "请你同意试跑",
            verb: "试跑",
            description: "试跑",
            parameters: { type: "object", properties: {} },
            executor: "server",
            scriptable: false,
          },
        ],
      },
      propose: {
        admin_test_run: {
          payload: { model_key: "video", attempt: 1, attempts_left: 2 },
          quote_credits: 0,
        },
      },
      execute: async () => {
        called++;
        await terminal;
        return {
          status: "executed",
          result: {
            test_run_id: "trial-id",
            status: "succeeded",
            output_url: "https://output.invalid/a.mp4",
          },
          outcome_text: "试跑成功；HTTP 200",
        };
      },
    },
    async (h, e) => {
      const headers = { "x-agent-role": "admin" };
      const sid = (await h.call("POST", "/admin/sessions", {}, headers)).body.data.id;
      const run = await h.call(
        "POST",
        `/agent/sessions/${sid}/runs`,
        { message: "测试平台" },
        headers,
      );
      await e.engine.idle();
      const [approval] = e.store.listApprovals(codec.decodeDecimal(run.body.data.id)!);
      assert.equal(approval.kind, "admin_test_run");
      assert.equal(called, 0);
      const deciding = h.call(
        "POST",
        `/agent/approvals/${codec.encodeDecimal(approval.id)}/decision`,
        { decision: "approve" },
        headers,
      );
      try {
        const reply = await Promise.race([
          deciding,
          new Promise<"blocked">((r) => {
            const timer = setTimeout(() => r("blocked"), 500);
            timer.unref();
          }),
        ]);
        assert.notEqual(reply, "blocked", "不能让浏览器等待十分钟而被代理超时");
        if (reply === "blocked") return;
        assert.equal(reply.status, 200);
        assert.equal(reply.body.data.status, "approved");
        assert.equal(reply.body.data.result, null);
      } finally {
        finish();
        await deciding;
      }
      await e.engine.idle();
      assert.equal(called, 1);
      assert.equal(
        (e.store.getApproval("1", approval.id).result as { status: string }).status,
        "succeeded",
      );
      const event = e.store
        .listEvents(codec.decodeDecimal(sid)!, 0, 1000)
        .filter((x) => x.type === "approval.decided")
        .at(-1)!;
      assert.equal((event.payload as { result: { status: string } }).result.status, "succeeded");
    },
  );
});

test("密钥直接保存后，全 SQLite、事件、审批和模型请求均不含特征密钥", async () => {
  const sentinel = "P2-SECRET-ZERO-LEAK-7fc09a81";
  let configured = false;
  const channel = createServer(async (req, res) => {
    let text = "";
    for await (const chunk of req) text += chunk;
    assert.equal(req.method, "PUT");
    assert.equal(JSON.parse(text).value, sentinel);
    configured = true;
    res.end("{}");
  });
  await new Promise<void>((r) => channel.listen(0, "127.0.0.1", r));
  const dir = mkdtempSync(join(tmpdir(), "admin-secret-"));
  const file = join(dir, "agent.sqlite");
  const g = await fakeGo({
    turns: [
      {
        tools: [
          {
            id: "secret",
            name: "admin_request_secret",
            args: { channel_key: "main", reason: "平台鉴权" },
          },
        ],
      },
      { text: "密钥已配置" },
    ],
    setup: {
      tools: [
        {
          name: "admin_request_secret",
          label: "请你填写密钥",
          verb: "配密钥",
          description: "密钥卡片",
          parameters: { type: "object", properties: {} },
          executor: "server",
          scriptable: false,
        },
      ],
    },
    propose: {
      admin_secret: {
        payload: {
          channel_key: "main",
          channel_label: "平台",
          exists: true,
          secret_configured: false,
          reason: "平台鉴权",
          required_role: "super_admin",
        },
        quote_credits: 0,
      },
    },
    resolve: () => ({
      status: "approved",
      decision: { decision: "approve", answer: "configured" },
      quote_credits: 0,
    }),
    execute: () => ({
      status: configured ? "executed" : "failed",
      result: { configured },
      outcome_text: configured ? "密钥已配置" : "密钥未保存",
    }),
  });
  const store = openStore(file, { workerId: 1 });
  const engine = await AgentEngine.open({
    store,
    platform: createPlatform({ baseUrl: g.url, serviceToken: TOKEN }),
    baseUrl: g.url,
    serviceToken: TOKEN,
    storage: await openNodeSqliteStorage(file, { prefix: "pi_" }),
    codec,
    autoTitle: false,
    activitySummary: false,
  });
  try {
    const session = store.createSession({
      userId: "1",
      canvasId: "0",
      mode: "admin",
      title: "配置渠道",
      modelKey: "claude",
    });
    const run = await engine.startRun({
      userId: "1",
      sessionId: session.id,
      message: "配置渠道",
      modelKey: "claude",
    });
    await engine.idle();
    const [approval] = store.listApprovals(run.id);
    assert.equal(approval.kind, "admin_secret");
    await assert.rejects(
      engine.decide("1", approval.id, { decision: "approve", answer: sentinel }),
      /只接受/,
    );
    const port = (channel.address() as { port: number }).port;
    await fetch(`http://127.0.0.1:${port}/admin/ai/channels/main/secret`, {
      method: "PUT",
      body: JSON.stringify({ value: sentinel }),
    });
    const result = await engine.decide("1", approval.id, {
      decision: "approve",
      answer: "configured",
    });
    assert.deepEqual(result.result, { configured: true });
    await engine.idle();
    assert.ok(!JSON.stringify(g.seen).includes(sentinel), "Go 平台调用和模型请求不应收到密钥");
    const db = new DatabaseSync(file);
    try {
      const tables = db
        .prepare("SELECT name FROM sqlite_master WHERE type='table'")
        .all() as Array<{ name: string }>;
      for (const { name } of tables) {
        const query = db.prepare(`SELECT * FROM "${name.replaceAll('"', '""')}"`);
        query.setReadBigInts(true);
        const rows = query.all();
        const text = JSON.stringify(rows, (_, value) =>
          typeof value === "bigint" ? value.toString() : value,
        );
        assert.ok(!text.includes(sentinel), `${name} 泄露密钥`);
      }
      assert.ok(tables.some((x) => x.name === "agent_events"));
      assert.ok(tables.some((x) => x.name === "agent_approvals"));
      assert.ok(tables.some((x) => x.name === "pi_entries"));
    } finally {
      db.close();
    }
  } finally {
    await engine.close();
    store.close();
    await g.close();
    await new Promise<void>((r) => channel.close(() => r()));
    rmSync(dir, { recursive: true, force: true });
  }
});

test("试跑批准后重启，恢复原审批并对账试跑结果", async () => {
  const dir = mkdtempSync(join(tmpdir(), "admin-trial-recovery-"));
  const file = join(dir, "agent.sqlite");
  let executions = 0;
  const g = await fakeGo({
    turns: [
      {
        tools: [
          {
            id: "trial",
            name: "admin_test_run",
            args: { model_key: "video", input: { prompt: "测试" }, reason: "测试" },
          },
        ],
      },
      { text: "试跑成功" },
    ],
    setup: {
      tools: [
        {
          name: "admin_test_run",
          label: "请你同意试跑",
          verb: "试跑",
          description: "试跑",
          parameters: { type: "object", properties: {} },
          executor: "server",
          scriptable: false,
        },
      ],
    },
    propose: { admin_test_run: { payload: { model_key: "video", attempt: 1 }, quote_credits: 0 } },
    execute: () => {
      executions++;
      return {
        status: "executed",
        result: { status: "succeeded", test_run_id: "original-task" },
        outcome_text: "原任务已成功",
      };
    },
  });
  const store = openStore(file, { workerId: 1 });
  const open = async () =>
    AgentEngine.open({
      store,
      platform: createPlatform({ baseUrl: g.url, serviceToken: TOKEN }),
      baseUrl: g.url,
      serviceToken: TOKEN,
      storage: await openNodeSqliteStorage(file, { prefix: "pi_" }),
      codec,
      autoTitle: false,
      activitySummary: false,
    });
  let engine = await open();
  try {
    const session = store.createSession({
      userId: "1",
      canvasId: "0",
      mode: "admin",
      title: "接入",
      modelKey: "claude",
    });
    const run = await engine.startRun({
      userId: "1",
      sessionId: session.id,
      message: "测试平台",
      modelKey: "claude",
    });
    await engine.idle();
    const [approval] = store.listApprovals(run.id);
    // 模拟决定 CAS 已落库、尚未执行 Go 请求时进程退出的窗口。
    store.updateApprovalIf(approval.id, ["pending"], {
      status: "approved",
      decision: { decision: "approve" },
      decidedAt: Date.now(),
    });
    store.updateRunIf(run.id, ["waiting_approval"], { status: "running" });
    await engine.close();
    engine = await open();
    await engine.idle();
    assert.equal(executions, 1);
    assert.deepEqual(store.getApproval("1", approval.id).result, {
      status: "succeeded",
      test_run_id: "original-task",
    });
    assert.equal(store.getRun("1", run.id).status, "succeeded");
  } finally {
    await engine.close();
    store.close();
    await g.close();
    rmSync(dir, { recursive: true, force: true });
  }
});

import assert from "node:assert/strict";
import { test } from "node:test";
import { openStore } from "../src/store/store.ts";
import { DuplicateError } from "../src/store/errors.ts";
import { encodeRunContext } from "../src/platform-client.ts";
import { withHttp } from "./http-helpers.ts";
import { codec } from "./engine-helpers.ts";

test("后台运行按用户互斥，画布仍按画布互斥", () => {
  const s = openStore(":memory:", { workerId: 1 });
  try {
    const create = (userId: string, canvasId: string) => {
      const session = s.createSession({
        userId,
        canvasId,
        mode: canvasId === "0" ? "admin" : "all",
        title: "",
        modelKey: "",
      });
      return s.createRun({
        userId,
        canvasId,
        sessionId: session.id,
        mode: session.mode,
        status: "queued",
      });
    };
    create("1", "0");
    create("2", "0");
    assert.throws(() => create("1", "0"), DuplicateError);
    create("1", "7");
    assert.throws(() => create("2", "7"), DuplicateError);
  } finally {
    s.close();
  }
});

test("后台上下文只允许零画布", () => {
  const rc = { run_id: "1", user_id: "1", session_id: "1", canvas_id: "0", mode: "admin" };
  assert.doesNotThrow(() => encodeRunContext(rc));
  assert.throws(() => encodeRunContext({ ...rc, mode: "all" }));
  assert.throws(() => encodeRunContext({ ...rc, canvas_id: "7" }));
});

test("后台会话要求实时角色、只许本人访问，画布列表排除后台会话", () =>
  withHttp({ turns: [] }, async (h, e) => {
    assert.equal((await h.call("GET", "/admin/sessions")).status, 403);
    const headers = { "x-agent-role": "admin" };
    const made = await h.call("POST", "/admin/sessions", { title: "后台" }, headers);
    assert.equal(made.status, 200);
    assert.equal(made.body.data.mode, "admin");
    assert.equal(made.body.data.canvas_id, "0");
    const sid = made.body.data.id;
    assert.equal((await h.call("GET", `/agent/sessions/${sid}/history`)).status, 403);
    assert.equal(
      (await h.call("GET", `/agent/sessions/${sid}/history`, undefined, headers)).status,
      200,
    );
    assert.equal(
      (
        await h.call("GET", `/agent/sessions/${sid}/history`, undefined, {
          ...headers,
          "x-agent-user": "2",
        })
      ).status,
      404,
    );
    assert.equal((await h.call("GET", "/admin/sessions", undefined, headers)).body.data.length, 1);
    assert.equal(
      (
        await h.call(
          "POST",
          `/agent/sessions/${sid}/runs`,
          { message: "改价", mode: "all" },
          headers,
        )
      ).status,
      400,
    );
    e.store.createSession({
      userId: "1",
      canvasId: "7",
      title: "错误历史数据",
      mode: "admin",
      modelKey: "",
    });
    const canvas = codec.encode(7n);
    assert.ok(
      (await h.call("GET", `/canvas/${canvas}/agent/sessions`)).body.data.every(
        (x: { mode: string }) => x.mode !== "admin",
      ),
    );
    assert.ok(!e.go.paths().includes("POST /messages/check"));
  }));

test("后台运行忽略画布输入、透传页面上下文，部分执行与拒绝结果持久化", async () => {
  for (const status of ["partially_approved", "rejected"] as const) {
    const result = {
      items: [{ index: 0, status: status === "rejected" ? "rejected" : "applied" }],
      applied_by: "运营",
      applied_at: "2026-11-02T00:00:00Z",
      audit_ids: status === "rejected" ? [] : [1],
    };
    await withHttp(
      {
        turns: [
          { tools: [{ id: "admin_call", name: "admin_propose_changes", args: {} }] },
          { text: "已处理" },
        ],
        setup: {
          tools: [
            {
              name: "admin_propose_changes",
              label: "申请变更",
              verb: "修改",
              description: "申请变更",
              parameters: { type: "object", properties: {} },
              executor: "server",
              scriptable: false,
            },
          ],
        },
        propose: { admin_change: { payload: { items: [{ status: "ok" }] }, quote_credits: 0 } },
        execute: () => ({ status, result, outcome_text: "已处理" }),
      },
      async (h, e) => {
        const headers = { "x-agent-role": "admin" };
        const made = await h.call("POST", "/admin/sessions", {}, headers);
        const sid = made.body.data.id;
        const page_context = { route: "/admin/ai/channels", model_key: "test" };
        const started = await h.call(
          "POST",
          `/agent/sessions/${sid}/runs`,
          {
            message: "调整价格",
            selection: "忽略",
            attachments: "忽略",
            viewport: "忽略",
            page_context,
          },
          headers,
        );
        assert.equal(started.status, 202);
        await e.engine.idle();
        const runId = codec.decodeDecimal(started.body.data.id)!;
        const [approval] = e.store.listApprovals(runId);
        assert.equal(approval.kind, "admin_change");
        const events = e.store.listEvents(codec.decodeDecimal(sid)!, 0, 200);
        assert.equal(
          (events.find((x) => x.type === "approval.created")!.payload as { result: unknown })
            .result,
          null,
        );
        assert.deepEqual(
          e.go.seen.find((x) => x.path.endsWith("/runs/prepare"))!.body.page_context,
          page_context,
        );
        assert.ok(
          !e.go.paths().some((x) => /canvases\/check|messages\/check|attachments\/check/.test(x)),
        );
        const aid = codec.encodeDecimal(approval.id);
        assert.equal(
          (await h.call("POST", `/agent/approvals/${aid}/decision`, { decision: "approve" }))
            .status,
          403,
        );
        const decided = await h.call(
          "POST",
          `/agent/approvals/${aid}/decision`,
          { decision: status === "rejected" ? "reject" : "approve" },
          headers,
        );
        assert.equal(decided.status, 200);
        assert.equal(decided.body.data.status, status);
        assert.deepEqual(decided.body.data.result, result);
        assert.deepEqual(e.store.getApproval("1", approval.id).result, result);
      },
    );
  }
});

for (const unavailable of ["清单为空", "指定模型不可用"] as const) {
  const models = unavailable === "清单为空" ? [] : [{ key: "claude", name: "Claude" }];
  const modelKey = unavailable === "清单为空" ? "" : "missing";
  test(`新建后台会话${unavailable}返回 60002 / 400`, () =>
    withHttp({ turns: [], models }, async (h) => {
      const r = await h.call(
        "POST",
        "/admin/sessions",
        { model_key: modelKey },
        { "x-agent-role": "super_admin" },
      );
      assert.equal(r.body.code, 60002);
      assert.equal(r.status, 400);
      assert.equal(r.body.msg, "Agent 模型不可用");
    }));

  test(`后台发起运行${unavailable}返回 60002 / 400`, () =>
    withHttp({ turns: [], models }, async (h, e) => {
      const session = e.store.createSession({
        userId: "1",
        canvasId: "0",
        mode: "admin",
        title: "后台",
        modelKey: "claude",
      });
      const sid = codec.encodeDecimal(session.id);
      const r = await h.call(
        "POST",
        `/agent/sessions/${sid}/runs`,
        { message: "调整价格", agent_model_key: modelKey },
        { "x-agent-role": "admin" },
      );
      assert.equal(r.body.code, 60002);
      assert.equal(r.status, 400);
      assert.equal(r.body.msg, "Agent 模型不可用");
      assert.equal(e.store.listEvents(session.id, 0, 200).length, 0);
    }));
}

for (const scenario of ["后台带上下文", "后台未带上下文", "画布带上下文"] as const) {
  test(`用户消息事件推送、回放和历史：${scenario}`, () =>
    withHttp({ turns: [{ text: "已收到" }] }, async (h, e) => {
      const admin = scenario !== "画布带上下文";
      const headers = { "x-agent-role": "admin" };
      const sid = admin
        ? (await h.call("POST", "/admin/sessions", {}, headers)).body.data.id
        : codec.encodeDecimal(e.sessionId);
      const page_context = {
        route: "/admin/ai/channels",
        model_key: "模型 A",
        channel_key: "渠道 B",
        plugin_key: "插件 C",
      };
      const started = await h.call(
        "POST",
        `/agent/sessions/${sid}/runs`,
        {
          message: "查看配置",
          ...(scenario !== "后台未带上下文" && { page_context }),
        },
        headers,
      );
      assert.equal(started.status, 202);
      await e.engine.idle();

      const expected = {
        text: "查看配置",
        mode: admin ? "admin" : "all",
        selection: [],
        ...(scenario === "后台带上下文" && { page_context }),
      };
      const published = e.events.find(
        (ev) => ev.type === "message.user" && ev.sessionId === codec.decodeDecimal(sid),
      );
      assert.ok(published);
      assert.deepEqual(published.payload, expected);
      const stored = e.store
        .listEvents(codec.decodeDecimal(sid)!, 0, 200)
        .find((ev) => ev.type === "message.user");
      assert.ok(stored);
      assert.deepEqual(stored.payload, expected);

      const replay = await h.call(
        "GET",
        `/agent/sessions/${sid}/events?after=0`,
        undefined,
        headers,
      );
      const history = await h.call("GET", `/agent/sessions/${sid}/history`, undefined, headers);
      assert.equal(replay.status, 200);
      assert.equal(history.status, 200);
      for (const events of [replay.body.data, history.body.data.events]) {
        const message = events.find((ev: { type: string }) => ev.type === "message.user");
        assert.ok(message);
        assert.deepEqual(message.data, expected);
      }
    }));
}

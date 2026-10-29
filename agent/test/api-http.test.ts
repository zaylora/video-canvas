import assert from "node:assert/strict";
import { test } from "node:test";
import { codec } from "./engine-helpers.ts";
import { withHttp } from "./http-helpers.ts";

const enc = (id: string) => codec.encode(BigInt(id));

test("认证：没有或错误的服务令牌 401；没有用户 401；不认识的路径 404", () =>
  withHttp({ turns: [] }, async (h) => {
    for (const hdr of [{ "x-agent-service-token": undefined }, { "x-agent-service-token": "wrong-wrong-wrong-wrong-wrong-wrong" }]) {
      const r = await h.call("GET", "/agent/models", undefined, hdr);
      assert.deepEqual([r.status, r.body.code], [401, 10003]);
    }
    for (const user of [undefined, "", "abc", "0", "007"]) {
      const r = await h.call("GET", "/agent/models", undefined, { "x-agent-user": user });
      assert.equal(r.status, 401, `用户 ${JSON.stringify(user)}`);
    }
    const r = await h.call("GET", "/nope");
    assert.deepEqual([r.status, r.body.code], [404, 10002]);
  }));

test("成功的响应是 Go 的标准信封 {code:0, msg:'success', data}；没有数据时没有 data 字段；发起运行是 202", () =>
  withHttp({ turns: [{ text: "好" }] }, async (h, e) => {
    const m = await h.call("GET", "/agent/models");
    assert.deepEqual([m.status, m.body.code, m.body.msg], [200, 0, "success"]);
    assert.equal(m.body.data[0].key, "claude");
    const created = await h.call("POST", "/canvas/" + enc("7") + "/agent/sessions", { title: "角色" });
    assert.equal(created.status, 200);
    const sid = created.body.data.id;
    const listed = await h.call("GET", "/canvas/" + enc("7") + "/agent/sessions");
    assert.ok(listed.body.data.some((s: { id: string }) => s.id === sid));
    const renamed = await h.call("PATCH", "/agent/sessions/" + sid, { title: "新名" });
    assert.equal(renamed.body.data.title, "新名");
    const started = await h.call("POST", "/agent/sessions/" + enc(e.sessionId) + "/runs", { message: "拆分镜" });
    assert.deepEqual([started.status, started.body.code, started.body.data.status], [202, 0, "queued"]);
    await e.engine.idle();
    const events = await h.call("GET", "/agent/sessions/" + enc(e.sessionId) + "/events?after=0&limit=3");
    assert.equal(events.body.data.length, 3);
    const hist = await h.call("GET", "/agent/sessions/" + enc(e.sessionId) + "/history");
    assert.equal(typeof hist.body.data.has_more, "boolean");
    const del = await h.call("DELETE", "/agent/sessions/" + sid);
    assert.deepEqual(del.body, { code: 0, msg: "success" });
  }));

test("运行的控制：取消、继续、插话、撤销、审批决定都走对应的路径", () =>
  withHttp({
    turns: [{ tools: [{ id: "c1", name: "generate_media" }] }, { text: "好" }],
    propose: { generate: { payload: { items: [] }, quote_credits: 0, summary: "s", content: "已提交" } },
  }, async (h, e) => {
    const run = await e.start();
    await e.engine.idle();
    const [approval] = e.store.listApprovals(run.id);
    const rid = enc(run.id);
    const decided = await h.call("POST", "/agent/approvals/" + enc(approval.id) + "/decision", { decision: "approve" });
    assert.deepEqual([decided.status, decided.body.data.status, decided.body.data.id], [200, "executed", enc(approval.id)]);
    await e.engine.idle();
    const undone = await h.call("POST", "/agent/runs/" + rid + "/undo");
    assert.equal(undone.body.data.revision, 5);
    const gone = await h.call("POST", "/agent/runs/" + rid + "/cancel");
    assert.deepEqual([gone.status, gone.body.code], [409, 60003], "已结束的运行不能取消");
    const resumed = await h.call("POST", "/agent/runs/" + rid + "/resume", {});
    assert.deepEqual([resumed.status, resumed.body.code], [409, 60003]);
    const interject = await h.call("POST", "/agent/runs/" + rid + "/interject", { message: "x" });
    assert.deepEqual([interject.status, interject.body.code], [409, 60003]);
  }));

test("业务错误：错误码、HTTP 状态、文字原样；别人的资源 404；内部错误只说通用提示", () =>
  withHttp({ turns: [] }, async (h, e) => {
    const other = await h.call("GET", "/agent/sessions/" + enc(e.sessionId) + "/events", undefined, { "x-agent-user": "2" });
    assert.deepEqual([other.status, other.body.code, other.body.msg], [404, 60011, "会话不存在"]);
    const run = await h.call("POST", "/agent/runs/" + enc("999") + "/cancel");
    assert.deepEqual([run.status, run.body.code], [404, 60012]);
  }));

test("路径里的 id 不是合法的编码串：10001「xxx 格式错误」，不是 404", () =>
  withHttp({ turns: [] }, async (h) => {
    for (const [method, path, name] of [
      ["GET", "/agent/sessions/xyz/events", "sid"], ["POST", "/agent/runs/12345/cancel", "rid"], ["POST", "/agent/approvals/zz/decision", "aid"], ["GET", "/canvas/!!/agent/sessions", "id"],
    ] as const) {
      const r = await h.call(method, path, method === "POST" ? { decision: "approve" } : undefined);
      assert.deepEqual([r.status, r.body.code, r.body.msg], [400, 10001, `${name} 格式错误`], path);
    }
  }));

test("请求体校验：规则和 Go 的绑定标签一致，错误是中文提示，多条用「；」连起来", () =>
  withHttp({ turns: [] }, async (h, e) => {
    const sid = enc(e.sessionId);
    const bad = async (method: string, path: string, body: unknown, want: RegExp) => {
      const r = await h.call(method, path, body);
      assert.deepEqual([r.status, r.body.code], [400, 10001], path + String(JSON.stringify(body)).slice(0, 60));
      assert.match(r.body.msg, want, path);
    };
    await bad("POST", `/agent/sessions/${sid}/runs`, {}, /消息为必填字段/);
    await bad("POST", `/agent/sessions/${sid}/runs`, { message: "字".repeat(20001) }, /消息长度不能超过20000个字符/);
    await bad("POST", `/agent/sessions/${sid}/runs`, { message: "x", budget_credits: -1 }, /本轮预算必须大于或等于0/);
    await bad("POST", `/agent/sessions/${sid}/runs`, { message: "x", budget_credits: 100001 }, /本轮预算必须小于或等于100000/);
    await bad("POST", `/agent/sessions/${sid}/runs`, { message: "x", selection: Array(201).fill("n") }, /选中节点/);
    await bad("POST", `/agent/sessions/${sid}/runs`, { message: "x", selection: ["n".repeat(65)] }, /选中节点/);
    await bad("POST", `/agent/sessions/${sid}/runs`, { mode: "fly" }, /消息为必填字段；任务模式必须是/);
    await bad("POST", `/agent/sessions/${sid}/runs`, { message: 5 }, /message 类型错误/);
    await bad("POST", `/agent/sessions/${sid}/runs`, "不是 JSON", /请求参数格式错误/);
    await bad("POST", `/agent/runs/${enc("1")}/interject`, { message: "" }, /消息为必填字段/);
    await bad("POST", `/agent/runs/${enc("1")}/resume`, { add_budget: -5 }, /追加预算必须大于或等于0/);
    await bad("POST", `/agent/approvals/${enc("1")}/decision`, { decision: "maybe" }, /决定必须是\[approve reject\]中的一个/);
    await bad("POST", `/agent/approvals/${enc("1")}/decision`, { decision: "approve", items: [{ index: -1, approve: true, count: 9 }] }, /序号必须大于或等于0；数量必须小于或等于4/);
    await bad("POST", `/agent/approvals/${enc("1")}/decision`, { decision: "approve", answer: "字".repeat(2001) }, /回答长度不能超过2000个字符/);
    await bad("PATCH", `/agent/sessions/${sid}`, { title: "" }, /标题为必填字段/);
    await bad("PATCH", `/agent/sessions/${sid}`, { title: "字".repeat(41) }, /标题长度不能超过40个字符/);
    await bad("POST", `/canvas/${enc("7")}/agent/sessions`, { title: "字".repeat(41), mode: "x" }, /标题长度不能超过40个字符；任务模式必须是/);
    await bad("GET", `/agent/sessions/${sid}/events?after=-1`, undefined, /after必须大于或等于0/);
    await bad("GET", `/agent/sessions/${sid}/events?limit=201`, undefined, /limit必须小于或等于200/);
    await bad("GET", `/agent/sessions/${sid}/events?after=abc`, undefined, /参数格式错误：abc/);
    await bad("GET", `/agent/sessions/${sid}/history?before=-1`, undefined, /before必须大于或等于0/);
  }));

test("可以不带请求体：继续（默认不追加）、撤销、取消；字符数按字（不是字节）算", () =>
  withHttp({ turns: [] }, async (h, e) => {
    const sid = enc(e.sessionId);
    const r = await h.call("PATCH", `/agent/sessions/${sid}`, { title: "字".repeat(40) });
    assert.equal(r.status, 200, "40 个中文字符正好够");
    const resume = await h.call("POST", `/agent/runs/${enc("1")}/resume`);
    assert.equal(resume.body.code, 60012, "走到了业务（运行不存在），说明空请求体被当成 {}");
  }));

test("请求体太大：413；不是 JSON 对象：400", () =>
  withHttp({ turns: [] }, async (h, e) => {
    const big = await h.call("POST", `/agent/sessions/${enc(e.sessionId)}/runs`, { message: "x".repeat(3000) });
    assert.deepEqual([big.status, big.body.code], [413, 10001]);
    const arr = await h.call("POST", `/agent/sessions/${enc(e.sessionId)}/runs`, "[1]");
    assert.equal(arr.status, 400);
  }, { maxBody: 2048 }));

test("评价的接口：PUT 记下并返回服务端记的，DELETE 取消；校验措辞和 Go 一致", () =>
  withHttp({ turns: [{ text: "好" }] }, async (h, e) => {
    const run = await e.start();
    await e.engine.idle();
    const rid = enc(run.id);
    const set = await h.call("PUT", `/agent/runs/${rid}/feedback`, { rating: -1, reasons: ["misread", "misread"], comment: "不对" });
    assert.deepEqual([set.status, set.body.code, set.body.data], [200, 0, { rating: -1, reasons: ["misread"], comment: "不对" }]);
    const clear = await h.call("DELETE", `/agent/runs/${rid}/feedback`);
    assert.deepEqual(clear.body, { code: 0, msg: "success" });
    const bad = async (body: unknown, want: RegExp) => {
      const r = await h.call("PUT", `/agent/runs/${rid}/feedback`, body);
      assert.deepEqual([r.status, r.body.code], [400, 10001], JSON.stringify(body).slice(0, 50));
      assert.match(r.body.msg, want);
    };
    await bad({}, /评价为必填字段/);
    await bad({ rating: 3 }, /评价必须是\[1 -1\]中的一个/);
    await bad({ rating: -1, reasons: Array(6).fill("slow") }, /原因最多包含5项/);
    await bad({ rating: -1, reasons: ["x".repeat(33)] }, /原因\[0\]长度不能超过32个字符/);
    await bad({ rating: 1, comment: "字".repeat(2001) }, /说明长度不能超过2000个字符/);
  }));

test("发起运行和插话的请求体里的 attachments 不会被校验白名单丢掉", () =>
  withHttp({ turns: [{ text: "好" }] }, async (h, e) => {
    const started = await h.call("POST", "/agent/sessions/" + enc(e.sessionId) + "/runs", { message: "看图", attachments: [{ asset_id: "11" }] });
    assert.equal(started.status, 202);
    await e.engine.idle();
    assert.deepEqual(e.persisted("message.user")[0].attachments?.map((a: { asset_id: string }) => a.asset_id), ["11"]);
    // 附件格式不对：10001，不是 500
    const bad = await h.call("POST", "/agent/sessions/" + enc(e.sessionId) + "/runs", { message: "看图", attachments: "abc" });
    assert.deepEqual([bad.status, bad.body.code], [400, 10001]);
  }));

// ---- 后台等待生成 ----

const WATCH_GO = {
  turns: [{ tools: [{ id: "call_g", name: "generate_media", args: { items: [{ nodeId: "n1" }, { nodeId: "n2" }] } }] }, { text: "已提交" }],
  propose: { generate: { payload: { items: [{ node_id: "n1", label: "镜头 1", price: 5 }, { node_id: "n2", label: "镜头 2", price: 5 }], quote_credits: 10, wait: true }, quote_credits: 10, summary: "s", content: "已提交" } },
  execute: () => ({ status: "executed", result: { tasks: [{ node_id: "n1", task_id: "101" }, { node_id: "n2", task_id: "102" }] }, outcome_text: "任务已提交" }),
};

/** 模型申请生成并被批准：这批任务进入后台等待 */
async function waiting(h: Parameters<Parameters<typeof withHttp>[1]>[0], e: Parameters<Parameters<typeof withHttp>[1]>[1]) {
  const run = await e.start();
  await e.engine.idle();
  const [approval] = e.store.listApprovals(run.id);
  await h.call("POST", "/agent/approvals/" + enc(approval.id) + "/decision", { decision: "approve" });
  await e.engine.idle();
  return run;
}

test("等待条：GET 给出进行中的批（任务、标题、进度），不用等了 POST 取消它们；别人的会话是 404", () =>
  withHttp(WATCH_GO, async (h, e) => {
    await waiting(h, e);
    const sid = enc(e.sessionId);
    const got = await h.call("GET", `/agent/sessions/${sid}/watches`);
    assert.deepEqual([got.status, got.body.code], [200, 0]);
    const [b] = got.body.data.items;
    assert.deepEqual([b.state, b.total, b.settled], ["waiting", 2, 0]);
    assert.deepEqual(b.tasks.map((t: any) => [t.task_id, t.node_id, t.label, t.status]), [["101", "n1", "镜头 1", "pending"], ["102", "n2", "镜头 2", "pending"]]);

    const other = await h.call("GET", `/agent/sessions/${sid}/watches`, undefined, { "x-agent-user": "2" });
    assert.deepEqual([other.status, other.body.code], [404, 60011]);
    const otherCancel = await h.call("POST", `/agent/sessions/${sid}/watches/cancel`, undefined, { "x-agent-user": "2" });
    assert.deepEqual([otherCancel.status, otherCancel.body.code], [404, 60011]);
    assert.equal((await h.call("GET", `/agent/sessions/${sid}/watches`)).body.data.items.length, 1, "别人取消不了");

    const canceled = await h.call("POST", `/agent/sessions/${sid}/watches/cancel`);
    assert.deepEqual([canceled.status, canceled.body.data.items.map((x: any) => x.state)], [200, ["canceled"]]);
    assert.deepEqual((await h.call("GET", `/agent/sessions/${sid}/watches`)).body.data.items, []);
    assert.deepEqual((await h.call("POST", `/agent/sessions/${sid}/watches/cancel`)).body.data.items, [], "再点一次什么都没有");
  }, { engine: { piClock: true } }));

test("没有任何等待的会话：GET 是空数组，不是 404", () =>
  withHttp({ turns: [] }, async (h, e) => {
    const r = await h.call("GET", `/agent/sessions/${enc(e.sessionId)}/watches`);
    assert.deepEqual([r.status, r.body.data], [200, { items: [] }]);
  }));

test("门铃（Go → Agent）：只认服务令牌，不要用户头；认识的任务 200，不认识的 404（Go 据此不再重试）；参数不对 400", () =>
  withHttp(WATCH_GO, async (h, e) => {
    const run = await waiting(h, e);
    const bell = (body: unknown, headers: Record<string, string | undefined> = { "x-agent-user": undefined }) => h.call("POST", "/internal/v1/watches/task-settled", body, headers);

    const noToken = await bell({ task_id: "101", run_id: run.id, status: "succeeded" }, { "x-agent-user": undefined, "x-agent-service-token": undefined });
    assert.equal(noToken.status, 401);

    const ok = await bell({ task_id: "101", run_id: run.id, status: "succeeded" });
    assert.deepEqual([ok.status, ok.body.code, ok.body.data], [200, 0, { accepted: true }]);
    const state = await h.call("GET", `/agent/sessions/${enc(e.sessionId)}/watches`);
    assert.equal(state.body.data.items[0].settled, 1);

    const unknownTask = await bell({ task_id: "555", run_id: run.id, status: "succeeded" });
    assert.deepEqual([unknownTask.status, unknownTask.body.code], [404, 60014]);
    const unknownRun = await bell({ task_id: "101", run_id: "999999", status: "succeeded" });
    assert.deepEqual([unknownRun.status, unknownRun.body.code], [404, 60014]);

    for (const [name, body] of Object.entries({
      "任务 id 不是数字": { task_id: "abc", run_id: run.id, status: "succeeded" },
      "运行 id 是 JSON 数字": { task_id: "101", run_id: 7, status: "succeeded" },
      "状态不是终态": { task_id: "101", run_id: run.id, status: "running" },
      "缺状态": { task_id: "101", run_id: run.id },
    })) {
      const r = await bell(body);
      assert.deepEqual([r.status, r.body.code], [400, 10001], name);
    }
  }, { engine: { piClock: true } }));

test("删除会话：它的后台等待一并收掉（不会留下没人认领的监听任务）", () =>
  withHttp(WATCH_GO, async (h, e) => {
    await waiting(h, e);
    assert.equal(await e.engine.liveTaskCount(), 1);
    const del = await h.call("DELETE", "/agent/sessions/" + enc(e.sessionId));
    assert.equal(del.status, 200);
    for (let i = 0; i < 100 && (await e.engine.liveTaskCount()) > 0; i++) await new Promise((r) => setTimeout(r, 20));
    assert.equal(await e.engine.liveTaskCount(), 0);
  }, { engine: { piClock: true } }));

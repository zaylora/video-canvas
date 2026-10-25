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
    await bad("POST", `/agent/sessions/${sid}/runs`, { message: "x", mode: "fly" }, /任务模式必须是\[all script storyboard prompt\]中的一个/);
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

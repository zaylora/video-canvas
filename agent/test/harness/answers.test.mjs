import test from "node:test";
import assert from "node:assert/strict";
import { findAnswer, buildDecision, suggestAnswer, validateAnswers, matchApproval } from "../../harness/answers.mjs";
import { approvalFromEvent } from "../../harness/events.mjs";
import { ev, approval } from "./helpers.mjs";

const ap = (kind, payload, id = "a1") => approvalFromEvent(ev(1, "approval.created", approval(id, kind, payload)));

const askRatio = ap("ask", { question: "画幅用哪个？16:9 / 9:16 / 1:1", kind: "choice", options: ["16:9", "9:16", "1:1"] });
const askModel = ap("ask", { question: "图片用哪个模型？", kind: "model", model_kind: "image", models: [{ key: "img-a", name: "A" }, { key: "img-b", name: "B" }] });
const gen = ap("generate", { reason: "出 3 张关键帧", items: [{ node_id: "n1" }] });
const del = ap("delete", { reason: "清理旧节点", node_ids: ["n9"], labels: ["旧镜头"] });

test("审批事件被规整成 kind / ask_kind / question", () => {
  assert.equal(askRatio.kind, "ask");
  assert.equal(askRatio.ask_kind, "choice");
  assert.equal(askRatio.question, "画幅用哪个？16:9 / 9:16 / 1:1");
  assert.equal(askModel.ask_kind, "model");
  assert.equal(askModel.model_kind, "image");
  assert.equal(gen.kind, "generate");
  assert.equal(gen.ask_kind, "");
  assert.equal(askRatio.id, "a1");
});

test("matchApproval：kind / ask_kind / model_kind / contains 都要命中；空 match 命中全部", () => {
  assert.equal(matchApproval({ kind: "ask", contains: "画幅" }, askRatio), true);
  assert.equal(matchApproval({ kind: "ask", contains: "画幅" }, askModel), false);
  assert.equal(matchApproval({ kind: "ask", ask_kind: "model" }, askModel), true);
  assert.equal(matchApproval({ kind: "ask", ask_kind: "choice" }, askModel), false);
  assert.equal(matchApproval({ kind: "ask", ask_kind: "model", model_kind: "video" }, askModel), false);
  assert.equal(matchApproval({ kind: "generate" }, gen), true);
  assert.equal(matchApproval({ kind: "generate" }, del), false);
  assert.equal(matchApproval({}, del), true);
  assert.equal(matchApproval(undefined, del), true);
});

test("contains 对 generate / delete 匹配理由和节点标题", () => {
  assert.equal(matchApproval({ kind: "generate", contains: "关键帧" }, gen), true);
  assert.equal(matchApproval({ kind: "delete", contains: "旧镜头" }, del), true);
});

const script = [
  { match: { kind: "ask", contains: "画幅" }, answer: { answer: "9:16" } },
  { match: { kind: "ask", ask_kind: "model" }, answer: { answer: "$first_model" } },
  { match: { kind: "generate" }, answer: { decision: "reject" } },
  { match: { kind: "delete" }, answer: { decision: "reject", answer: "不要删" } },
];

test("findAnswer 按顺序匹配，第一条命中的生效", () => {
  assert.equal(findAnswer(script, askRatio).index, 0);
  assert.equal(findAnswer(script, askModel).index, 1);
  assert.equal(findAnswer(script, gen).index, 2);
  assert.equal(findAnswer(script, del).index, 3);
  // 前面有更宽的规则时，后面的不会被用到
  const wide = [{ match: { kind: "ask" }, answer: { answer: "第一条" } }, ...script];
  assert.equal(findAnswer(wide, askRatio).index, 0);
});

test("没命中返回 null", () => {
  assert.equal(findAnswer([{ match: { kind: "generate" }, answer: { decision: "reject" } }], askRatio), null);
  assert.equal(findAnswer([], askRatio), null);
  assert.equal(findAnswer(undefined, askRatio), null);
});

test("buildDecision：回答提问时默认 approve，字段与 DecideAgentApprovalReq 一致", () => {
  assert.deepEqual(buildDecision({ answer: { answer: "9:16" } }, askRatio), { decision: "approve", answer: "9:16" });
  assert.deepEqual(buildDecision({ answer: { decision: "reject" } }, gen), { decision: "reject" });
  assert.deepEqual(buildDecision({ answer: { decision: "reject", answer: "不要删" } }, del), { decision: "reject", answer: "不要删" });
  assert.deepEqual(buildDecision({ answer: { decision: "approve", items: [{ index: 0, approve: true, count: 2 }], add_budget: 10 } }, gen), {
    decision: "approve",
    items: [{ index: 0, approve: true, count: 2 }],
    add_budget: 10,
  });
});

test("buildDecision：$first_model 取提问里的第一个模型 key，没有就报错", () => {
  assert.deepEqual(buildDecision({ answer: { answer: "$first_model" } }, askModel), { decision: "approve", answer: "img-a" });
  assert.throws(() => buildDecision({ answer: { answer: "$first_model" } }, askRatio), /first_model/);
});

test("buildDecision 不会把未知字段发给后端", () => {
  const body = buildDecision({ answer: { answer: "x", evil: 1 } }, askRatio);
  assert.deepEqual(Object.keys(body).sort(), ["answer", "decision"]);
});

test("suggestAnswer：按审批种类给出能直接粘进用例的 answers 条目", () => {
  const s1 = suggestAnswer(askRatio);
  assert.deepEqual(s1.match, { kind: "ask", contains: "画幅用哪个" });
  assert.equal(s1.answer.answer, "16:9"); // 选项题给第一个选项当占位
  const s2 = suggestAnswer(askModel);
  assert.deepEqual(s2.match, { kind: "ask", ask_kind: "model", model_kind: "image" });
  assert.equal(s2.answer.answer, "$first_model");
  assert.deepEqual(suggestAnswer(gen), { match: { kind: "generate" }, answer: { decision: "reject" } });
  assert.deepEqual(suggestAnswer(del), { match: { kind: "delete" }, answer: { decision: "reject" } });
  // 建议出来的条目自己必须能命中这条提问
  for (const a of [askRatio, askModel, gen, del]) assert.equal(matchApproval(suggestAnswer(a).match, a), true);
});

test("validateAnswers：kind 只能是 generate/delete/ask；ask 的批准要有 answer", () => {
  assert.deepEqual(validateAnswers(script), []);
  assert.ok(validateAnswers([{ match: { kind: "pay" }, answer: { decision: "reject" } }]).length > 0);
  assert.ok(validateAnswers([{ match: { kind: "ask" }, answer: {} }]).length > 0, "ask 默认批准，必须给 answer");
  assert.deepEqual(validateAnswers([{ match: { kind: "ask" }, answer: { decision: "reject" } }]), []);
  assert.ok(validateAnswers([{ answer: { decision: "reject" } }]).length === 0, "match 可省略，表示全部命中");
  assert.ok(validateAnswers([{ match: { kind: "generate" } }]).length > 0, "缺 answer");
  assert.ok(validateAnswers([{ match: { kind: "generate" }, answer: { decision: "maybe" } }]).length > 0);
  assert.ok(validateAnswers("nope").length > 0);
});

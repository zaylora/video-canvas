// 应答剧本：Agent 在运行中会提问（ask_user）或申请批准（生成、删除），harness 没有人，只能按用例里预写的 answers 应答。
//
// answers 按顺序匹配，第一条命中的生效。match 字段：
//   kind        审批种类 generate / delete / ask（model.ApprovalGenerate 等）
//   ask_kind    ask 下的子类：ask_user 工具的 kind 参数，choice（选项题）或 model（选模型题）
//   model_kind  选模型题要选哪类模型：image / video
//   contains    提问原文包含这段文字（generate / delete 则匹配理由和节点标题）
// answer 的字段与 DecideAgentApprovalReq 一致：decision（approve / reject，不写默认 approve）、items、answer、add_budget。

const KINDS = ["generate", "delete", "ask"];

/** 这条审批是否命中 match。match 为空表示命中全部。 */
export function matchApproval(match, approval) {
  if (!match) return true;
  if (match.kind !== undefined && match.kind !== approval.kind) return false;
  if (match.ask_kind !== undefined && match.ask_kind !== approval.ask_kind) return false;
  if (match.model_kind !== undefined && match.model_kind !== approval.model_kind) return false;
  if (match.contains !== undefined && !String(approval.text ?? "").includes(match.contains)) return false;
  return true;
}

/** 按顺序找第一条命中的应答，返回 {index, entry}；没命中返回 null。 */
export function findAnswer(answers, approval) {
  if (!Array.isArray(answers)) return null;
  for (let i = 0; i < answers.length; i++) {
    if (matchApproval(answers[i].match, approval)) return { index: i, entry: answers[i] };
  }
  return null;
}

/** 占位符：$first_model 取选模型题里的第一个模型 key，$first_option 取选项题的第一个选项。 */
function resolveAnswerText(text, approval) {
  if (text === "$first_model") {
    const key = approval.models?.[0]?.key;
    if (!key) throw new Error("应答写了 $first_model，但这条提问里没有可选的模型（只有选模型的提问才能用 $first_model）");
    return key;
  }
  if (text === "$first_option") {
    if (!approval.options?.length) throw new Error("应答写了 $first_option，但这条提问没有选项");
    return approval.options[0];
  }
  return text;
}

/** 生成发给 POST /agent/approvals/:aid/decision 的请求体，只带后端认识的字段。 */
export function buildDecision(entry, approval) {
  const a = entry.answer ?? {};
  const body = { decision: a.decision ?? "approve" };
  if (a.items !== undefined) body.items = a.items;
  if (typeof a.answer === "string") body.answer = resolveAnswerText(a.answer, approval);
  if (a.add_budget !== undefined) body.add_budget = a.add_budget;
  return body;
}

/** 提问原文的前几个字，作为 contains 的建议值。 */
export function questionStem(question) {
  const first = question.split(/[？?，,。.！!、:：;；/\n]/)[0].trim();
  return first.slice(0, 12) || question.slice(0, 12);
}

/** 未预写的提问：给出能直接粘进用例 answers 的条目（回答只是占位，需要人确认）。 */
export function suggestAnswer(approval) {
  if (approval.kind === "ask") {
    if (approval.ask_kind === "model") {
      const match = { kind: "ask", ask_kind: "model" };
      if (approval.model_kind) match.model_kind = approval.model_kind;
      return { match, answer: { answer: "$first_model" } };
    }
    return { match: { kind: "ask", contains: questionStem(approval.question) }, answer: { answer: approval.options?.[0] ?? "<填写回答>" } };
  }
  return { match: { kind: approval.kind }, answer: { decision: "reject" } };
}

/** 校验用例里的 answers，返回问题列表。 */
export function validateAnswers(answers) {
  if (answers === undefined) return [];
  if (!Array.isArray(answers)) return ["answers 必须是数组"];
  const errs = [];
  answers.forEach((e, i) => {
    const at = `answers[${i}]`;
    if (!e || typeof e !== "object" || Array.isArray(e)) return errs.push(`${at} 必须是对象`);
    const m = e.match;
    if (m !== undefined) {
      if (!m || typeof m !== "object" || Array.isArray(m)) errs.push(`${at}.match 必须是对象`);
      else {
        if (m.kind !== undefined && !KINDS.includes(m.kind)) errs.push(`${at}.match.kind 只能是 ${KINDS.join(" / ")}`);
        for (const k of ["ask_kind", "model_kind", "contains"]) if (m[k] !== undefined && typeof m[k] !== "string") errs.push(`${at}.match.${k} 必须是字符串`);
      }
    }
    const a = e.answer;
    if (!a || typeof a !== "object" || Array.isArray(a)) return errs.push(`${at}.answer 必须是对象（至少写 decision 或 answer）`);
    if (a.decision !== undefined && !["approve", "reject"].includes(a.decision)) errs.push(`${at}.answer.decision 只能是 approve / reject`);
    if (a.answer !== undefined && typeof a.answer !== "string") errs.push(`${at}.answer.answer 必须是字符串`);
    if (a.items !== undefined && !Array.isArray(a.items)) errs.push(`${at}.answer.items 必须是数组`);
    if (a.add_budget !== undefined && !(Number.isFinite(a.add_budget) && a.add_budget >= 0)) errs.push(`${at}.answer.add_budget 必须是非负数`);
    const approves = (a.decision ?? "approve") === "approve";
    if (m?.kind === "ask" && approves && !(typeof a.answer === "string" && a.answer !== "")) errs.push(`${at} 回答提问必须写 answer.answer（拒绝回答则写 decision: "reject"）`);
  });
  return errs;
}

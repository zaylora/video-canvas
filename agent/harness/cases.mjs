// 用例的读取与校验。用例格式见 README.md 和设计文档 6.B.4。

import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { validateAssertion } from "./assertions.mjs";
import { validateAnswers } from "./answers.mjs";

const HARNESS_ROOT = path.dirname(fileURLToPath(import.meta.url));
const MODES = ["all", "script", "storyboard", "prompt"]; // model.CreateAgentSessionReq 的 oneof
const ID_PATTERN = /^[a-z0-9][a-z0-9_-]*$/; // 要进目录名和 X-Agent-Harness 请求头
const CASE_SUFFIX = ".case.json";
export const DEFAULT_TIMEOUT_SEC = 900;

/** 用例读取 / 校验失败。message 里一次说完所有问题。 */
export class CaseError extends Error {}

/** 返回问题列表，空数组表示用例合法。 */
export function validateCase(def) {
  if (!def || typeof def !== "object" || Array.isArray(def)) return ["用例必须是 JSON 对象"];
  const errs = [];
  if (typeof def.id !== "string" || def.id === "") errs.push("缺少 id");
  else if (!ID_PATTERN.test(def.id) || def.id.length > 128) errs.push(`id ${JSON.stringify(def.id)} 不合法：只能用小写字母、数字、横线和下划线，不超过 128 个字符`);
  if (def.title !== undefined && typeof def.title !== "string") errs.push("title 必须是字符串");
  if (def.tags !== undefined && !(Array.isArray(def.tags) && def.tags.every((t) => typeof t === "string"))) errs.push("tags 必须是字符串数组");
  if (def.fixture !== undefined && typeof def.fixture !== "string") errs.push("fixture 必须是字符串（初始画布 JSON 的路径）");
  if (def.mode !== undefined && !MODES.includes(def.mode)) errs.push(`mode 只能是 ${MODES.join(" / ")}`);
  if (def.agent_model_key !== undefined && typeof def.agent_model_key !== "string") errs.push("agent_model_key 必须是字符串");
  if (typeof def.message !== "string" || def.message.trim() === "") errs.push("缺少 message（发给 Agent 的用户原话）");
  else if ([...def.message].length > 20000) errs.push("message 超过 20000 个字符");
  if (def.selection !== undefined && !(Array.isArray(def.selection) && def.selection.every((s) => typeof s === "string"))) errs.push("selection 必须是节点 id 的字符串数组");
  if (def.budget === undefined) errs.push("缺少 budget（本轮积分预算；不花钱的用例写 0，后端会按它强制执行）");
  else if (typeof def.budget !== "number" || !Number.isFinite(def.budget) || def.budget < 0) errs.push("budget 必须是 ≥ 0 的数字");
  if (def.timeout_sec !== undefined && !(typeof def.timeout_sec === "number" && def.timeout_sec > 0)) errs.push("timeout_sec 必须是正数");
  if (def.wait_tasks !== undefined && typeof def.wait_tasks !== "boolean") errs.push("wait_tasks 必须是 true / false");
  errs.push(...validateAnswers(def.answers));
  if (!Array.isArray(def.expect) || def.expect.length === 0) errs.push("expect 必须是非空数组（没有断言的用例永远通过）");
  else def.expect.forEach((a, i) => validateAssertion(a).forEach((e) => errs.push(`expect[${i}]：${e}`)));
  return errs;
}

/** 补默认值（不改传入对象）。 */
export function withDefaults(def) {
  return { mode: "all", selection: [], timeout_sec: DEFAULT_TIMEOUT_SEC, wait_tasks: false, answers: [], agent_model_key: "", ...def };
}

function listCaseFiles(dir) {
  const out = [];
  for (const entry of fs.readdirSync(dir, { withFileTypes: true }).sort((a, b) => a.name.localeCompare(b.name))) {
    const full = path.join(dir, entry.name);
    if (entry.isDirectory()) out.push(...listCaseFiles(full));
    else if (entry.name.endsWith(CASE_SUFFIX)) out.push(full);
  }
  return out;
}

/**
 * 读用例：target 是单个文件，或目录（递归找 *.case.json，按路径排序）。
 * 返回 [{file, def}]，def 已补默认值。任何文件有问题都抛 CaseError，message 里列出全部问题。
 */
export function loadCases(target) {
  if (!fs.existsSync(target)) throw new CaseError(`路径不存在：${target}`);
  const files = fs.statSync(target).isDirectory() ? listCaseFiles(target) : [target];
  if (files.length === 0) throw new CaseError(`${target} 下没有找到 *${CASE_SUFFIX} 用例文件`);
  const problems = [];
  const list = [];
  const seen = new Map();
  for (const file of files) {
    let def;
    try {
      def = JSON.parse(fs.readFileSync(file, "utf8"));
    } catch (e) {
      problems.push(`${file}：不是合法的 JSON（${e.message}）`);
      continue;
    }
    const errs = validateCase(def);
    for (const e of errs) problems.push(`${file}：${e}`);
    if (errs.length) continue;
    if (seen.has(def.id)) problems.push(`${file}：用例 id ${def.id} 重复（另一个在 ${seen.get(def.id)}）`);
    seen.set(def.id, file);
    list.push({ file, def: withDefaults(def) });
  }
  if (problems.length) throw new CaseError(`用例有 ${problems.length} 个问题：\n` + problems.map((p) => `  - ${p}`).join("\n"));
  return list;
}

/** 读初始画布。相对路径依次在用例所在目录、其上一级、harness 根目录里找；不写 fixture 就是空画布。 */
export function loadFixture(def, caseFile) {
  if (!def.fixture) return { nodes: [], edges: [] };
  const dir = path.dirname(path.resolve(caseFile));
  const candidates = path.isAbsolute(def.fixture) ? [def.fixture] : [path.join(dir, def.fixture), path.join(path.dirname(dir), def.fixture), path.join(HARNESS_ROOT, def.fixture)];
  const found = candidates.find((p) => fs.existsSync(p));
  if (!found) throw new CaseError(`用例 ${def.id} 的 fixture 找不到：${def.fixture}（找过：${candidates.join("、")}）`);
  try {
    return JSON.parse(fs.readFileSync(found, "utf8"));
  } catch (e) {
    throw new CaseError(`fixture ${found} 不是合法的 JSON：${e.message}`);
  }
}

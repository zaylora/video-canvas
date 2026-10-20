import test from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { validateCase, loadCases, loadFixture, CaseError } from "../../harness/cases.mjs";
import { planBudget } from "../../harness/budget.mjs";

const here = path.dirname(fileURLToPath(import.meta.url));
const casesDir = path.resolve(here, "../../harness/cases");

const good = () => ({
  id: "demo-case",
  title: "示例",
  message: "把剧本拆成 3 个镜头",
  budget: 0,
  expect: [{ type: "run_status", in: ["succeeded"] }],
});

test("最小合法用例没有错误", () => {
  assert.deepEqual(validateCase(good()), []);
});

test("必填项：id、message、budget、expect", () => {
  for (const k of ["id", "message", "budget", "expect"]) {
    const c = good();
    delete c[k];
    const errs = validateCase(c);
    assert.ok(errs.some((e) => e.includes(k)), `缺 ${k} 应报错：${errs}`);
  }
});

test("source：线上导入用例的来源字段允许存在，必须是对象，字段类型要对", () => {
  assert.deepEqual(validateCase({ ...good(), source: { run_id: "abc", exported_at: "2026-10-20T09:00:00Z", prompt_version: 9, status: "succeeded" } }), []);
  assert.deepEqual(validateCase({ ...good(), source: { run_id: "abc", exported_at: null } }), []);
  assert.ok(validateCase({ ...good(), source: "线上" }).some((e) => e.includes("source")));
  assert.ok(validateCase({ ...good(), source: [] }).some((e) => e.includes("source")));
  assert.ok(validateCase({ ...good(), source: { run_id: 5 } }).some((e) => e.includes("source.run_id")));
  assert.ok(validateCase({ ...good(), source: { exported_at: 5 } }).some((e) => e.includes("source.exported_at")));
  assert.ok(validateCase({ ...good(), source: { prompt_version: "9" } }).some((e) => e.includes("source.prompt_version")));
});

test("缺 budget 的报错要说清怎么改", () => {
  const c = good();
  delete c.budget;
  assert.match(validateCase(c).join("\n"), /budget.*0/);
});

test("id 只能是小写字母、数字、横线和下划线，且不超过 128 个字符（要进目录名和请求头）", () => {
  assert.ok(validateCase({ ...good(), id: "有空格 的" }).length > 0);
  assert.ok(validateCase({ ...good(), id: "a".repeat(129) }).length > 0);
  assert.deepEqual(validateCase({ ...good(), id: "ok_case-1" }), []);
});

test("字段类型：mode、timeout_sec、selection、wait_tasks", () => {
  assert.ok(validateCase({ ...good(), mode: "chaos" }).length > 0);
  assert.ok(validateCase({ ...good(), timeout_sec: 0 }).length > 0);
  assert.ok(validateCase({ ...good(), selection: "n1" }).length > 0);
  assert.ok(validateCase({ ...good(), wait_tasks: "yes" }).length > 0);
  assert.deepEqual(validateCase({ ...good(), mode: "storyboard", timeout_sec: 600, selection: ["n1"], wait_tasks: true }), []);
});

test("expect 里的每条断言和 answers 里的每条应答都会被校验", () => {
  assert.ok(validateCase({ ...good(), expect: [{ type: "count" }] }).length > 0);
  assert.ok(validateCase({ ...good(), expect: [] }).length > 0, "至少一条断言，否则永远通过");
  assert.ok(validateCase({ ...good(), answers: [{ match: { kind: "bogus" }, answer: { decision: "reject" } }] }).length > 0);
});

test("loadCases 读目录：只读 *.case.json，按文件名排序，补默认值", () => {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "hn-cases-"));
  fs.writeFileSync(path.join(dir, "b.case.json"), JSON.stringify({ ...good(), id: "b" }));
  fs.writeFileSync(path.join(dir, "a.case.json"), JSON.stringify({ ...good(), id: "a" }));
  fs.writeFileSync(path.join(dir, "notes.json"), "{ 不是用例 }");
  const list = loadCases(dir);
  assert.deepEqual(list.map((c) => c.def.id), ["a", "b"]);
  const d = list[0].def;
  assert.equal(d.mode, "all");
  assert.equal(d.timeout_sec, 900);
  assert.equal(d.wait_tasks, false);
  assert.deepEqual(d.selection, []);
  assert.deepEqual(d.answers, []);
});

test("loadCases 读单个文件", () => {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "hn-cases-"));
  const f = path.join(dir, "x.case.json");
  fs.writeFileSync(f, JSON.stringify(good()));
  assert.equal(loadCases(f).length, 1);
});

test("loadCases 把所有用例的所有问题一次说完，并带上文件名", () => {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "hn-cases-"));
  fs.writeFileSync(path.join(dir, "a.case.json"), JSON.stringify({ id: "a", message: "m", expect: [{ type: "no_overlap" }] })); // 缺 budget
  fs.writeFileSync(path.join(dir, "b.case.json"), "{ 坏的 json");
  assert.throws(
    () => loadCases(dir),
    (e) => e instanceof CaseError && /a\.case\.json/.test(e.message) && /budget/.test(e.message) && /b\.case\.json/.test(e.message),
  );
});

test("用例 id 重复会报错", () => {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "hn-cases-"));
  fs.writeFileSync(path.join(dir, "a.case.json"), JSON.stringify(good()));
  fs.writeFileSync(path.join(dir, "b.case.json"), JSON.stringify(good()));
  assert.throws(() => loadCases(dir), /重复/);
});

test("目录里没有用例、路径不存在都报错", () => {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "hn-cases-"));
  assert.throws(() => loadCases(dir), /没有找到/);
  assert.throws(() => loadCases(path.join(dir, "nope")), /不存在/);
});

test("loadFixture：fixture 相对用例文件所在目录、其上一级、harness 根目录依次找", () => {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "hn-fx-"));
  fs.mkdirSync(path.join(dir, "cases"));
  fs.mkdirSync(path.join(dir, "fixtures"));
  fs.writeFileSync(path.join(dir, "fixtures", "f.canvas.json"), JSON.stringify({ nodes: [{ id: "n1" }], edges: [] }));
  const file = path.join(dir, "cases", "x.case.json");
  assert.deepEqual(loadFixture({ fixture: "fixtures/f.canvas.json" }, file), { nodes: [{ id: "n1" }], edges: [] });
  assert.deepEqual(loadFixture({}, file), { nodes: [], edges: [] }, "不写 fixture 就是空画布");
  assert.throws(() => loadFixture({ fixture: "fixtures/missing.json" }, file), /找不到/);
});

test("随仓库提供的示例用例全部合法，预算都已写明", () => {
  const list = loadCases(casesDir);
  const ids = list.map((c) => c.def.id);
  for (const want of ["storyboard-9x16-3shots", "script-draft", "prompt-optimize"]) assert.ok(ids.includes(want), `缺示例用例 ${want}`);
  for (const c of list) {
    assert.equal(typeof c.def.budget, "number");
    loadFixture(c.def, c.file); // fixture 都存在且是合法 JSON
  }
  // 整批默认上限 200 内能放行
  assert.equal(planBudget(list.map((c) => c.def), { repeat: 1, maxCredits: 200 }).ok, true);
});

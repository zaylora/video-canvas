import test from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { nodeSize } from "../../harness/size.mjs";

const here = path.dirname(fileURLToPath(import.meta.url));
// 前后端共用的测试向量：JS 版尺寸规则必须对它全部通过
const vectorFile = path.resolve(here, "../../../backend/internal/tests/testdata/canvasgraph/node-size-cases.json");
const { cases } = JSON.parse(fs.readFileSync(vectorFile, "utf8"));

test("共享测试向量不为空", () => {
  assert.ok(cases.length >= 10);
});

for (const c of cases) {
  test(`尺寸向量：${c.name}`, () => {
    const data = { kind: c.kind, label: "n", model: "m" };
    if (c.params) data.params = c.params;
    if (c.mediaAspect !== undefined) data.mediaAspect = c.mediaAspect;
    const n = { id: "n", type: "canvas", position: { x: 0, y: 0 }, data };
    // 与 Go 端测试一致：只有模型 "m" 有默认画幅
    const defaultAspect = (model) => (model === "m" ? (c.defaultAspect ?? "") : "");
    const { w, h } = nodeSize(n, { defaultAspect });
    assert.deepEqual({ w, h }, { w: c.want.w, h: c.want.h });
  });
}

test("组取自己的 width/height，不小于 200×140", () => {
  assert.deepEqual(nodeSize({ id: "g", type: "group", width: 2064, height: 444 }), { w: 2064, h: 444 });
  assert.deepEqual(nodeSize({ id: "g", type: "group", width: 10, height: 10 }), { w: 200, h: 140 });
  assert.deepEqual(nodeSize({ id: "g", type: "group" }), { w: 200, h: 140 });
});

test("不传 defaultAspect 时，没写比例的图片按 16:9", () => {
  assert.deepEqual(nodeSize({ id: "n", type: "canvas", data: { kind: "image" } }), { w: 576, h: 324 });
});

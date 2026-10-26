import assert from "node:assert/strict";
import { test } from "node:test";
import { wrapScalarArrays } from "../src/tools.ts";

test("单个值传给数组参数：自动包成数组（和迁移前一样），对象不动", () => {
  const schema = { type: "object", properties: { ids: { type: "array", items: { type: "string" } }, deep: { type: "object", properties: { xs: { type: "array", items: { type: "number" } } } } } };
  assert.deepEqual(wrapScalarArrays(schema, { ids: "a" }), { ids: ["a"] });
  assert.deepEqual(wrapScalarArrays(schema, { ids: ["a", "b"] }), { ids: ["a", "b"] });
  assert.deepEqual(wrapScalarArrays(schema, { deep: { xs: 3 } }), { deep: { xs: [3] } });
  assert.deepEqual(wrapScalarArrays(schema, { ids: { a: 1 } }), { ids: { a: 1 } }, "对象放在要求数组的地方不改，让校验报错");
  assert.deepEqual(wrapScalarArrays(schema, {}), {});
  const input = { ids: "a" };
  wrapScalarArrays(schema, input);
  assert.deepEqual(input, { ids: "a" }, "不修改入参");
});

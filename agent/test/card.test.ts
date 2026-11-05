import assert from "node:assert/strict";
import { test } from "node:test";
import { validateCard, type Card } from "../src/card.ts";

const ok = (args: unknown): Card => {
  const r = validateCard(args);
  assert.ok(r.ok, r.ok ? "" : r.error);
  return r.card;
};
const bad = (args: unknown, want: RegExp) => {
  const r = validateCard(args);
  assert.equal(r.ok, false, "应当校验失败");
  if (!r.ok) assert.match(r.error, want);
};

const summary = (over: Record<string, unknown> = {}) => ({
  kind: "summary",
  title: "有可复用的插件",
  rows: [{ label: "插件", value: "New API 视频" }],
  ...over,
});
const table = (over: Record<string, unknown> = {}) => ({
  kind: "table",
  title: "对照",
  columns: ["模型", "协议"],
  rows: [{ cells: ["kling-v3", "newapi"] }],
  ...over,
});

test("summary：归一化（去空白、补默认 tone、保留 sub/badge/ref）", () => {
  const c = ok(
    summary({
      title: " 有可复用的插件 ",
      tone: "ok",
      rows: [
        {
          label: "插件",
          value: "New API 视频",
          sub: "/v1/videos",
          badge: "已启用",
          tone: "ok",
          ref: { type: "plugin", key: "new-api-video" },
        },
        { label: "渠道", value: " x " },
      ],
      notes: [{ tone: "warn", text: "价格页是动态加载的" }, { text: "没有 tone" }],
      sources: [{ title: "价格页", url: "https://newapi.vbing.site/pricing" }],
    }),
  );
  assert.deepEqual(c, {
    kind: "summary",
    title: "有可复用的插件",
    tone: "ok",
    rows: [
      {
        label: "插件",
        value: "New API 视频",
        sub: "/v1/videos",
        badge: "已启用",
        tone: "ok",
        ref: { type: "plugin", key: "new-api-video" },
      },
      { label: "渠道", value: "x" },
    ],
    notes: [
      { tone: "warn", text: "价格页是动态加载的" },
      { tone: "info", text: "没有 tone" },
    ],
    sources: [{ title: "价格页", url: "https://newapi.vbing.site/pricing" }],
  });
  assert.equal(ok(summary()).tone, "info", "缺省 tone 是 info");
});

test("table：单元格可以是字符串，归一化成 {text, tone?, ref?}", () => {
  const c = ok(
    table({
      rows: [
        {
          cells: [
            { text: "kling-v3", tone: "ok", ref: { type: "model", key: "kling-v3" } },
            "newapi",
          ],
        },
      ],
    }),
  );
  assert.deepEqual(c, {
    kind: "table",
    title: "对照",
    tone: "info",
    columns: ["模型", "协议"],
    rows: [
      {
        cells: [
          { text: "kling-v3", tone: "ok", ref: { type: "model", key: "kling-v3" } },
          { text: "newapi" },
        ],
      },
    ],
  });
});

test("pricing_settings 的 ref 不需要 key；其他类型必须有 key", () => {
  const c = ok(
    summary({ rows: [{ label: "定价", value: "去看看", ref: { type: "pricing_settings" } }] }),
  ) as Extract<Card, { kind: "summary" }>;
  assert.deepEqual(c.rows[0].ref, { type: "pricing_settings" });
  bad(
    summary({ rows: [{ label: "模型", value: "x", ref: { type: "model" } }] }),
    /rows\[0\]\.ref.*key/,
  );
});

test("新增后台管理对象可作为卡片引用", () => {
  for (const type of ["user", "register_settings", "smtp_settings", "storage", "image_processor"]) {
    const result = validateCard({
      kind: "summary",
      title: "后台状态",
      rows: [{ label: "对象", value: "已读取", ref: { type, key: "1" } }],
    });
    assert.equal(result.ok, true);
  }
});

test("非法参数：返回中文错误，指出位置", () => {
  bad(null, /参数/);
  bad(summary({ kind: "chart" }), /kind.*summary.*table/);
  bad(summary({ title: " " }), /title/);
  bad(summary({ title: "字".repeat(41) }), /title.*40/);
  bad(summary({ tone: "red" }), /tone/);
  bad(summary({ rows: [] }), /rows.*1.*12/);
  bad(
    summary({ rows: Array.from({ length: 13 }, () => ({ label: "a", value: "b" })) }),
    /rows.*1.*12/,
  );
  bad(summary({ rows: [{ label: "标".repeat(13), value: "b" }] }), /rows\[0\]\.label.*12/);
  bad(summary({ rows: [{ label: "a", value: "值".repeat(121) }] }), /rows\[0\]\.value.*120/);
  bad(summary({ rows: [{ label: "a", value: "" }] }), /rows\[0\]\.value/);
  bad(
    summary({ rows: [{ label: "a", value: "b", badge: "徽".repeat(13) }] }),
    /rows\[0\]\.badge.*12/,
  );
  bad(summary({ rows: [{ label: "a", value: "b", sub: "s".repeat(121) }] }), /rows\[0\]\.sub.*120/);
  bad(summary({ rows: [{ label: "a", value: "b", tone: "x" }] }), /rows\[0\]\.tone/);
  bad(
    summary({ rows: [{ label: "a", value: "b", ref: { type: "payment", key: "1" } }] }),
    /rows\[0\]\.ref\.type.*model/,
  );
  bad(summary({ rows: [{ label: "a", value: "b", ref: "model" }] }), /rows\[0\]\.ref/);
  bad(summary({ notes: Array.from({ length: 4 }, () => ({ text: "x" })) }), /notes.*3/);
  bad(summary({ notes: [{ text: "提".repeat(121) }] }), /notes\[0\]\.text.*120/);
  bad(
    summary({ sources: Array.from({ length: 6 }, () => ({ title: "t", url: "https://a.com" })) }),
    /sources.*5/,
  );
  bad(
    summary({ sources: [{ title: "t", url: "javascript:alert(1)" }] }),
    /sources\[0\]\.url.*http/,
  );
  bad(summary({ sources: [{ title: "t", url: "ftp://a.com" }] }), /sources\[0\]\.url.*http/);
  bad(summary({ sources: [{ title: "t", url: "不是网址" }] }), /sources\[0\]\.url/);
  bad(
    summary({ sources: [{ title: "标".repeat(41), url: "https://a.com" }] }),
    /sources\[0\]\.title.*40/,
  );
});

test("table 的非法参数：列数、行数、单元格数必须等于列数、单元格内容", () => {
  bad(table({ columns: ["一"] }), /columns.*2.*5/);
  bad(table({ columns: ["1", "2", "3", "4", "5", "6"] }), /columns.*2.*5/);
  bad(table({ columns: undefined }), /columns/);
  bad(table({ columns: ["列".repeat(13), "b"] }), /columns\[0\].*12/);
  bad(table({ rows: [] }), /rows.*1.*20/);
  bad(table({ rows: Array.from({ length: 21 }, () => ({ cells: ["a", "b"] })) }), /rows.*1.*20/);
  bad(table({ rows: [{ cells: ["a"] }] }), /rows\[0\]\.cells.*2/);
  bad(table({ rows: [{ cells: ["a", "b", "c"] }] }), /rows\[0\]\.cells.*2/);
  bad(table({ rows: [{ cells: ["a", "单".repeat(121)] }] }), /rows\[0\]\.cells\[1\].*120/);
  bad(
    table({ rows: [{ cells: ["a", { text: "x", ref: { type: "bogus", key: "k" } }] }] }),
    /rows\[0\]\.cells\[1\]\.ref\.type/,
  );
  bad(table({ rows: [{ cells: ["a", 3] }] }), /rows\[0\]\.cells\[1\]/);
  bad(table({ rows: [{ label: "a", value: "b" }] }), /rows\[0\]\.cells/);
});

test("table 可以到 20 行、5 列；summary 可以到 12 行", () => {
  ok(
    table({
      columns: ["1", "2", "3", "4", "5"],
      rows: Array.from({ length: 20 }, () => ({ cells: ["a", "b", "c", "d", "e"] })),
    }),
  );
  ok(summary({ rows: Array.from({ length: 12 }, (_, i) => ({ label: "k" + i, value: "v" })) }));
});

import { describe, expect, test } from "bun:test";

import { parseCard, resolveRef, safeSourceUrl } from "@/utils/agent/card";

const summary = (over: Record<string, unknown> = {}) => ({
  kind: "summary",
  title: "有可复用的插件",
  tone: "ok",
  rows: [{ label: "插件", value: "New API 视频", badge: "已启用", tone: "ok" }],
  ...over,
});
const table = (over: Record<string, unknown> = {}) => ({
  kind: "table",
  title: "对照",
  columns: ["模型", "协议"],
  rows: [{ cells: ["kling-v3", { text: "newapi", tone: "ok" }] }],
  ...over,
});

describe("parseCard", () => {
  test("结论卡：保留标题、状态色、行、徽标；缺省 tone 为 info", () => {
    const c = parseCard(summary());
    expect(c).toMatchObject({ kind: "summary", title: "有可复用的插件", tone: "ok" });
    expect(c?.kind === "summary" && c.rows[0]).toEqual({
      label: "插件",
      value: "New API 视频",
      badge: "已启用",
      tone: "ok",
    });
    expect(parseCard(summary({ tone: undefined }))?.tone).toBe("info");
    expect(parseCard(summary({ tone: "purple" }))?.tone).toBe("info");
  });

  test("对比表：单元格可以是字符串或对象", () => {
    const c = parseCard(table());
    expect(c?.kind === "table" && c.rows[0].cells).toEqual([
      { text: "kling-v3" },
      { text: "newapi", tone: "ok" },
    ]);
  });

  test("结构不对整张卡不渲染：类型未知、没标题、没行、列数对不上、行缺值", () => {
    expect(parseCard(null)).toBeNull();
    expect(parseCard(summary({ kind: "chart" }))).toBeNull();
    expect(parseCard(summary({ title: "" }))).toBeNull();
    expect(parseCard(summary({ rows: [] }))).toBeNull();
    expect(parseCard(summary({ rows: [{ label: "a" }] }))).toBeNull();
    expect(parseCard(table({ rows: [{ cells: ["只有一格"] }] }))).toBeNull();
    expect(parseCard(table({ columns: ["一"] }))).toBeNull();
    expect(parseCard(table({ rows: [{ cells: ["a", 3] }] }))).toBeNull();
  });

  test("来源：只有合法 http(s) 网址保留链接，其余只留标题", () => {
    const c = parseCard(
      summary({
        sources: [
          { title: "价格页", url: "https://a.com/pricing" },
          { title: "脚本", url: "javascript:alert(1)" },
          { title: "数据", url: "data:text/html,x" },
          { title: "协议相对", url: "//evil.com" },
          { title: "ftp", url: "ftp://a.com" },
        ],
      }),
    );
    expect(c?.sources).toEqual([
      { title: "价格页", url: "https://a.com/pricing" },
      { title: "脚本" },
      { title: "数据" },
      { title: "协议相对" },
      { title: "ftp" },
    ]);
  });

  test("safeSourceUrl", () => {
    expect(safeSourceUrl(" http://a.com ")).toBe("http://a.com/");
    expect(safeSourceUrl("vbscript:x")).toBeUndefined();
    expect(safeSourceUrl("")).toBeUndefined();
  });
});

describe("resolveRef", () => {
  test("后台：模型 / 渠道带选中参数，插件和定价设置只跳页面", () => {
    expect(resolveRef({ type: "model", key: "kling-v3" }, "admin")).toEqual({
      kind: "page",
      to: "/admin/ai/channels?model=kling-v3",
    });
    expect(resolveRef({ type: "channel", key: "yswg" }, "admin")).toEqual({
      kind: "page",
      to: "/admin/ai/channels?channel=yswg",
    });
    expect(resolveRef({ type: "plugin", key: "new-api-video" }, "admin")).toEqual({
      kind: "page",
      to: "/admin/ai/plugins",
    });
    expect(resolveRef({ type: "pricing_settings" }, "admin")).toEqual({
      kind: "page",
      to: "/admin/settings/pricing",
    });
  });

  test("画布：node 定位节点；后台页面类型用不了", () => {
    expect(resolveRef({ type: "node", key: "n1" }, "canvas")).toEqual({ kind: "node", id: "n1" });
    expect(resolveRef({ type: "model", key: "k" }, "canvas")).toBeNull();
    expect(resolveRef({ type: "pricing_settings" }, "canvas")).toBeNull();
  });

  test("后台里出现 node、不认识的类型、模型缺 key、没有 ref：都只显示文字", () => {
    expect(resolveRef({ type: "node", key: "n1" }, "admin")).toBeNull();
    expect(resolveRef({ type: "unknown", key: "1" }, "admin")).toBeNull();
    expect(resolveRef({ type: "model" }, "admin")).toBeNull();
    expect(resolveRef({ type: "node" }, "canvas")).toBeNull();
    expect(resolveRef(undefined, "admin")).toBeNull();
  });
});

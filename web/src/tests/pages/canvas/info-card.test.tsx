import { describe, expect, test } from "bun:test";
import { ReactFlowProvider } from "@xyflow/react";
import { renderToStaticMarkup } from "react-dom/server";
import { MemoryRouter } from "react-router";

import { InfoCard } from "@/pages/canvas/agent/info-card";
import { parseCard, type RefScope } from "@/utils/agent/card";

/** 解析后再渲染，和消息流里的路径一致（坏数据在 parseCard 就被挡掉） */
const html = (raw: unknown, scope: RefScope = "admin") => {
  const card = parseCard(raw);
  if (!card) throw new Error("卡片解析失败");
  return renderToStaticMarkup(
    <MemoryRouter>
      <ReactFlowProvider>
        <InfoCard card={card} scope={scope} />
      </ReactFlowProvider>
    </MemoryRouter>,
  );
};

const summary = (over: Record<string, unknown> = {}) => ({
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
    { label: "渠道", value: "newapi.vbing.site", ref: { type: "channel", key: "12" } },
  ],
  notes: [{ tone: "warn", text: "价格页是动态加载的" }],
  sources: [{ title: "价格页", url: "https://newapi.vbing.site/pricing" }],
  ...over,
});
const table = (rowCount: number, over: Record<string, unknown> = {}) => ({
  kind: "table",
  title: "模型对照",
  tone: "info",
  columns: ["模型", "本地状态"],
  rows: Array.from({ length: rowCount }, (_, i) => ({
    cells: [
      { text: `model-${i}`, ref: { type: "model", key: `model-${i}` } },
      { text: "已配置", tone: "ok" },
    ],
  })),
  ...over,
});

describe("结论卡", () => {
  test("标题、状态图标、行的标签 / 值 / 补充说明 / 徽标、提示、来源都在", () => {
    const out = html(summary());
    expect(out).toContain("有可复用的插件");
    expect(out).toContain("lucide-circle-check");
    expect(out).toContain("text-status-success");
    for (const t of ["插件", "New API 视频", "/v1/videos", "已启用", "价格页是动态加载的"]) {
      expect(out).toContain(t);
    }
    expect(out).toContain("text-status-warning");
  });

  test("tone 映射成现有状态色：warn 黄、bad 红、info 弱化", () => {
    expect(html(summary({ tone: "warn" }))).toContain("lucide-triangle-alert");
    expect(html(summary({ tone: "bad" }))).toContain("text-destructive");
    expect(html(summary({ tone: "info" }))).toContain("lucide-info");
  });

  test("后台：ref 渲染成站内链接并带 ↗，模型 / 渠道带选中参数", () => {
    const out = html(summary());
    expect(out).toContain('href="/admin/ai/plugins"');
    expect(out).toContain('href="/admin/ai/channels?channel=12"');
    expect(out).toContain("lucide-arrow-up-right");
  });

  test("画布：后台页面类型只显示文字，不渲染链接", () => {
    const out = html(summary({ sources: [] }), "canvas");
    expect(out).not.toContain("<a ");
    expect(out).toContain("New API 视频");
  });

  test("画布：node 引用渲染成可点击按钮；后台里同样的 node 引用只显示文字", () => {
    const raw = summary({
      sources: [],
      rows: [{ label: "节点", value: "镜头 1", ref: { type: "node", key: "n_1" } }],
    });
    expect(html(raw, "canvas")).toContain("<button");
    expect(html(raw, "admin")).not.toContain("<button");
  });

  test("白名单外的 ref 类型只显示文字", () => {
    const out = html(
      summary({
        sources: [],
        rows: [{ label: "用户", value: "张三", ref: { type: "unknown", key: "1" } }],
      }),
    );
    expect(out).toContain("张三");
    expect(out).not.toContain("<a ");
    expect(out).not.toContain("<button");
  });

  test("用户引用在后台跳转到用户管理，在画布中只显示文字", () => {
    const raw = summary({
      sources: [],
      rows: [{ label: "用户", value: "张三", ref: { type: "user", key: "1" } }],
    });
    expect(html(raw, "admin")).toContain('href="/admin/users"');
    expect(html(raw, "canvas")).not.toContain("<a ");
  });

  test("来源链接新窗口打开且带 noreferrer noopener；不合法的 URL 不渲染链接", () => {
    const out = html(
      summary({
        rows: [{ label: "a", value: "b" }],
        sources: [
          { title: "价格页", url: "https://a.com/p" },
          { title: "坏链接", url: "javascript:alert(1)" },
        ],
      }),
    );
    expect(out).toContain('href="https://a.com/p"');
    expect(out).toContain('target="_blank"');
    expect(out).toContain('rel="noreferrer noopener"');
    expect(out).toContain("坏链接");
    expect(out).not.toContain("javascript:");
  });
});

describe("对比表", () => {
  test("列头、单元格、状态点（带读屏文字）和引用", () => {
    const out = html(table(2));
    expect(out).toContain("模型");
    expect(out).toContain("本地状态");
    expect(out).toContain("model-0");
    expect(out).toContain("已配置");
    expect(out).toContain("bg-status-success");
    expect(out).toContain("正常");
    expect(out).toContain('href="/admin/ai/channels?model=model-1"');
  });

  test("不超过 6 行：全部显示，没有「还有 N 项」", () => {
    const out = html(table(6));
    expect(out).toContain("model-5");
    expect(out).not.toContain("还有");
  });

  test("超过 6 行：折叠成前 6 行，按钮写明还有几项", () => {
    const out = html(table(9));
    expect(out).toContain("model-5");
    expect(out).not.toContain("model-6");
    expect(out).toContain("还有 3 项");
  });
});

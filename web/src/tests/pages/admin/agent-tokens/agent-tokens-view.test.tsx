import { describe, expect, test } from "bun:test";
import { renderToStaticMarkup } from "react-dom/server";

import type { AgentTokenItem, IssuedAgentToken } from "@/api/admin/agent-token/type.d";
import {
  AgentTokensView,
  IssuedTokenContent,
  type AgentTokensViewProps,
} from "@/pages/admin/agent-tokens/agent-tokens-view";
import { initialTokenPage, tokenPageReducer, type TokenPageState } from "@/utils/admin/agent-token";

const NOW = Date.parse("2026-10-21T12:00:00Z");
const PLAIN = "vca_9f2c1d8e7a6b5c4d3e2f1a0b9c8d7e6f";
const item = (patch: Partial<AgentTokenItem> = {}): AgentTokenItem => ({
  id: 1,
  name: "本机 Claude Code",
  prefix: "vca_9f2c",
  scope: "agent:read",
  expires_at: "2026-11-15T12:00:00Z",
  last_used_at: "2026-10-20T14:10:00Z",
  revoked_at: null,
  created_at: "2026-10-16T00:00:00Z",
  ...patch,
});
const issued: IssuedAgentToken = { item: item(), token: PLAIN };

const noop = () => undefined;
const view = (state: Partial<TokenPageState> = {}, props: Partial<AgentTokensViewProps> = {}) =>
  renderToStaticMarkup(
    <AgentTokensView
      state={{ ...initialTokenPage, status: "ready", ...state }}
      values={{ name: "", days: "30" }}
      errors={{}}
      issuing={false}
      now={NOW}
      onChange={noop}
      onIssue={noop}
      onDismiss={noop}
      onRevoke={noop}
      onRetry={noop}
      {...props}
    />,
  );

describe("分析令牌页：基本内容", () => {
  test("标题、说明一句、签发表单（用途 + 有效期默认 30）", () => {
    const out = view();
    expect(out).toContain("分析令牌");
    expect(out).toContain("agent-trace");
    expect(out).toContain("用途");
    expect(out).toContain("有效期");
    expect(out).toMatch(/<input[^>]*value="30"/);
    expect(out).toContain("签发");
  });

  test("空状态文案", () => {
    expect(view({ items: [] })).toContain("还没有令牌。签发一个，供本地 agent-trace 使用。");
  });

  test("加载失败：提示并给重试", () => {
    const out = view({ status: "error" });
    expect(out).toContain("令牌列表加载失败");
    expect(out).toContain("重试");
  });
});

describe("分析令牌页：表格", () => {
  test("列：用途 / 前缀… / 到期 / 最近使用 / 状态 / 操作", () => {
    const out = view({ items: [item()] });
    for (const head of ["用途", "前缀", "到期", "最近使用", "状态"]) expect(out).toContain(head);
    expect(out).toContain("本机 Claude Code");
    expect(out).toContain("vca_9f2c…");
    expect(out).toMatch(/2026-11-1[56]/);
    expect(out).toMatch(/2026-10-(20|21) /);
  });

  test("从未使用显示「从未使用」", () => {
    expect(view({ items: [item({ last_used_at: null })] })).toContain("从未使用");
  });

  test("状态：有效、已过期，都有「注销」按钮", () => {
    const out = view({
      items: [
        item({ id: 1, name: "甲" }),
        item({ id: 3, name: "丙", expires_at: "2026-10-01T00:00:00Z" }),
      ],
    });
    expect(out).toContain(">有效<");
    expect(out).toContain(">已过期<");
    expect(out).toContain('aria-label="注销 甲"');
    expect(out).toContain('aria-label="注销 丙"');
    expect(out).toContain(">注销<");
    expect(out).not.toContain("吊销");
  });

  test("已注销的令牌不显示（即使列表里混进来了）", () => {
    // 页面状态由 reducer 过滤；这里验证 loaded 之后视图里没有它
    const s = tokenPageReducer(initialTokenPage, {
      type: "loaded",
      items: [
        item({ id: 1, name: "甲" }),
        item({ id: 2, name: "乙", revoked_at: "2026-10-19T00:00:00Z" }),
      ],
    });
    const out = view(s);
    expect(out).toContain("甲");
    expect(out).not.toContain("乙");
    expect(out).not.toContain("已注销");
  });

  test("有令牌时不出现空状态文案", () => {
    expect(view({ items: [item()] })).not.toContain("还没有令牌");
  });
});

describe("分析令牌页：明文只显示一次", () => {
  test("弹窗内容：明文恰好出现一次，带复制和 .traces/.env 的说明", () => {
    const out = renderToStaticMarkup(<IssuedTokenContent token={PLAIN} />);
    expect(out.split(PLAIN).length - 1).toBe(1);
    expect(out).toContain("复制");
    expect(out).toContain(".traces/.env");
    expect(out).toContain("VC_TRACE_TOKEN");
  });

  test("明文不会被渲染在页面正文里（只在弹窗里）：签发后页面本身不再有绿色提示块", () => {
    const out = view({ items: [issued.item], issued });
    expect(out).not.toContain('data-slot="secret-reveal"');
    // 弹窗是挂到 body 的浮层，不在页面正文的 HTML 里
    expect(out).not.toContain("令牌已签发，明文只显示这一次");
  });

  test("点「我已保存」之后（issued 为空）：页面任何位置都没有明文", () => {
    const out = view({ items: [issued.item], issued: null });
    expect(out).not.toContain(PLAIN);
  });

  test("列表只显示前缀，不显示明文", () => {
    expect(view({ items: [issued.item] })).not.toContain(PLAIN);
  });

  test("页面上没有说明段落和底部的 401 提示", () => {
    const out = view({ items: [item()] });
    expect(out).not.toContain("给本地 agent-trace 用的只读令牌");
    expect(out).not.toContain("会收到 401");
    expect(out).not.toContain("写入操作审计");
  });
});

describe("分析令牌页：签发表单", () => {
  test("校验错误显示在对应字段下", () => {
    const out = view({}, { errors: { name: "请填写用途", days: "有效期必须是 1–90 天" } });
    expect(out).toContain("请填写用途");
    expect(out).toContain("有效期必须是 1–90 天");
  });

  test("签发中：按钮禁用", () => {
    const out = view({}, { issuing: true });
    const button = out.match(
      /<button[^>]*>(?:(?!<\/button>).)*签发(?:(?!<\/button>).)*<\/button>/,
    )?.[0];
    expect(button).toMatch(/ disabled=""/);
  });

  test("有效期输入框限定 1–90", () => {
    const out = view();
    expect(out).toContain('min="1"');
    expect(out).toContain('max="90"');
  });
});

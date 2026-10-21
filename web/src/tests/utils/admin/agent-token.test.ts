import { describe, expect, test } from "bun:test";

import { adminAgentTokenEndpoints as ep } from "@/api/admin/agent-token/endpoints";
import type { AgentTokenItem, IssuedAgentToken } from "@/api/admin/agent-token/type.d";
import {
  initialTokenPage,
  runIssue,
  tokenPageReducer,
  tokenStatus,
  validateIssueForm,
} from "@/utils/admin/agent-token";

const NOW = Date.parse("2026-10-21T12:00:00Z");
const item = (patch: Partial<AgentTokenItem> = {}): AgentTokenItem => ({
  id: 1,
  name: "本机 Claude Code",
  prefix: "vca_9f2c",
  scope: "agent:read",
  expires_at: "2026-11-15T00:00:00Z",
  last_used_at: null,
  revoked_at: null,
  created_at: "2026-10-16T00:00:00Z",
  ...patch,
});
const issued = (patch: Partial<AgentTokenItem> = {}): IssuedAgentToken => ({
  item: item(patch),
  token: "vca_9f2c1d8e7a6b5c4d3e2f1a0b9c8d7e6f",
});

describe("接口路径", () => {
  test("列表与签发是同一个地址，注销带令牌 ID", () => {
    expect(ep.list()).toBe("/admin/agent/tokens");
    expect(ep.issue()).toBe("/admin/agent/tokens");
    expect(ep.revoke(7)).toBe("/admin/agent/tokens/7");
  });
});

describe("签发表单校验", () => {
  test("用途为空或只有空格：拒绝", () => {
    for (const name of ["", "   "]) {
      const r = validateIssueForm({ name, days: "30" });
      expect(r.ok).toBe(false);
      if (!r.ok) expect(r.errors.name).toBe("请填写用途");
    }
  });

  test("用途最多 64 字：64 字通过，65 字拒绝；按字数而不是字节", () => {
    expect(validateIssueForm({ name: "测".repeat(64), days: "30" }).ok).toBe(true);
    const r = validateIssueForm({ name: "测".repeat(65), days: "30" });
    expect(r.ok).toBe(false);
    if (!r.ok) expect(r.errors.name).toBe("用途最多 64 个字");
  });

  test("有效期 0、91、负数、小数、空、非数字：拒绝", () => {
    for (const days of ["0", "91", "-1", "1.5", "", "abc", "100"]) {
      const r = validateIssueForm({ name: "本机", days });
      expect(r.ok).toBe(false);
      if (!r.ok) expect(r.errors.days).toBe("有效期必须是 1–90 天");
    }
  });

  test("有效期 1 和 90 是边界，通过；用途首尾空格去掉", () => {
    expect(validateIssueForm({ name: " 本机 ", days: "1" })).toEqual({
      ok: true,
      value: { name: "本机", days: 1 },
    });
    expect(validateIssueForm({ name: "本机", days: "90" })).toEqual({
      ok: true,
      value: { name: "本机", days: 90 },
    });
  });

  test("用途和有效期都不对：两个错误一起给", () => {
    const r = validateIssueForm({ name: "", days: "0" });
    expect(r.ok).toBe(false);
    if (!r.ok) expect(Object.keys(r.errors).sort()).toEqual(["days", "name"]);
  });
});

describe("runIssue：校验不过不发请求，通过则发规范化后的请求", () => {
  test("校验不过：不调用接口", async () => {
    let called = 0;
    const r = await runIssue({ name: "", days: "30" }, async () => {
      called++;
      return issued();
    });
    expect(r.ok).toBe(false);
    expect(called).toBe(0);
  });

  test("通过：发 {name, days}，返回签发结果", async () => {
    const sent: unknown[] = [];
    const r = await runIssue({ name: " 本机 Codex ", days: "45" }, async (req) => {
      sent.push(req);
      return issued({ name: req.name });
    });
    expect(sent).toEqual([{ name: "本机 Codex", days: 45 }]);
    expect(r.ok && r.result.token.startsWith("vca_")).toBe(true);
  });
});

describe("令牌状态", () => {
  test("有效 / 已注销 / 已过期；注销优先于过期", () => {
    expect(tokenStatus(item(), NOW)).toBe("active");
    expect(tokenStatus(item({ revoked_at: "2026-10-20T00:00:00Z" }), NOW)).toBe("revoked");
    expect(tokenStatus(item({ expires_at: "2026-10-01T00:00:00Z" }), NOW)).toBe("expired");
    expect(
      tokenStatus(
        item({ expires_at: "2026-10-01T00:00:00Z", revoked_at: "2026-09-30T00:00:00Z" }),
        NOW,
      ),
    ).toBe("revoked");
  });

  test("刚好到期那一刻算已过期", () => {
    expect(tokenStatus(item({ expires_at: "2026-10-21T12:00:00Z" }), NOW)).toBe("expired");
  });
});

describe("令牌页状态", () => {
  test("加载列表后进入 ready；失败后若已有数据保持 ready，没有数据才是 error", () => {
    const loaded = tokenPageReducer(initialTokenPage, { type: "loaded", items: [item()] });
    expect(loaded.status).toBe("ready");
    expect(tokenPageReducer(loaded, { type: "load-failed" }).status).toBe("ready");
    expect(tokenPageReducer(initialTokenPage, { type: "load-failed" }).status).toBe("error");
  });

  test("签发成功：明文进 issued，新令牌排到列表最前，列表里只有记录没有明文", () => {
    const base = tokenPageReducer(initialTokenPage, { type: "loaded", items: [item()] });
    const next = tokenPageReducer(base, {
      type: "issued",
      result: issued({ id: 2, name: "新的" }),
    });
    expect(next.issued?.token).toBe("vca_9f2c1d8e7a6b5c4d3e2f1a0b9c8d7e6f");
    expect(next.items.map((i) => i.id)).toEqual([2, 1]);
    expect(JSON.stringify(next.items)).not.toContain("vca_9f2c1d8e");
  });

  test("明文只显示一次：点「我已保存」后没了，之后重新加载列表也不会回来", () => {
    let s = tokenPageReducer(initialTokenPage, { type: "issued", result: issued() });
    s = tokenPageReducer(s, { type: "dismissed" });
    expect(s.issued).toBeNull();
    s = tokenPageReducer(s, { type: "loaded", items: [item()] });
    expect(s.issued).toBeNull();
  });

  test("明文还没关时重新加载列表：明文保留，不会因为刷新丢掉", () => {
    let s = tokenPageReducer(initialTokenPage, { type: "issued", result: issued() });
    s = tokenPageReducer(s, { type: "loaded", items: [item()] });
    expect(s.issued?.token).toBeTruthy();
  });

  test("再签发一枚：上一枚的明文被换掉", () => {
    let s = tokenPageReducer(initialTokenPage, { type: "issued", result: issued({ id: 1 }) });
    s = tokenPageReducer(s, {
      type: "issued",
      result: { item: item({ id: 2 }), token: "vca_second" },
    });
    expect(s.issued?.token).toBe("vca_second");
  });

  test("注销：该行从列表里消失，别的行不变；重复注销不报错", () => {
    let s = tokenPageReducer(initialTokenPage, {
      type: "loaded",
      items: [item({ id: 1 }), item({ id: 2 })],
    });
    s = tokenPageReducer(s, { type: "revoked", id: 1, at: "2026-10-21T01:00:00Z" });
    expect(s.items.map((i) => i.id)).toEqual([2]);
    s = tokenPageReducer(s, { type: "revoked", id: 1, at: "2026-10-21T02:00:00Z" });
    expect(s.items.map((i) => i.id)).toEqual([2]);
  });

  test("加载列表时过滤掉已注销的令牌", () => {
    const s = tokenPageReducer(initialTokenPage, {
      type: "loaded",
      items: [item({ id: 1 }), item({ id: 2, revoked_at: "2026-10-20T00:00:00Z" })],
    });
    expect(s.items.map((i) => i.id)).toEqual([1]);
  });
});

import { describe, expect, test } from "bun:test";

import type { MCPTokenItem } from "@/api/admin/mcp-project/type";
import {
  MCP_SCOPES,
  MCP_SCOPE_GROUPS,
  TOKEN_STATUS_LABEL,
  dropMCPScope,
  expandMCPScopes,
  formatMCPTime,
  mcpConfigSnippet,
  scopeLabel,
  scopeRequiresLabel,
  tokenStatus,
} from "@/utils/admin/mcp-project";

const token = (patch: Partial<MCPTokenItem> = {}): MCPTokenItem => ({
  id: "1",
  project_id: "2",
  name: "本机 Codex",
  prefix: "vcm_9f2c",
  expires_at: "2026-12-23T10:00:00Z",
  created_at: "2026-11-23T10:00:00Z",
  ...patch,
});

const NOW = Date.parse("2026-11-23T12:00:00Z");

describe("scopeLabel", () => {
  test("认识的权限回中文名", () => {
    expect(scopeLabel("canvas:browse")).toBe("浏览目录");
    expect(scopeLabel("canvas:read")).toBe("读画布");
    expect(scopeLabel("canvas:media")).toBe("查看素材");
    expect(scopeLabel("canvas:write")).toBe("改画布");
    expect(scopeLabel("canvas:layout")).toBe("重排布局");
    expect(scopeLabel("canvas:compose")).toBe("合成成片");
    expect(scopeLabel("drama:read")).toBe("读短剧");
    expect(scopeLabel("drama:write")).toBe("改剧本");
    expect(scopeLabel("drama:run")).toBe("跑短剧任务");
  });

  test("不认识的权限原样返回，便于发现后台写错", () => {
    expect(scopeLabel("admin:all")).toBe("admin:all");
  });

  test("十个档位按能力域分组，花钱的两档各在域尾", () => {
    expect(MCP_SCOPES.map((s) => s.value)).toEqual([
      "canvas:browse",
      "canvas:read",
      "canvas:media",
      "canvas:write",
      "canvas:layout",
      "canvas:compose",
      "canvas:generate",
      "drama:read",
      "drama:write",
      "drama:run",
    ]);
  });

  test("短剧是独立能力域，不挂 canvas: 前缀", () => {
    const drama = MCP_SCOPES.filter((s) => s.group === "drama").map((s) => s.value);
    expect(drama).toEqual(["drama:read", "drama:write", "drama:run"]);
    expect(drama.every((s) => s.startsWith("drama:"))).toBe(true);
  });

  test("每个分组都有档位，不留空块", () => {
    for (const group of MCP_SCOPE_GROUPS) {
      expect(MCP_SCOPES.some((s) => s.group === group.key)).toBe(true);
    }
  });
});

describe("权限依赖不互斥", () => {
  test("勾改画布自动带上读画布与浏览目录", () => {
    expect(expandMCPScopes(["canvas:write"])).toEqual([
      "canvas:browse",
      "canvas:read",
      "canvas:write",
    ]);
  });

  test("勾查看素材自动带上读画布", () => {
    expect(expandMCPScopes(["canvas:media"])).toEqual([
      "canvas:browse",
      "canvas:read",
      "canvas:media",
    ]);
  });

  test("勾生成素材自动带上读画布与浏览目录", () => {
    expect(expandMCPScopes(["canvas:generate"])).toEqual([
      "canvas:browse",
      "canvas:read",
      "canvas:generate",
    ]);
  });

  test("只勾浏览目录不会顺带给出读画布", () => {
    expect(expandMCPScopes(["canvas:browse"])).toEqual(["canvas:browse"]);
  });

  test("取消读画布会连带取消依赖它的档位", () => {
    expect(dropMCPScope(["canvas:browse", "canvas:read", "canvas:write"], "canvas:read")).toEqual([
      "canvas:browse",
    ]);
  });

  test("取消浏览目录会把下游全部取消", () => {
    expect(
      dropMCPScope(["canvas:browse", "canvas:read", "canvas:compose"], "canvas:browse"),
    ).toEqual([]);
  });

  test("依赖文案用于界面提示", () => {
    expect(scopeRequiresLabel("canvas:write")).toBe("读画布");
    expect(scopeRequiresLabel("canvas:browse")).toBe("");
  });

  test("改剧本自动带上读短剧，但不逼着开画布权限", () => {
    expect(expandMCPScopes(["drama:write"])).toEqual(["drama:read", "drama:write"]);
  });

  test("跑短剧任务自动带上读短剧", () => {
    expect(expandMCPScopes(["drama:run"])).toEqual(["drama:read", "drama:run"]);
  });

  test("只读短剧不会顺带给出任何画布权限", () => {
    expect(expandMCPScopes(["drama:read"])).toEqual(["drama:read"]);
  });

  test("短剧与画布各自独立：勾短剧改剧本不带读画布", () => {
    expect(expandMCPScopes(["drama:write"])).not.toContain("canvas:read");
  });

  test("取消读短剧会连带取消改剧本与跑任务", () => {
    expect(dropMCPScope(["drama:read", "drama:write", "drama:run"], "drama:read")).toEqual([]);
  });

  test("短剧的依赖文案指回短剧自己的一档", () => {
    expect(scopeRequiresLabel("drama:run")).toBe("读短剧");
    expect(scopeRequiresLabel("drama:read")).toBe("");
  });
});

describe("tokenStatus", () => {
  test("未注销且没过期是有效", () => {
    expect(tokenStatus(token(), NOW)).toBe("active");
  });

  test("过了到期时间是已过期", () => {
    expect(tokenStatus(token({ expires_at: "2026-11-01T00:00:00Z" }), NOW)).toBe("expired");
  });

  test("注销优先于过期", () => {
    expect(
      tokenStatus(
        token({ expires_at: "2026-11-01T00:00:00Z", revoked_at: "2026-11-02T00:00:00Z" }),
        NOW,
      ),
    ).toBe("revoked");
  });

  test("时间解析不了时不误判成过期", () => {
    expect(tokenStatus(token({ expires_at: "不是时间" }), NOW)).toBe("active");
  });

  test("三个状态都有中文名", () => {
    expect(TOKEN_STATUS_LABEL.active).toBe("有效");
    expect(TOKEN_STATUS_LABEL.expired).toBe("已过期");
    expect(TOKEN_STATUS_LABEL.revoked).toBe("已注销");
  });
});

describe("formatMCPTime", () => {
  test("空值显示破折号", () => {
    expect(formatMCPTime(undefined)).toBe("—");
    expect(formatMCPTime(null)).toBe("—");
  });

  test("合法时间格式化成可读文本", () => {
    expect(formatMCPTime("2026-11-23T10:00:00Z")).toContain("2026");
  });

  test("非法时间原样返回", () => {
    expect(formatMCPTime("坏值")).toBe("坏值");
  });
});

describe("mcpConfigSnippet", () => {
  test("拼出带 Authorization 的宿主配置，端点用当前 origin", () => {
    const snippet = mcpConfigSnippet("vcm_abc", "/api/v1/mcp", "https://lj.igeewa.com");
    expect(JSON.parse(snippet)).toEqual({
      mcpServers: {
        "video-canvas": {
          type: "http",
          url: "https://lj.igeewa.com/api/v1/mcp",
          headers: { Authorization: "Bearer vcm_abc" },
          timeout: 60_000,
        },
      },
    });
  });

  test("origin 末尾有斜杠时不拼出双斜杠", () => {
    const snippet = mcpConfigSnippet("vcm_abc", "/api/v1/mcp", "https://a.com/");
    expect(JSON.parse(snippet).mcpServers["video-canvas"].url).toBe("https://a.com/api/v1/mcp");
  });

  test("客户端超时按毫秒给（60 秒 = 60000），不是秒", () => {
    const snippet = mcpConfigSnippet("vcm_abc", "/api/v1/mcp", "https://a.com");
    expect(JSON.parse(snippet).mcpServers["video-canvas"].timeout).toBe(60_000);
  });
});

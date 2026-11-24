import type { MCPTokenItem } from "@/api/admin/mcp-project/type";

/**
 * 能力分组：MCP 是独立能力出口，画布与短剧是两条并列的能力域。
 * 界面上分开摆，避免「勾了画布权限就能改剧本」这种误解。
 */
export const MCP_SCOPE_GROUPS = [
  { key: "canvas", label: "画布" },
  { key: "drama", label: "短剧" },
] as const;

/** 权限档位：按能力域分组，域内从最安全到影响最大，界面与后端回显都用这个顺序。 */
export const MCP_SCOPES = [
  {
    value: "canvas:browse",
    group: "canvas",
    label: "浏览目录",
    hint: "列画布、列模型、搜技能、查任务，全部无副作用",
  },
  {
    value: "canvas:read",
    group: "canvas",
    label: "读画布",
    hint: "读节点与连线结构；改画布前必须先有它",
  },
  {
    value: "canvas:media",
    group: "canvas",
    label: "查看素材",
    hint: "看图片、看视频、从视频抓一帧",
  },
  { value: "canvas:write", group: "canvas", label: "改画布", hint: "改节点属性与生成提示词" },
  { value: "canvas:layout", group: "canvas", label: "重排布局", hint: "移动节点、整理分组与顺序" },
  {
    value: "canvas:compose",
    group: "canvas",
    label: "合成成片",
    hint: "触发成片合成，消耗算力不消耗积分",
  },
  {
    value: "canvas:generate",
    group: "canvas",
    label: "生成素材",
    hint: "出图、出视频、出配音，花你自己名下的积分",
  },
  {
    value: "drama:read",
    group: "drama",
    label: "读短剧",
    hint: "列短剧、读大纲人物、读单集全文与片段",
  },
  {
    value: "drama:write",
    group: "drama",
    label: "改剧本",
    hint: "改大纲人物、分集标题梗概剧本、片段提示词与秒数",
  },
  {
    value: "drama:run",
    group: "drama",
    label: "跑短剧任务",
    hint: "分集剧本、资产拆解、分镜、合成整集，花你自己名下的积分",
  },
] as const;

/**
 * 各档的隐含依赖，必须与后端 `model.MCPScopeRequires` 保持一致。
 * 权限之间不该互斥：勾「改画布」却不给「读画布」，等于给了把用不了的钥匙（工具改之前必须先读）。
 *
 * 短剧一族不依赖 canvas:*：短剧画布不在画布列表里，靠 drama_list 自己发现，逼用户连带开画布权限没有道理。
 */
export const MCP_SCOPE_REQUIRES: Record<string, string[]> = {
  "canvas:read": ["canvas:browse"],
  "canvas:media": ["canvas:read"],
  "canvas:write": ["canvas:read"],
  "canvas:layout": ["canvas:read"],
  "canvas:compose": ["canvas:read"],
  "canvas:generate": ["canvas:read"],
  "drama:write": ["drama:read"],
  "drama:run": ["drama:read"],
};

const MCP_SCOPE_VALUES = MCP_SCOPES.map((s) => s.value) as readonly string[];

/**
 * 把勾选结果补成闭包：缺的依赖一层层补上，按 MCP_SCOPES 的顺序输出。
 * @param scopes 用户勾的档位
 * @returns 补全依赖后的档位
 */
export const expandMCPScopes = (scopes: string[]): string[] => {
  const have = new Set<string>();
  const add = (scope: string) => {
    if (have.has(scope)) return;
    have.add(scope);
    for (const dep of MCP_SCOPE_REQUIRES[scope] ?? []) add(dep);
  };
  for (const s of scopes) {
    if (MCP_SCOPE_VALUES.includes(s)) add(s);
  }
  return MCP_SCOPE_VALUES.filter((s) => have.has(s));
};

/**
 * 取消某一档：连同依赖它的档一起取消（依赖不成立，留着就是配错的）。
 * @param scopes 当前勾选
 * @param value 要取消的档位
 * @returns 取消后的档位（仍会补全剩余依赖）
 */
export const dropMCPScope = (scopes: string[], value: string): string[] => {
  const drop = new Set<string>([value]);
  for (let changed = true; changed;) {
    changed = false;
    for (const s of scopes) {
      if (drop.has(s)) continue;
      if ((MCP_SCOPE_REQUIRES[s] ?? []).some((dep) => drop.has(dep))) {
        drop.add(s);
        changed = true;
      }
    }
  }
  return expandMCPScopes(scopes.filter((s) => !drop.has(s)));
};

/** 某一档的依赖，用来在界面上写「需要：读画布」。 */
export const scopeRequiresLabel = (scope: string): string =>
  (MCP_SCOPE_REQUIRES[scope] ?? [])
    .map((dep) => MCP_SCOPES.find((s) => s.value === dep)?.label ?? dep)
    .join("、");

/** 权限值的显示名；不认识的直接回原值，便于发现后台写错了。 */
export const scopeLabel = (scope: string) =>
  MCP_SCOPES.find((s) => s.value === scope)?.label ?? scope;

/** 令牌状态：未注销且没过期才算有效。 */
export type MCPTokenStatus = "active" | "expired" | "revoked";

/** 判断令牌状态。now 由调用方给，便于测试与定时刷新。 */
export const tokenStatus = (token: MCPTokenItem, now: number): MCPTokenStatus => {
  if (token.revoked_at) return "revoked";
  const expires = Date.parse(token.expires_at);
  if (!Number.isFinite(expires)) return "active";
  return expires <= now ? "expired" : "active";
};

/** 令牌状态的中文名。 */
export const TOKEN_STATUS_LABEL: Record<MCPTokenStatus, string> = {
  active: "有效",
  expired: "已过期",
  revoked: "已注销",
};

/**
 * 生成可直接粘贴的配置片段：端点用当前站点的 origin，换域名部署不用改后台配置。
 *
 * `timeout` 的单位是**毫秒**（60 秒写作 60000），别和服务端「MCP 调用超时」混淆——
 * 那一项在注册设置里配，单位是秒。客户端拿到的值偏小会先把请求掐断，宁可给足。
 */
export const mcpConfigSnippet = (token: string, endpoint: string, origin: string) => {
  const url = `${origin.replace(/\/$/, "")}${endpoint}`;
  return JSON.stringify(
    {
      mcpServers: {
        "video-canvas": {
          type: "http",
          url,
          headers: { Authorization: `Bearer ${token}` },
          timeout: 60_000,
        },
      },
    },
    null,
    2,
  );
};

/** 格式化时间，空值显示「—」。 */
export const formatMCPTime = (value?: string | null) => {
  if (!value) return "—";
  const t = Date.parse(value);
  if (!Number.isFinite(t)) return value;
  return new Date(t).toLocaleString("zh-CN", { hour12: false });
};

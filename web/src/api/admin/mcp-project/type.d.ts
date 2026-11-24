/** 一个 MCP 接入项目（后台列表项；不含任何凭证） */
export interface MCPProjectItem {
  /** 项目 ID（十进制字符串，雪花 id 超出 JS 安全整数） */
  id: string;
  /** 项目名称，1–64 字 */
  name: string;
  /** 归属用户 ID：读写与积分都算在这个用户名下 */
  owner_user_id: string;
  /** 归属用户昵称，用户被删后为空 */
  owner_name?: string;
  /** 权限档位：canvas:read / canvas:write / canvas:compose */
  scopes: string[];
  /** 绑定的画布 ID */
  canvas_code: string;
  /** 画布标题，画布被删后为空 */
  canvas_title?: string;
  /** 是否启用：停用后该项目全部令牌立刻失效 */
  enabled: boolean;
  /**
   * 这个项目唯一的一枚令牌；还没生成过（老数据）就是空。
   * 一个项目一个接入，不存在「多枚令牌」的列表。
   */
  token?: MCPTokenItem;
  /** 最近一次调用的时间，从未使用为 null */
  last_used_at?: string;
  created_at: string;
  updated_at: string;
}

/** 项目列表 */
export interface MCPProjectList {
  items: MCPProjectItem[];
}

/** 新建 / 修改项目的请求体 */
export interface MCPProjectReq {
  name: string;
  /** 归属用户 ID（十进制字符串）；只有管理员代建时要给，用户自助入口忽略它 */
  owner_user_id?: string;
  /** 默认画布 code（32 位十六进制，可空）；必须属于归属用户 */
  canvas_code?: string;
  scopes: string[];
  /** 修改时才带：不传表示保持原状 */
  enabled?: boolean;
}

/** 一个项目令牌（永远不含明文） */
export interface MCPTokenItem {
  id: string;
  project_id: string;
  /** 用途备注 */
  name: string;
  /** 明文前 8 位，列表里靠它辨认 */
  prefix: string;
  /** 能不能取回明文（早期签发的令牌没存密文，只能重新签发） */
  has_secret: boolean;
  expires_at: string;
  last_used_at?: string;
  revoked_at?: string;
  created_at: string;
}

/** 签发令牌 */
export interface IssueMCPTokenReq {
  /** 用途备注，1–64 字；留空由服务端用默认名（一个项目只有一枚令牌，用途没有区分余地） */
  name?: string;
  /** 有效天数，1–90；不传就用后台配的默认值 */
  days?: number;
}

/** 签发结果：明文与端点只在这里出现一次 */
export interface IssuedMCPToken {
  item: MCPTokenItem;
  /** 令牌明文（vcm_ 开头），库里只存哈希 */
  token: string;
  /** 端点路径，前端补上 origin 就是可直接粘贴的地址 */
  endpoint: string;
}

/**
 * 新建项目的结果：项目 + 首枚令牌。
 *
 * 一个项目就是一个接入点，建项目时令牌一起签好，用户拿到响应就能直接粘配置，
 * 不用再点一次「签发」。
 */
export interface MCPProjectCreated {
  item: MCPProjectItem;
  issued: IssuedMCPToken;
}

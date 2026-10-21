/** 一枚分析令牌（列表和签发响应共用；永远不含明文） */
export interface AgentTokenItem {
  /** 令牌 ID */
  id: number;
  /** 用途备注，1–64 字 */
  name: string;
  /** 明文的前 8 位（如 vca_9f2c），列表里靠它辨认 */
  prefix: string;
  /** 权限范围，目前只有 agent:read */
  scope: string;
  /** 到期时间（RFC3339） */
  expires_at: string;
  /** 最近一次使用的时间，从未使用为 null */
  last_used_at: string | null;
  /** 注销时间，未注销为 null */
  revoked_at: string | null;
  /** 签发时间（RFC3339） */
  created_at: string;
}

/** 令牌列表 */
export interface AgentTokenList {
  /** 全部令牌，新的在前 */
  items: AgentTokenItem[];
}

/** 签发令牌 */
export interface IssueAgentTokenReq {
  /** 用途备注，1–64 字 */
  name: string;
  /** 有效天数，1–90 */
  days: number;
}

/** 签发结果 */
export interface IssuedAgentToken {
  /** 新令牌的记录 */
  item: AgentTokenItem;
  /** 令牌明文（vca_ 开头）：只有这一次能看到，库里只存哈希 */
  token: string;
}

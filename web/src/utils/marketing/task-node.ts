/**
 * 营销片出片任务的 node_id 前缀（mkt:{脚本 id}）。
 * 和短剧的 seg:、首页的 rec: 一样靠前缀区分（后端不校验 node_id），任务进度按它归到脚本卡。
 */
export const MARKETING_NODE_PREFIX = "mkt:";

/** 脚本对应的出片任务 node_id */
export const marketingNodeId = (scriptId: string): string => `${MARKETING_NODE_PREFIX}${scriptId}`;

/** node_id 是不是营销片出片任务 */
export const isMarketingNode = (nodeId: string): boolean =>
  nodeId.startsWith(MARKETING_NODE_PREFIX);

/** 从 node_id 取脚本 id；不是营销片任务返回 null */
export const scriptIdOfNode = (nodeId: string): string | null =>
  isMarketingNode(nodeId) ? nodeId.slice(MARKETING_NODE_PREFIX.length) : null;

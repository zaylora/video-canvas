/** 合成任务的状态；queued / running 是非终态 */
export type ComposeStatus = "queued" | "running" | "succeeded" | "failed" | "canceled";

/** 一次成片合成任务：接口返回与前端状态共用这一份结构 */
export type CanvasComposeJob = {
  id: string;
  canvas_id: string;
  node_id: string;
  mode?: string;
  status: ComposeStatus;
  fingerprint: string;
  /** 成片素材 id（十进制），只有成功时才有 */
  result_asset_id?: string;
  /** 成片的稳定访问地址（/files/...），前端直接播放和下载 */
  result_asset_url?: string;
  duration_ms: number;
  segment_count: number;
  error?: string;
  /** 只在 active 查询里出现：当前输入和这条任务的输入不一致，成片已过期 */
  stale?: boolean;
};

/** 合成的触发方式：只用于排查和区分入口，不影响执行 */
export type ComposeMode = "storyboard" | "compose" | "selection";

/** 提交合成：asset_ids 的顺序就是成片顺序 */
export type CanvasComposeRequest = {
  node_id: string;
  asset_ids: string[];
  seqs?: number[];
  mode?: ComposeMode;
};

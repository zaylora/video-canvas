import type { CanvasVersion } from "@/utils/canvas/collaboration-version";

/** 「从剧本拆分镜」接口的入参 */
export interface StoryboardSplitRequest {
  /** 分镜表节点 id，用来做「同一张表同时只拆一次」的锁 */
  node_id: string;
  /** 剧本正文：连了多个剧本节点时由前端按连线顺序拼好 */
  script: string;
  /** Agent 模型 key，空串表示用后台第一个可用的 */
  model: string;
  mode?: "append" | "replace";
  collaboration?: { epoch: CanvasVersion; revision: CanvasVersion };
}

/** 后端返回的一镜，字段和分镜表的 StoryboardShot 对齐（没有 id，由前端分配） */
export interface StoryboardSplitShotDto {
  scene: string;
  shot: string;
  camera: string;
  action: string;
  dialogue: string;
  duration: number;
}

/** 拆分镜任务的状态：pending / running 是进行中，succeeded / failed 是终态 */
export type StoryboardSplitStatus = "pending" | "running" | "succeeded" | "failed";

/**
 * 拆分镜任务的快照：提交接口、按节点 / 按 id 查询、WebSocket `storyboard.split` 推送用同一个形状。
 * 提交时只拿到 pending 快照，结果靠推送或补查拿到。
 */
export interface StoryboardSplitJob {
  id: string;
  canvas_id: string;
  node_id: string;
  status: StoryboardSplitStatus;
  collaboration_epoch?: CanvasVersion;
  /** 实际用的模型 key（用户选的下架时会回落到第一个可用的） */
  model: string;
  /** 只在 succeeded 时有 */
  shots?: StoryboardSplitShotDto[];
  /** 实扣积分，失败不扣 */
  credits: number;
  /** 只在 failed 时有 */
  error?: string;
}

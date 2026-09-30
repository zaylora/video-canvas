import type { TaskView } from "@/api/generation-task/type";
import type { CanvasNodeData } from "@/types";

export const makeTask = (over: Partial<TaskView> = {}): TaskView => ({
  id: 1,
  canvas_id: 10,
  node_id: "n1",
  kind: "video",
  model_id: "rh-1",
  status: "running",
  progress: null,
  outputs: null,
  error_code: null,
  error_message: null,
  credits: 10,
  version: 1,
  deadline_at: "2026-09-29T10:30:00Z",
  created_at: "2026-09-29T10:00:00Z",
  finished_at: null,
  ...over,
});

export const makeData = (over: Partial<CanvasNodeData> = {}): CanvasNodeData => ({
  kind: "video",
  label: "视频",
  ...over,
});

export const succeeded = (over: Partial<TaskView> = {}) =>
  makeTask({
    status: "succeeded",
    version: 5,
    outputs: [
      {
        asset_id: 77,
        url: "/files/a.mp4",
        media_type: "video",
        duration_ms: 5000,
        width: 1280,
        height: 720,
      },
    ],
    finished_at: "2026-09-29T10:03:00Z",
    ...over,
  });

/** 文本任务成功：产出只有 text，没有 asset_id / url */
export const textSucceeded = (text: string, over: Partial<TaskView> = {}) =>
  makeTask({
    kind: "text",
    status: "succeeded",
    version: 5,
    outputs: [{ media_type: "text", text }],
    finished_at: "2026-09-29T10:00:03Z",
    ...over,
  });

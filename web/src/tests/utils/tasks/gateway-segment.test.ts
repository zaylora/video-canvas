import { afterEach, beforeEach, describe, expect, spyOn, test, type Mock } from "bun:test";

import { submitSegmentTask } from "@/utils/tasks/gateway";
import service from "@/utils/requests/service";
import { makeTask } from "./fixtures";

describe("gateway.submitSegmentTask：片段视频 / 首帧走统一入口", () => {
  let post: Mock<typeof service.post>;
  let get: Mock<typeof service.get>;
  beforeEach(() => {
    post = spyOn(service, "post");
    get = spyOn(service, "get").mockResolvedValue({ available: 0 });
  });
  afterEach(() => {
    post.mockRestore();
    get.mockRestore();
  });

  test("视频走 generate，首帧走 keyframe，都带 Idempotency-Key，返回任务快照", async () => {
    post.mockResolvedValue({ task: makeTask({ id: 501, node_id: "seg:5d2a", status: "pending" }) });
    const body = { model_id: "veo", input: { prompt: "x" } };
    const task = await submitSegmentTask("video", "5d2a", body);
    expect(String(task.id)).toBe("501");
    expect(post.mock.calls[0][0]).toBe("/drama-segments/5d2a/generate");
    const headers = (post.mock.calls[0][2] as { headers: Record<string, string> }).headers;
    expect(headers["Idempotency-Key"]).toBeTruthy();
    await submitSegmentTask("image", "5d2a", body);
    expect(post.mock.calls[1][0]).toBe("/drama-segments/5d2a/keyframe");
  });
});

import { describe, expect, test } from "bun:test";

import {
  buildSubmittedPatch,
  describeSubmitError,
  isRetryableSubmitError,
  settleSubmission,
  submitWithRetry,
} from "@/utils/tasks/submit";
import { makeTask } from "./fixtures";

describe("describeSubmitError：提交处就地提示", () => {
  test("按后端业务码翻译", () => {
    expect(describeSubmitError({ code: 40001 }).kind).toBe("credits");
    expect(describeSubmitError({ code: 40002 }).kind).toBe("limit");
    expect(describeSubmitError({ code: 40003 }).kind).toBe("unavailable");
    expect(describeSubmitError({ code: 40007 }).kind).toBe("asset");
  });

  test("40006 带上后端的字段错误信息", () => {
    const info = describeSubmitError({ code: 40006, message: "duration 取值不合法" });
    expect(info.kind).toBe("invalid");
    expect(info.message).toContain("duration 取值不合法");
  });

  test("没有业务码时按 HTTP 状态兜底：402 / 429", () => {
    expect(describeSubmitError({ code: "HTTP_402", status: 402 }).kind).toBe("credits");
    expect(describeSubmitError({ code: "HTTP_429", status: 429 }).kind).toBe("limit");
  });

  test("429 / 40002 的文案是“排队任务已达上限”，不再说“超过并发”", () => {
    const text = "排队任务已达上限，请等前面的任务完成后再试";
    expect(describeSubmitError({ code: 40002 }).message).toBe(text);
    expect(describeSubmitError({ code: "HTTP_429", status: 429 }).message).toBe(text);
  });

  test("网络问题与未知错误", () => {
    expect(describeSubmitError({ code: "NETWORK_ERROR", status: 0 }).kind).toBe("network");
    expect(describeSubmitError({ code: "TIMEOUT", status: 0 }).kind).toBe("network");
    expect(describeSubmitError(new Error("boom")).message).toBe("boom");
    expect(describeSubmitError(undefined).kind).toBe("unknown");
  });
});

describe("submitWithRetry：同一次点击的重试复用同一个 Idempotency-Key", () => {
  const noSleep = async () => undefined;

  test("网络错误后重试，全程同一个 key", async () => {
    const keys: string[] = [];
    let calls = 0;
    const result = await submitWithRetry(
      async (key) => {
        keys.push(key);
        calls += 1;
        if (calls < 3) throw { code: "NETWORK_ERROR", status: 0 };
        return "ok";
      },
      "key-1",
      { sleep: noSleep },
    );
    expect(result).toBe("ok");
    expect(keys).toEqual(["key-1", "key-1", "key-1"]);
  });

  test("4xx（积分不足等确定答复）不重试", async () => {
    let calls = 0;
    await expect(
      submitWithRetry(
        async () => {
          calls += 1;
          throw { code: 40001, status: 402 };
        },
        "k",
        { sleep: noSleep },
      ),
    ).rejects.toMatchObject({ code: 40001 });
    expect(calls).toBe(1);
  });

  test("重试次数有上限，耗尽后抛出最后一次错误", async () => {
    let calls = 0;
    await expect(
      submitWithRetry(
        async () => {
          calls += 1;
          throw { code: "HTTP_503", status: 503 };
        },
        "k",
        { retries: 2, sleep: noSleep },
      ),
    ).rejects.toMatchObject({ status: 503 });
    expect(calls).toBe(3);
  });

  test("哪些错误可重试", () => {
    expect(isRetryableSubmitError({ code: "TIMEOUT", status: 0 })).toBe(true);
    expect(isRetryableSubmitError({ status: 502 })).toBe(true);
    expect(isRetryableSubmitError({ status: 400 })).toBe(false);
    expect(isRetryableSubmitError({ status: 429 })).toBe(false);
  });
});

describe("buildSubmittedPatch：提交成功后写进节点的补丁", () => {
  test("文本节点：running + taskId，清正文与错误，不碰素材字段", () => {
    expect(buildSubmittedPatch("script", "9")).toEqual({
      taskId: "9",
      status: "running",
      text: null,
      error: null,
    });
  });

  test("视频节点：running + taskId，清素材相关字段", () => {
    expect(buildSubmittedPatch("video", "9")).toMatchObject({
      taskId: "9",
      status: "running",
      src: null,
      assetId: undefined,
      uploaded: false,
      error: null,
    });
    expect("text" in buildSubmittedPatch("video", "9")).toBe(false);
  });
});

describe("buildSubmittedPatch：一次提交了多个任务", () => {
  test("多个任务：taskId 是第一个，pendingTaskIds 是全部", () => {
    const patch = buildSubmittedPatch("image", ["5", "6", "7"]);
    expect(patch).toMatchObject({
      taskId: "5",
      pendingTaskIds: ["5", "6", "7"],
      status: "running",
      src: null,
    });
  });

  test("只成功了一个任务：和单任务完全一致，不写 pendingTaskIds（并清掉上一批的残留）", () => {
    const patch = buildSubmittedPatch("image", ["5"]);
    expect(patch).toMatchObject({ taskId: "5", status: "running" });
    expect(patch.pendingTaskIds).toBeUndefined();
  });

  test("仍然接受单个任务 id", () => {
    expect(buildSubmittedPatch("image", "9")).toMatchObject({ taskId: "9", status: "running" });
  });
});

describe("settleSubmission：把一次提交的逐项结果整理成「成功的任务」和「失败的原因」", () => {
  const ok = (id: number) => ({ node_id: "n1", task: makeTask({ id }) });
  const err = (code: number, status: number, message = "") => ({
    node_id: "n1",
    error: { status, code, message },
  });

  test("按下标逐项处理：同一个节点的多个任务各取各的（不能按 node_id 查找）", () => {
    const result = settleSubmission([ok(11), ok(12), ok(13)]);
    expect(result.taskIds).toEqual(["11", "12", "13"]);
    expect(result.failures).toEqual([]);
  });

  test("部分失败：成功的留着，失败的翻译成给用户看的话", () => {
    const result = settleSubmission([ok(11), ok(12), err(40001, 402), err(40002, 429)]);
    expect(result.taskIds).toEqual(["11", "12"]);
    expect(result.failures.map((item) => item.kind)).toEqual(["credits", "limit"]);
  });

  test("一个都没成功：taskIds 为空，失败原因都在", () => {
    const result = settleSubmission([err(40001, 402), err(40001, 402)]);
    expect(result.taskIds).toEqual([]);
    expect(result.failures).toHaveLength(2);
  });
});

import { describe, expect, test } from "bun:test";

import {
  describeSubmitError,
  isRetryableSubmitError,
  submitWithRetry,
} from "@/utils/tasks/submit";

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

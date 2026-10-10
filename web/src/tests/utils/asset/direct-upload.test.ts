import { describe, expect, test } from "bun:test";

import type { UploadIntent } from "@/api/asset/type";
import {
  buildDirectRequest,
  buildIntentBody,
  isUploadAborted,
  planUpload,
  sendDirect,
} from "@/utils/asset/direct-upload";

const png = () => new File(["png-bytes"], "a.png", { type: "image/png" });

describe("buildIntentBody", () => {
  test("带文件名、大小与类型", () => {
    expect(buildIntentBody(png())).toEqual({
      file_name: "a.png",
      size: 9,
      mime_type: "image/png",
    });
  });

  test("文件类型为空时不申请直传（后端要求类型在白名单内）", () => {
    expect(buildIntentBody(new File(["x"], "noext"))).toBeNull();
  });

  test("空文件不申请直传", () => {
    expect(buildIntentBody(new File([], "a.png", { type: "image/png" }))).toBeNull();
  });
});

describe("planUpload：决定走哪条路", () => {
  test("mode=proxy 走中转", () => {
    expect(planUpload({ mode: "proxy" }, png())).toEqual({ kind: "proxy" });
  });

  test("接口失败（没有响应）走中转", () => {
    expect(planUpload(null, png())).toEqual({ kind: "proxy" });
    expect(planUpload(undefined, png())).toEqual({ kind: "proxy" });
  });

  test("direct 但缺关键信息时走中转，不抛错", () => {
    expect(planUpload({ mode: "direct", method: "put", url: "https://x" }, png())).toEqual({
      kind: "proxy",
    });
    expect(planUpload({ mode: "direct", intent_id: 1, method: "put" }, png())).toEqual({
      kind: "proxy",
    });
    expect(planUpload({ mode: "direct", intent_id: 1, url: "https://x" }, png())).toEqual({
      kind: "proxy",
    });
    expect(
      planUpload(
        { mode: "direct", intent_id: 1, url: "https://x", method: "delete" as never },
        png(),
      ),
    ).toEqual({ kind: "proxy" });
  });

  test("direct 且信息完整：给出意图 ID 与请求", () => {
    const plan = planUpload(
      {
        mode: "direct",
        intent_id: 9,
        method: "put",
        url: "https://b.s3/k",
        headers: { "Content-Type": "image/png" },
      },
      png(),
    );
    expect(plan.kind).toBe("direct");
    if (plan.kind === "direct") {
      expect(plan.intentId).toBe(9);
      expect(plan.request.url).toBe("https://b.s3/k");
    }
  });
});

describe("buildDirectRequest", () => {
  test("POST：fields 全部先进表单，file 字段最后一个", () => {
    const file = png();
    const intent: UploadIntent = {
      mode: "direct",
      intent_id: 1,
      method: "post",
      url: "https://b.s3.example.com/",
      fields: { key: "u1/202610/x.png", policy: "p", "x-amz-signature": "s" },
    };
    const request = buildDirectRequest(intent, file);
    expect(request?.url).toBe("https://b.s3.example.com/");
    expect(request?.init.method).toBe("POST");
    expect(request?.init.headers).toBeUndefined();
    const body = request?.init.body as FormData;
    expect([...body.keys()]).toEqual(["key", "policy", "x-amz-signature", "file"]);
    expect(body.get("key")).toBe("u1/202610/x.png");
    expect((body.get("file") as File).name).toBe("a.png");
  });

  test("POST：fields 里就算有名为 file 的键也不会盖掉真正的文件，file 仍只有一份且在最后", () => {
    const request = buildDirectRequest(
      {
        mode: "direct",
        intent_id: 1,
        method: "post",
        url: "https://x",
        fields: { file: "evil", key: "k" },
      },
      png(),
    );
    const body = request?.init.body as FormData;
    expect([...body.keys()]).toEqual(["key", "file"]);
    expect(body.get("file")).toBeInstanceOf(File);
  });

  test("POST：没有 fields 也能构造", () => {
    const request = buildDirectRequest(
      { mode: "direct", intent_id: 1, method: "post", url: "https://x" },
      png(),
    );
    const body = request?.init.body as FormData;
    expect([...body.keys()]).toEqual(["file"]);
  });

  test("PUT：直接把文件当 body，带上签名要求的请求头", () => {
    const file = png();
    const request = buildDirectRequest(
      {
        mode: "direct",
        intent_id: 2,
        method: "put",
        url: "https://acct.r2.cloudflarestorage.com/b/k?X-Amz-Signature=s",
        headers: { "Content-Type": "image/png" },
      },
      file,
    );
    expect(request?.init.method).toBe("PUT");
    expect(request?.init.body).toBe(file);
    expect(request?.init.headers).toEqual({ "Content-Type": "image/png" });
  });

  test("PUT：浏览器不允许脚本设置 Content-Length，签名时带上的这个头要去掉，其余保留", () => {
    const request = buildDirectRequest(
      {
        mode: "direct",
        intent_id: 2,
        method: "put",
        url: "https://x",
        headers: { "Content-Type": "image/png", "Content-Length": "9", "content-length": "9" },
      },
      png(),
    );
    expect(request?.init.headers).toEqual({ "Content-Type": "image/png" });
  });

  test("缺 url 或方法不对时返回 null", () => {
    expect(buildDirectRequest({ mode: "direct", intent_id: 1, method: "put" }, png())).toBeNull();
    expect(
      buildDirectRequest({ mode: "direct", intent_id: 1, url: "https://x" }, png()),
    ).toBeNull();
  });
});

/** 假的 XMLHttpRequest：记下收到的调用，由测试决定什么时候给什么结果 */
class FakeXhr {
  method = "";
  url = "";
  headers: Record<string, string> = {};
  body: unknown = null;
  withCredentials = true;
  status = 0;
  aborted = false;
  upload: { onprogress: ((event: unknown) => void) | null } = { onprogress: null };
  onload: (() => void) | null = null;
  onerror: (() => void) | null = null;
  ontimeout: (() => void) | null = null;
  onabort: (() => void) | null = null;
  open(method: string, url: string) {
    this.method = method;
    this.url = url;
  }
  setRequestHeader(key: string, value: string) {
    this.headers[key] = value;
  }
  send(body: unknown) {
    this.body = body;
  }
  abort() {
    this.aborted = true;
    this.onabort?.();
  }
  /** 服务端回了某个状态码 */
  respond(status: number) {
    this.status = status;
    this.onload?.();
  }
  /** 上传了多少字节 */
  progress(loaded: number, total: number, lengthComputable = true) {
    this.upload.onprogress?.({ loaded, total, lengthComputable });
  }
}

/** 发一次直传，返回假 XHR 和结果 promise */
function send(
  options: Omit<NonNullable<Parameters<typeof sendDirect>[1]>, "createXhr"> = {},
  request: Parameters<typeof sendDirect>[0] = {
    url: "https://x",
    init: { method: "PUT", body: png() },
  },
) {
  const xhr = new FakeXhr();
  const result = sendDirect(request, {
    ...options,
    createXhr: () => xhr as unknown as XMLHttpRequest,
  });
  return { xhr, result };
}

describe("sendDirect：直传结果判定（注入假 XHR，不发真实网络请求）", () => {
  test("2xx 算成功（S3 的 POST Policy 成功返回 204）", async () => {
    for (const status of [200, 201, 204]) {
      const { xhr, result } = send();
      xhr.respond(status);
      expect(await result).toBe(true);
    }
  });

  test("非 2xx 算失败，需要降级", async () => {
    for (const status of [403, 404, 500]) {
      const { xhr, result } = send();
      xhr.respond(status);
      expect(await result).toBe(false);
    }
  });

  test("网络出错、超时（CORS、断网）算失败，不向外抛", async () => {
    const first = send();
    first.xhr.onerror?.();
    expect(await first.result).toBe(false);

    const second = send();
    second.xhr.ontimeout?.();
    expect(await second.result).toBe(false);
  });

  test("原样交出方法、地址、请求头和 body，并且不带 cookie", async () => {
    const file = png();
    const { xhr, result } = send(
      {},
      {
        url: "https://x",
        init: { method: "PUT", headers: { "Content-Type": "image/png" }, body: file },
      },
    );
    xhr.respond(204);
    await result;
    expect(xhr.method).toBe("PUT");
    expect(xhr.url).toBe("https://x");
    expect(xhr.headers).toEqual({ "Content-Type": "image/png" });
    expect(xhr.body).toBe(file);
    expect(xhr.withCredentials).toBe(false);
  });
});

describe("sendDirect：上传进度", () => {
  test("按字节算整数百分比回调，向下取整", async () => {
    const seen: number[] = [];
    const { xhr, result } = send({ onProgress: (percent) => seen.push(percent) });
    xhr.progress(0, 200);
    xhr.progress(1, 200);
    xhr.progress(100, 200);
    xhr.progress(200, 200);
    xhr.respond(204);
    await result;
    expect(seen).toEqual([0, 0, 50, 100]);
  });

  test("总大小算不出来时不回调，免得报出 NaN", async () => {
    const seen: number[] = [];
    const { xhr, result } = send({ onProgress: (percent) => seen.push(percent) });
    xhr.progress(10, 0, false);
    xhr.respond(204);
    await result;
    expect(seen).toEqual([]);
  });
});

describe("sendDirect：取消", () => {
  test("信号中止就中止请求，并抛出 AbortError，不当成失败去降级", async () => {
    const controller = new AbortController();
    const { xhr, result } = send({ signal: controller.signal });
    controller.abort();
    expect(xhr.aborted).toBe(true);
    await expect(result).rejects.toMatchObject({ name: "AbortError" });
  });

  test("发出之前信号就已经中止：不发请求直接抛 AbortError", async () => {
    const controller = new AbortController();
    controller.abort();
    const { xhr, result } = send({ signal: controller.signal });
    expect(xhr.body).toBeNull();
    await expect(result).rejects.toMatchObject({ name: "AbortError" });
  });
});

describe("isUploadAborted：认出取消", () => {
  test("浏览器的 AbortError 和 axios 的 CanceledError 都算取消，其他错误不算", () => {
    expect(isUploadAborted(new DOMException("x", "AbortError"))).toBe(true);
    expect(isUploadAborted(Object.assign(new Error("canceled"), { name: "CanceledError" }))).toBe(
      true,
    );
    expect(isUploadAborted(new Error("boom"))).toBe(false);
    expect(isUploadAborted(null)).toBe(false);
  });
});
